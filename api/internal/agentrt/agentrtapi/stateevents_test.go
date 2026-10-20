package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T25 (design v5 §5.2 Gate A, 状态只由事件改), the routes' half. Every route
// Mount serves, walked from the router, is sent first what a client of it
// sends (each write route's valid path, stateevents_world_test.go), then
// bodies made of every field the package and the store declare, state words
// and the world's ids, from every caller, in four worlds: ready, blocked,
// paused and proposed work; a running attempt; a submitted task; a lost
// attempt. After each request:
//   - refused (4xx, 5xx): no table of the runtime differs in any row and no
//     event was appended;
//   - answered: for every task, mission, plan node and attempt that has state
//     events of the request or whose state differs, those events, replayed
//     from the state before, lead to the state after along the transition
//     tables (a new row from its creating event and its first state), each
//     task, mission and step move with its reason or the event of the same
//     transaction about the same row that caused it; a step's state_event_id
//     is its last move;
//   - every stream's hash chain verifies at the end of each world.
//
// At the end every write route (each attempt action apart) must have taken
// its valid request: answered, and, for a route whose valid path writes,
// with events appended; a read route must never have appended one, and a
// route nobody classified fails the test. The direct-SQL half is
// store_test.go TestIdentitiesAndTheEventRule.
//
// The seed is fixed; AGENTRT_FUZZ_SEED tries another, AGENTRT_FUZZ_N sets the
// bodies per route and caller in each world.
func TestRoutesMoveStateOnlyByEvents(t *testing.T) {
	routes := walkRoutes(t)
	if len(routes) < 40 {
		t.Fatalf("walked %d routes; the walk found less than Mount serves", len(routes))
	}
	seed, n := int64(20261020), 3
	if v := os.Getenv("AGENTRT_FUZZ_SEED"); v != "" {
		var err error
		seed, err = strconv.ParseInt(v, 10, 64)
		must(t, err)
	}
	if v := os.Getenv("AGENTRT_FUZZ_N"); v != "" {
		var err error
		n, err = strconv.Atoi(v)
		must(t, err)
	}
	tally := map[string]*routeTally{}
	moves := map[string]int{}
	for _, st := range fuzzStages {
		t.Logf("stage %q: seed %d", st, seed)
		f := newStateFuzz(t, seed, n, tally, moves)
		s := f.stage(t, st)
		f.round(t, s, routes, st)
		t.Logf("stage %q: seed %d, %d requests, %d answered, %d rows checked", st, seed, f.requests, f.answered, f.checked)
		streams := f.ids(t, `SELECT event_stream_id FROM agentrt.event_stream ORDER BY 1`)
		for _, id := range streams {
			if _, err := store.VerifyStream(ctx, f.pool, id); err != nil {
				t.Errorf("stage %q: stream %s: %v", st, id, err)
			}
		}
	}

	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := tally[k]
		t.Logf("  %-34s valid %2d  answered %4d  wrote %4d  refused %4d  moves %3d", k, c.valid, c.answered, c.wrote, c.refused, c.moves)
	}
	for _, rt := range routes {
		for _, k := range routeKeys(rt) {
			want, ok := routeExpect[k]
			c := tally[k]
			if c == nil {
				c = &routeTally{}
			}
			if rt.method == http.MethodGet && want != expectWrites && c.wrote > 0 {
				t.Errorf("%s: a read appended events %d times", k, c.wrote)
			}
			switch {
			case !ok && rt.method != http.MethodGet:
				t.Errorf("%s: a write route nobody classified; add its valid request and its expectation", k)
			case !ok:
			case c.valid == 0:
				t.Errorf("%s: its valid request was never answered", k)
			case want == expectWrites && c.validWrote == 0:
				t.Errorf("%s: its valid request never appended an event", k)
			case want == expectAnswers && c.validWrote > 0:
				t.Errorf("%s: its valid request, which writes nothing, appended events", k)
			}
		}
	}
	keys = keys[:0]
	for k := range moves {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("  move %-44s %3d", k, moves[k])
	}
}

// The transition tables: store/kernel.go taskMoves (with store/adopt.go
// adoptMoves, which accepts a salvaged result from ready, leased or running)
// and missionMoves; store/write.go stepMoves. A step is created planned, any
// other node with no state. An attempt's moves are by its events.
var (
	fuzzTaskMoves = map[string][]string{
		"proposed":       {"ready", "awaiting_owner", "blocked", "cancelled"},
		"awaiting_owner": {"ready", "cancelled"},
		"ready":          {"leased", "paused", "blocked", "cancelled", "accepted"},
		"leased":         {"running", "ready", "blocked", "accepted"},
		"running":        {"submitted", "ready", "blocked", "accepted"},
		"submitted":      {"verifying", "accepted", "blocked"},
		"verifying":      {"accepted", "rework", "rejected", "blocked"},
		"rework":         {"ready", "cancelled"},
		"blocked":        {"ready", "cancelled"},
		"paused":         {"ready", "cancelled"},
	}
	fuzzMissionMoves = map[string][]string{
		"proposed":       {"awaiting_owner", "active", "stopped"},
		"awaiting_owner": {"active", "stopped"},
		"active":         {"paused", "done", "stopped", "expired"},
		"paused":         {"active", "stopped", "expired"},
	}
	fuzzStepMoves = map[string][]string{
		"planned": {"active", "done", "stopped", "dropped"},
		"active":  {"done", "stopped", "dropped"},
		"done":    {"active", "dropped"},
		"stopped": {"active", "dropped"},
	}
	// By event: the attempt states it moves from and to (an ended attempt
	// that is already over keeps its state).
	fuzzAttemptMoves = map[string]map[string][]string{
		"attempt_leased":    {"": {"leased"}},
		"attempt_started":   {"leased": {"running"}},
		"attempt_stale":     {"leased": {"stale"}, "running": {"stale"}},
		"attempt_renewed":   {"stale": {"leased", "running"}, "leased": {"leased"}, "running": {"running"}},
		"attempt_lost":      {"leased": {"lost"}, "running": {"lost"}, "stale": {"lost"}},
		"attempt_submitted": {"running": {"submitted"}},
		"attempt_ended": {"leased": {"failed"}, "running": {"failed"}, "stale": {"failed"},
			"lost": {"lost"}, "failed": {"failed"}, "cancelled": {"cancelled"}, "finished": {"finished"}, "submitted": {"submitted"}},
		"attempt_cancelled": {"leased": {"cancelled"}, "running": {"cancelled"}, "stale": {"cancelled"}},
	}
)

type rowKey struct{ table, id string }

type routeTally struct{ valid, validWrote, answered, wrote, refused, moves int }

type stateFuzz struct {
	pool     *pgxpool.Pool
	task     string // the world's first task
	r        *rand.Rand
	n        int
	fields   []string
	digestQ  string // one query: a digest of every table of the runtime
	tally    map[string]*routeTally
	moves    map[string]int
	requests int
	answered int
	checked  int
}

func newStateFuzz(t *testing.T, seed int64, n int, tally map[string]*routeTally, moves map[string]int) *stateFuzz {
	t.Helper()
	pool, task := world(t)
	f := &stateFuzz{pool: pool, task: task, r: rand.New(rand.NewSource(seed)), n: n, fields: requestFields(t), tally: tally, moves: moves}
	var parts []string
	// The event table is append-only (its triggers) and checked by its last
	// id; every other table is digested whole but for its liveness columns.
	live := liveness(t, f)
	for _, tbl := range f.ids(t, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'agentrt' AND table_type = 'BASE TABLE'
	  AND table_name <> 'event' ORDER BY 1`) {
		row := "to_jsonb(x)"
		if cols := live[tbl]; len(cols) > 0 {
			row = "(to_jsonb(x) - ARRAY['" + strings.Join(cols, "','") + "']::text[])"
		}
		parts = append(parts, fmt.Sprintf(`SELECT '%[1]s', count(*), md5(COALESCE(string_agg(%[2]s::text, '|' ORDER BY %[2]s::text), '')) FROM agentrt.%[1]s x`, tbl, row))
	}
	if len(parts) < 15 {
		t.Fatalf("found %d tables of the runtime", len(parts))
	}
	f.digestQ = strings.Join(parts, " UNION ALL ")
	return f
}

// liveness is the runtime's liveness columns by table, read from its own
// definition, agentrt.only_liveness (migrations/0002_guards.sql): what is
// written without an event (a heartbeat, a host's poll). A refused request
// is not held to them: another transaction may write one meanwhile, and does
// (ci-platform-tj5wx: a claim's last poll committed after its handler had
// answered, pgx reporting the deadline on a commit the server still made).
// Every other column of every table must stay as it was.
func liveness(t *testing.T, f *stateFuzz) map[string][]string {
	t.Helper()
	var src string
	must(t, f.pool.QueryRow(ctx, `SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
	  WHERE n.nspname = 'agentrt' AND p.proname = 'only_liveness'`).Scan(&src))
	out := map[string][]string{}
	for _, m := range regexp.MustCompile(`tbl = '([a-z_]+)' THEN\s+live := ARRAY\[([^\]]*)\]`).FindAllStringSubmatch(src, -1) {
		for _, c := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(m[2], -1) {
			out[m[1]] = append(out[m[1]], c[1])
		}
	}
	if n := strings.Count(src, "live := ARRAY["); n == 0 || n != len(out) {
		t.Fatalf("read the liveness columns of %d tables from agentrt.only_liveness, which lists %d:\n%s", len(out), n, src)
	}
	for tbl, cols := range out {
		for _, c := range cols {
			var ok bool
			must(t, f.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'agentrt'
			  AND table_name = $1 AND column_name = $2)`, tbl, c).Scan(&ok))
			if !ok {
				t.Fatalf("agentrt.only_liveness names %s.%s, which is no column", tbl, c)
			}
		}
	}
	return out
}

// snapshot is the state of every row of the four state tables.
func (f *stateFuzz) snapshot(t *testing.T) map[rowKey]string {
	t.Helper()
	rows, err := f.pool.Query(ctx, `SELECT 'task', task_id, state FROM agentrt.task
	  UNION ALL SELECT 'mission', mission_id, state FROM agentrt.mission
	  UNION ALL SELECT 'plan_node', plan_node_id, COALESCE(state, '') FROM agentrt.plan_node
	  UNION ALL SELECT 'attempt', attempt_id, state FROM agentrt.attempt`)
	must(t, err)
	defer rows.Close()
	out := map[rowKey]string{}
	for rows.Next() {
		var k rowKey
		var st string
		must(t, rows.Scan(&k.table, &k.id, &st))
		out[k] = st
	}
	must(t, rows.Err())
	return out
}

// digest is every table's row count and a digest of all its rows.
func (f *stateFuzz) digest(t *testing.T) map[string]string {
	t.Helper()
	rows, err := f.pool.Query(ctx, f.digestQ)
	must(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var tbl, sum string
		var n int
		must(t, rows.Scan(&tbl, &n, &sum))
		out[tbl] = fmt.Sprintf("%d %s", n, sum)
	}
	must(t, rows.Err())
	return out
}

func (f *stateFuzz) lastEvent(t *testing.T) int64 {
	t.Helper()
	var id int64
	must(t, f.pool.QueryRow(ctx, `SELECT COALESCE(max(event_id), 0) FROM agentrt.event`).Scan(&id))
	return id
}

// ids lists one column, in the order the query gives.
func (f *stateFuzz) ids(t *testing.T, q string, args ...any) []string {
	t.Helper()
	rows, err := f.pool.Query(ctx, q, args...)
	must(t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	must(t, err)
	return out
}

// send sends one request as a client of key's route would and checks it;
// valid marks the route's valid request.
func (f *stateFuzz) send(t *testing.T, s *httptest.Server, key string, valid bool, caller, method, path string, body any, header ...string) (int, map[string]any) {
	t.Helper()
	header = append([]string{session, sessionOf(caller)}, header...)
	before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
	code, out := call(t, s, caller, method, path, body, header...)
	f.requests++
	doc, _ := json.Marshal(body)
	if b, ok := body.([]byte); ok {
		doc = b
	}
	f.check(t, key, valid, fmt.Sprintf("%s %s as %q (%d), headers %q, body %s", method, path, caller, code, header, doc), code, before, digest, since)
	return code, out
}

// check holds a request to T25 and counts it for its route.
func (f *stateFuzz) check(t *testing.T, key string, valid bool, what string, code int, before map[rowKey]string, digest map[string]string, since int64) {
	t.Helper()
	if err := f.judge(t, key, valid, code, before, digest, since); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// judge is check's verdict.
func (f *stateFuzz) judge(t *testing.T, key string, valid bool, code int, before map[rowKey]string, digest map[string]string, since int64) error {
	t.Helper()
	c := f.tally[key]
	if c == nil {
		c = &routeTally{}
		f.tally[key] = c
	}
	last := f.lastEvent(t)
	if code >= 400 || code == 0 {
		c.refused++
		if last != since {
			return fmt.Errorf("refused, yet it appended %d events", last-since)
		}
		now := f.digest(t)
		for tbl, d := range digest {
			if now[tbl] != d {
				return fmt.Errorf("refused, yet agentrt.%s changed (%s -> %s)", tbl, d, now[tbl])
			}
		}
		return nil
	}
	f.answered++
	c.answered++
	if valid {
		c.valid++
	}
	if last != since {
		c.wrote++
		if valid {
			c.validWrote++
		}
	}
	after := f.snapshot(t)
	for k := range before {
		if _, ok := after[k]; !ok {
			return fmt.Errorf("%s %s is gone", k.table, k.id)
		}
	}
	touched := map[rowKey]bool{}
	for k, st := range after {
		if old, ok := before[k]; !ok || old != st {
			touched[k] = true
		}
	}
	rows, err := f.pool.Query(ctx, `SELECT DISTINCT tbl, id FROM agentrt.event e,
	  LATERAL (VALUES ('task', e.task_id), ('mission', e.mission_id), ('plan_node', e.plan_node_id), ('attempt', e.attempt_id)) v(tbl, id)
	  WHERE e.event_id > $1 AND id IS NOT NULL
	    AND ((tbl = 'attempt' AND e.event_type = ANY($2)) OR e.event_type = tbl || '_state_changed')`, since, attemptStateEvents())
	must(t, err)
	for rows.Next() {
		var k rowKey
		must(t, rows.Scan(&k.table, &k.id))
		touched[k] = true
	}
	rows.Close()
	must(t, rows.Err())
	for _, k := range sortedKeys(touched) {
		old, existed := before[k]
		now, ok := after[k]
		if !ok {
			return fmt.Errorf("an event names %s %s, which is not there", k.table, k.id)
		}
		f.checked++
		if !existed || old != now {
			c.moves++
			f.moves[fmt.Sprintf("%s %s -> %s", k.table, map[bool]string{true: old, false: "(new)"}[existed], now)]++
		}
		if err := f.replay(t, k, old, now, existed, since); err != nil {
			return fmt.Errorf("%s %s %q -> %q: %w", k.table, k.id, old, now, err)
		}
	}
	return nil
}

// sessionOf is a session a caller's token keeps to itself (a thread is
// registered by the token it first wrote with).
func sessionOf(caller string) string { return "s-" + strings.NewReplacer(":", "-").Replace(caller) }

func attemptStateEvents() []string {
	out := make([]string, 0, len(fuzzAttemptMoves))
	for e := range fuzzAttemptMoves {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

type fuzzEvent struct {
	id       int64
	typ      string
	cause    bool          // cause_event_id is set
	siblings []fuzzSibling // the other events of its transaction about the same row, in order
	body     map[string]any
}

type fuzzSibling struct {
	id  int64
	typ string
}

// The moves that may leave their reason to another event of their
// transaction about the same row, by the state moved to: that event's type,
// and whether the kernel writes it after the move or before it. A task runs
// because its attempt started (store/attempt.go Start: the event, then the
// move), is submitted because its attempt was (Submit, the same), is ready
// with its route (kernel.go moveTaskWith: the move, then route_decided).
// STG 10-20: every task move without a reason is one of these three. Every
// other move says why.
var fuzzImplicitCause = map[string]map[string]struct {
	typ   string
	after bool
}{
	"task": {"running": {"attempt_started", false}, "submitted": {"attempt_submitted", false}, "ready": {"route_decided", true}},
}

// replay checks one row's events since the request began against its states
// before and after.
func (f *stateFuzz) replay(t *testing.T, k rowKey, old, now string, existed bool, since int64) error {
	t.Helper()
	col := k.table + "_id"
	rows, err := f.pool.Query(ctx, `SELECT e.event_id, e.event_type, e.cause_event_id IS NOT NULL, e.body_json,
	    ARRAY(SELECT o.event_id FROM agentrt.event o WHERE o.txid = e.txid AND o.event_id <> e.event_id AND o.`+col+` = $2 ORDER BY o.event_id),
	    ARRAY(SELECT o.event_type FROM agentrt.event o WHERE o.txid = e.txid AND o.event_id <> e.event_id AND o.`+col+` = $2 ORDER BY o.event_id)
	  FROM agentrt.event e WHERE e.event_id > $1 AND e.`+col+` = $2 ORDER BY e.event_id`, since, k.id)
	must(t, err)
	var evs []fuzzEvent
	for rows.Next() {
		var e fuzzEvent
		var raw []byte
		var ids []int64
		var types []string
		must(t, rows.Scan(&e.id, &e.typ, &e.cause, &raw, &ids, &types))
		for i := range ids {
			e.siblings = append(e.siblings, fuzzSibling{ids[i], types[i]})
		}
		must(t, json.Unmarshal(raw, &e.body))
		evs = append(evs, e)
	}
	rows.Close()
	must(t, rows.Err())
	if k.table == "attempt" {
		return replayAttempt(evs, old, now, existed)
	}
	from := old
	if !existed {
		if from, err = f.created(t, k, evs, since); err != nil {
			return err
		}
	}
	last, err := replayChain(k.table, evs, from, now)
	if err != nil {
		return err
	}
	if k.table == "plan_node" && last != 0 {
		var at int64
		must(t, f.pool.QueryRow(ctx, `SELECT COALESCE(state_event_id, 0) FROM agentrt.plan_node WHERE plan_node_id = $1`, k.id).Scan(&at))
		if at != last {
			return fmt.Errorf("state_event_id %d, the last move is event %d", at, last)
		}
	}
	return nil
}

// created checks the event that made a new row and returns the state it
// starts in: a task and a mission proposed; a step planned, another node none.
func (f *stateFuzz) created(t *testing.T, k rowKey, evs []fuzzEvent, since int64) (string, error) {
	t.Helper()
	switch k.table {
	case "task":
		if !slices.ContainsFunc(evs, func(e fuzzEvent) bool { return e.typ == "task_proposed" }) {
			return "", fmt.Errorf("a new task without task_proposed")
		}
		return "proposed", nil
	case "mission":
		if !slices.ContainsFunc(evs, func(e fuzzEvent) bool { return e.typ == "mission_proposed" }) {
			return "", fmt.Errorf("a new mission without mission_proposed")
		}
		return "proposed", nil
	}
	// A plan node: plan_node_created names it (an import writes one for each
	// node, store/planimport.go importNode), or a plan change lists it.
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_id > $1 AND (
	    (event_type = 'plan_node_created' AND plan_node_id = $2)
	    OR (event_type = 'plan_changed' AND body_json->'nodes' ? $2))`, since, k.id).Scan(&n))
	if n == 0 {
		return "", fmt.Errorf("a new plan node that no event of the request creates")
	}
	var kind string
	must(t, f.pool.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1`, k.id).Scan(&kind))
	if kind == "step" {
		return "planned", nil
	}
	return "", nil
}

// replayChain runs a task's, mission's or step's moves among evs (its events
// of the request, in order) from the state before and returns the last
// move's event: each from the state the last left, along its table, with a
// cause, ending at the state after.
func replayChain(table string, evs []fuzzEvent, from, now string) (int64, error) {
	moves := map[string]map[string][]string{"task": fuzzTaskMoves, "mission": fuzzMissionMoves, "plan_node": fuzzStepMoves}[table]
	var last int64
	for _, e := range evs {
		if e.typ != table+"_state_changed" {
			continue
		}
		ef, et := fmt.Sprint(e.body["from"]), fmt.Sprint(e.body["to"])
		if ef != from {
			return 0, fmt.Errorf("event %d moves from %q, the row was %q", e.id, ef, from)
		}
		if !slices.Contains(moves[ef], et) {
			return 0, fmt.Errorf("event %d makes %q -> %q, which no transition table has", e.id, ef, et)
		}
		if !hasCause(table, e) {
			return 0, fmt.Errorf("event %d (%s) has no reason, no cause and no event of its transaction about this row", e.id, e.typ)
		}
		from, last = et, e.id
	}
	if from != now {
		return 0, fmt.Errorf("the events end at %q, the row is %q", from, now)
	}
	return last, nil
}

// hasCause: a move says why (reason, cause or cause_event_id), or it is one
// of fuzzImplicitCause's and its transaction has that event about the row
// where the kernel writes it for this move: between the row's move before it
// and this one, or between this one and the row's next move.
func hasCause(table string, e fuzzEvent) bool {
	if e.cause {
		return true
	}
	for _, key := range []string{"reason", "cause"} {
		if v, ok := e.body[key].(string); ok && v != "" {
			return true
		}
	}
	by, ok := fuzzImplicitCause[table][fmt.Sprint(e.body["to"])]
	if !ok {
		return false
	}
	lo, hi := int64(0), int64(math.MaxInt64) // the row's moves around this one
	for _, o := range e.siblings {
		if o.typ != table+"_state_changed" {
			continue
		}
		if o.id < e.id && o.id > lo {
			lo = o.id
		}
		if o.id > e.id && o.id < hi {
			hi = o.id
		}
	}
	for _, o := range e.siblings {
		if o.typ == by.typ && ((by.after && o.id > e.id && o.id < hi) || (!by.after && o.id < e.id && o.id > lo)) {
			return true
		}
	}
	return false
}

// replayAttempt runs an attempt's state events from where it was; a renewal
// may come back leased or running, so the possible states are a set.
func replayAttempt(evs []fuzzEvent, old, now string, existed bool) error {
	states := map[string]bool{old: true}
	if !existed {
		states = map[string]bool{"": true}
	}
	for _, e := range evs {
		m, ok := fuzzAttemptMoves[e.typ]
		if !ok {
			continue
		}
		next := map[string]bool{}
		for s := range states {
			for _, to := range m[s] {
				next[to] = true
			}
		}
		if len(next) == 0 {
			return fmt.Errorf("event %d (%s) cannot follow %v", e.id, e.typ, keysOf(states))
		}
		states = next
	}
	if !states[now] {
		return fmt.Errorf("the events end at %v, the row is %q", keysOf(states), now)
	}
	return nil
}

func sortedKeys(m map[rowKey]bool) []rowKey {
	out := make([]rowKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].table+out[i].id < out[j].table+out[j].id })
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The checker catches what it is there to catch. Reviews 449 and 451 found
// each of these passing an earlier checker (a regression case): a step moved
// out of dropped; a wrong move inside a round trip that ends where it began;
// a move whose only other event in its transaction is about something else,
// or is about the task but is not what causes that move; a refused request
// that changed a row's other columns; a plan node no event of the request
// creates beside an import head on its tree; an attempt that submits without
// starting. Two retained cases held before too: a move with no reason and
// nothing else in its transaction, and events that end elsewhere than the row.
func TestStateEventCheckerCatches(t *testing.T) {
	// ev is a move; each sibling is "<id> <type>".
	ev := func(id int64, typ, from, to string, reason bool, siblings ...string) fuzzEvent {
		body := map[string]any{"from": from, "to": to}
		if reason {
			body["reason"] = "test"
		}
		e := fuzzEvent{id: id, typ: typ, body: body}
		for _, x := range siblings {
			n, kind, _ := strings.Cut(x, " ")
			sid, err := strconv.ParseInt(n, 10, 64)
			must(t, err)
			e.siblings = append(e.siblings, fuzzSibling{sid, kind})
		}
		return e
	}
	for name, c := range map[string]struct {
		table     string
		evs       []fuzzEvent
		from, now string
		want      string
	}{
		"regression: a step out of dropped": {"plan_node", []fuzzEvent{ev(1, "plan_node_state_changed", "dropped", "active", true)}, "dropped", "active", "no transition table"},
		"regression: a round trip with a wrong second move": {"task", []fuzzEvent{ev(1, "task_state_changed", "ready", "paused", true),
			ev(2, "task_state_changed", "blocked", "ready", true)}, "ready", "ready", `moves from "blocked"`},
		"regression: paused, its only sibling a checkpoint of the task": {"task", []fuzzEvent{ev(1, "task_state_changed", "ready", "paused", false, "2 attempt_checkpointed")},
			"ready", "paused", "no reason"},
		"regression: running, its only sibling the route": {"task", []fuzzEvent{ev(1, "task_state_changed", "leased", "running", false, "2 route_decided")},
			"leased", "running", "no reason"},
		"regression: ready again, the route an earlier one's": {"task", []fuzzEvent{
			ev(2, "task_state_changed", "ready", "paused", true, "1 route_decided", "3 task_state_changed"),
			ev(3, "task_state_changed", "paused", "ready", false, "1 route_decided", "2 task_state_changed")}, "ready", "ready", "no reason"},
		"regression: running, started before the task's move before it": {"task", []fuzzEvent{
			ev(2, "task_state_changed", "running", "ready", true, "1 attempt_started", "3 task_state_changed", "4 task_state_changed"),
			ev(3, "task_state_changed", "ready", "leased", true, "1 attempt_started", "2 task_state_changed", "4 task_state_changed"),
			ev(4, "task_state_changed", "leased", "running", false, "1 attempt_started", "2 task_state_changed", "3 task_state_changed")},
			"running", "running", "no reason"},
		"retained: no reason, nothing else about the row": {"task", []fuzzEvent{ev(1, "task_state_changed", "leased", "running", false)}, "leased", "running", "no reason"},
		"retained: events that end elsewhere":             {"mission", []fuzzEvent{ev(1, "mission_state_changed", "active", "paused", true)}, "active", "stopped", "end at"},
	} {
		if _, err := replayChain(c.table, c.evs, c.from, c.now); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", name, err, c.want)
		}
	}
	for name, e := range map[string]fuzzEvent{
		"running, its attempt started":   ev(2, "task_state_changed", "leased", "running", false, "1 attempt_started"),
		"submitted, its attempt was":     ev(2, "task_state_changed", "running", "submitted", false, "1 attempt_submitted"),
		"ready, with its route":          ev(1, "task_state_changed", "proposed", "ready", false, "2 route_decided"),
		"paused, with its reason":        ev(1, "task_state_changed", "ready", "paused", true),
		"a move with its cause_event_id": {id: 1, typ: "task_state_changed", cause: true, body: map[string]any{"from": "ready", "to": "paused"}},
	} {
		if !hasCause("task", e) {
			t.Errorf("%s: refused", name)
		}
	}
	if err := replayAttempt([]fuzzEvent{{id: 1, typ: "attempt_submitted"}}, "leased", "submitted", true); err == nil {
		t.Error("regression: a submit of an attempt that never started passed")
	}

	f := newStateFuzz(t, 1, 1, map[string]*routeTally{}, map[string]int{})
	move := func(tx pgx.Tx, from, to string, reason bool) error {
		body := map[string]any{"from": from, "to": to}
		if reason {
			body["reason"] = "test"
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-1", PlanNode: "R2:1-1", TaskID: f.task, Body: body}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = $2, version = version + 1 WHERE task_id = $1`, f.task, to)
		return err
	}
	judge := func(code int, fn func(tx pgx.Tx) error) error {
		t.Helper()
		before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
		must(t, store.InTx(ctx, f.pool, fn))
		return f.judge(t, "test", false, code, before, digest, since)
	}
	// A move whose transaction's other event (an upload) is not about the task.
	err := judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte("unrelated")); err != nil {
			return err
		}
		return move(tx, "ready", "paused", false)
	})
	if err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Errorf("regression: a move caused by an unrelated upload: %v", err)
	}
	// A move whose transaction's other event is about the task but is a note.
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, nerr := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "note", Actor: "controller",
			MissionID: "M-1", TaskID: f.task, Body: map[string]any{"text": "unrelated"}}); nerr != nil {
			return nerr
		}
		return move(tx, "paused", "ready", false)
	})
	if err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Errorf("regression: a move caused by a note about the task: %v", err)
	}
	// Ready again, the only route in the transaction one decided before the
	// task was paused (review 452 A-4).
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, rerr := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "route_decided", Actor: "controller",
			MissionID: "M-1", PlanNode: "R2:1-1", TaskID: f.task, Body: map[string]any{"job": "implementer", "candidates": []any{},
				"excluded": []any{}, "inputs": map[string]any{}, "fingerprint": "sha256:" + strings.Repeat("ab", 32)}}); rerr != nil {
			return rerr
		}
		if merr := move(tx, "ready", "paused", true); merr != nil {
			return merr
		}
		return move(tx, "paused", "ready", false)
	})
	if err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Errorf("regression: ready again with an earlier route: %v", err)
	}
	must(t, store.InTx(ctx, f.pool, func(tx pgx.Tx) error { return move(tx, "ready", "paused", true) }))
	// A round trip back to where the task began (paused), its second move
	// recorded wrong: the state is the same before and after.
	if st := f.snapshot(t)[rowKey{"task", f.task}]; st != "paused" {
		t.Fatalf("the task is %s", st)
	}
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if merr := move(tx, "paused", "ready", true); merr != nil {
			return merr
		}
		return move(tx, "blocked", "paused", true)
	})
	if err == nil || !strings.Contains(err.Error(), `moves from "blocked"`) {
		t.Errorf("regression: a wrong move inside a round trip: %v", err)
	}
	// A node written beside an import head on its tree, with no event of its own.
	must(t, store.InTx(ctx, f.pool, func(tx pgx.Tx) error {
		_, cerr := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "A", Title: "architecture", Root: "ARCH"})
		return cerr
	}))
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, ierr := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: an import head"}}); ierr != nil {
			return ierr
		}
		_, ierr := tx.Exec(ctx, `INSERT INTO agentrt.plan_node (plan_node_id, parent_plan_node_id, node_kind, ordinal, label, title, created_by_actor_id)
		  VALUES ('ARCH:extra', 'ARCH', 'arch_row', 9, 'extra', 'not in the document', 'controller')`)
		return ierr
	})
	if err == nil || !strings.Contains(err.Error(), "no event of the request creates") {
		t.Errorf("regression: a node beside an import head: %v", err)
	}
	// A refused request that changed a column other than the state.
	var db string
	must(t, f.pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
	for _, q := range []string{`SET session_replication_role = replica`, `UPDATE agentrt.task SET title = 'edited' WHERE task_id = '` + f.task + `'`} {
		_, err = admin.Exec(ctx, q)
		must(t, err)
	}
	if err = f.judge(t, "test", false, http.StatusConflict, before, digest, since); err == nil || !strings.Contains(err.Error(), "agentrt.task changed") {
		t.Errorf("regression: a refused request that edited a title: %v", err)
	}
	// A refused request while another transaction writes a host's poll, a
	// liveness column (ci-platform-tj5wx): nothing the request did.
	before, digest, since = f.snapshot(t), f.digest(t), f.lastEvent(t)
	_, err = f.pool.Exec(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = 'h1'`)
	must(t, err)
	if err = f.judge(t, "test", false, http.StatusUnauthorized, before, digest, since); err != nil {
		t.Errorf("regression: a refused request beside a host's poll: %v", err)
	}
	// Another column of the same row still counts.
	before, digest, since = f.snapshot(t), f.digest(t), f.lastEvent(t)
	_, err = admin.Exec(ctx, `UPDATE agentrt.host SET capacity_json = '{"slots": 9}' WHERE host_id = 'h1'`)
	must(t, err)
	if err = f.judge(t, "test", false, http.StatusUnauthorized, before, digest, since); err == nil || !strings.Contains(err.Error(), "agentrt.host changed") {
		t.Errorf("a refused request that changed a host's capacity: %v", err)
	}
}
