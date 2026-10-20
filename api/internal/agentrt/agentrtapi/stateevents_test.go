package agentrtapi_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T25 (design v5 §5.2 Gate A, 状态只由事件改), the routes' half: every route
// Mount serves, walked from the router so a new one is fuzzed without being
// listed, is sent bodies made of the fields the package and the store read,
// state words and ids of the world, from every caller. After each request:
// a refused one changed nothing and appended nothing; for an answered one,
// every task, mission, plan node and attempt whose state moved has events of
// that request naming it that lead from the old state to the new one, each
// move one of the transition tables, each with its reason or the event of
// the same transaction that caused it. The world is walked through a claim,
// a start and a hand-over between rounds, so the routes are fuzzed against
// live, running and submitted work too. The database half (direct SQL by the
// runtime role) is store_test.go TestIdentitiesAndTheEventRule.
//
// AGENTRT_FUZZ_SEED repeats a run; AGENTRT_FUZZ_N sets the bodies per route
// and caller in each round.
func TestRoutesMoveStateOnlyByEvents(t *testing.T) {
	routes := walkRoutes(t)
	if len(routes) < 40 {
		t.Fatalf("walked %d routes; the walk found less than Mount serves", len(routes))
	}
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	// Each stage is its own world, brought to its point by a client's own
	// requests (checked like the rest), then fuzzed: what one round's
	// requests do (a stop, a claim) never keeps the next from its stage.
	stages := []struct {
		name  string
		steps int // of the hand-over below, after the claim: 0 not claimed, 1 started, 5 submitted
	}{{"a ready task, a blocked, a paused and a proposed one", 0}, {"a running attempt", 1}, {"a submitted task", 5}}
	var requests, answered, moved int
	tally, moves := map[string][3]int{}, map[string]int{}
	for _, st := range stages {
		pool, task := world(t)
		f := newStateFuzz(t, pool)
		s := server(t, pool)
		cards, askFails = nil, false
		if st.steps > 0 {
			code, claim := f.send(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}})
			if code != http.StatusOK || claim["task_id"] != task {
				t.Fatalf("claim: %d %v", code, claim)
			}
			attempt := claim["attempt_id"].(string)
			fence := func(extra map[string]any) map[string]any {
				b := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]}
				for k, v := range extra {
					b[k] = v
				}
				return b
			}
			report := []byte("# done\nall tests pass\n")
			steps := []struct {
				action string
				body   any
			}{
				{"start", fence(map[string]any{"model_provider": "cursor-mix"})},
				{"upload", report},
				{"computed", fence(map[string]any{"exit_code": 0, "end_how": "exit", "conclusion": "done"})},
				{"persist", fence(map[string]any{"report": artifactID(report), "pushed_ref": claim["pushed_ref"], "pushed_commit": pushedAt})},
				{"submit", fence(nil)},
			}
			for _, step := range steps[:st.steps] {
				var code int
				var out map[string]any
				if step.action == "upload" {
					code, out = f.send(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "report")
				} else {
					code, out = f.send(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body)
				}
				if code != http.StatusOK {
					t.Fatalf("%s: %d %v", step.action, code, out)
				}
			}
			want := map[int]string{1: "running", 5: "submitted"}[st.steps]
			if got := f.snapshot(t)[rowKey{"task", task}]; got != want {
				t.Fatalf("stage %q: the task is %s", st.name, got)
			}
		}
		f.round(t, s, routes, st.name)
		requests, answered, moved = requests+f.requests, answered+f.answered, moved+f.moved
		for k, v := range f.tally {
			c := tally[k]
			tally[k] = [3]int{c[0] + v[0], c[1] + v[1], c[2] + v[2]}
		}
		for k, v := range f.moves {
			moves[k] += v
		}
		t.Logf("stage %q: seed %d, %d routes, %d requests, %d answered, %d state moves checked", st.name, f.seed, len(routes), f.requests, f.answered, f.moved)
		rows, err := pool.Query(ctx, `SELECT event_stream_id FROM agentrt.event_stream ORDER BY 1`)
		must(t, err)
		streams, err := pgx.CollectRows(rows, pgx.RowTo[string])
		must(t, err)
		for _, id := range streams {
			if _, err := store.VerifyStream(ctx, pool, id); err != nil {
				t.Errorf("stream %s: %v", id, err)
			}
		}
	}
	if moved == 0 {
		t.Fatal("no request moved any state: the fuzz checked nothing")
	}
	t.Logf("%d requests, %d answered, %d state moves checked", requests, answered, moved)
	for _, rt := range routes {
		c := tally[rt.method+" "+rt.pattern]
		t.Logf("  %-6s %-28s answered %4d  refused %4d  moves %3d", rt.method, rt.pattern, c[0], c[1], c[2])
	}
	keys := make([]string, 0, len(moves))
	for k := range moves {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("  move %-40s %3d", k, moves[k])
	}
}

// The transition tables (store/kernel.go taskMoves and missionMoves, and
// store/adopt.go adoptMoves, which accepts a salvaged result from ready,
// leased or running). A route that makes any other move fails the test.
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
	// The events that put an attempt in each state.
	fuzzAttemptBy = map[string][]string{
		"leased":    {"attempt_leased", "attempt_renewed"},
		"running":   {"attempt_started", "attempt_renewed"},
		"stale":     {"attempt_stale"},
		"lost":      {"attempt_lost"},
		"submitted": {"attempt_submitted"},
		"failed":    {"attempt_ended"},
		"cancelled": {"attempt_cancelled"},
		"finished":  {"verification_recorded"},
	}
)

// Every state of every table (migrations/0001_core.sql), and other words a
// caller might try.
var fuzzStates = []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying", "accepted",
	"rework", "rejected", "blocked", "paused", "cancelled", "active", "done", "stopped", "expired", "planned", "dropped",
	"stale", "finished", "lost", "failed", "draining", "retired", "approved", "pass", "merged", ""}

// Keys that name a state, sent beside or instead of a route's own fields.
var fuzzStateKeys = []string{"state", "to", "from", "status", "task_state", "mission_state", "attempt_state", "plan_state",
	"phase", "verdict", "accepted", "set_state", "new_state"}

var fuzzCallers = []string{"agentd:h1", "agentd:h2", "reporter", "operator", "admin", "seat:chief_of_staff", "viewer", ""}

var fuzzAttemptActions = []string{"heartbeat", "start", "checkpoint", "computed", "persist", "submit", "salvage", "ended",
	"accept", "state", "cancel", "lost", ""}

type rowKey struct{ table, id string }

type stateFuzz struct {
	pool     *pgxpool.Pool
	route    string            // the route of the request being checked
	tally    map[string][3]int // by route: answered, refused, moves
	moves    map[string]int    // by table and move
	r        *rand.Rand
	seed     int64
	n        int
	fields   []string
	requests int
	answered int
	moved    int
}

// newStateFuzz adds to world: missions M-2 (proposed) and M-3 (paused);
// tasks of M-1 that are blocked, paused and proposed; host h2.
func newStateFuzz(t *testing.T, pool *pgxpool.Pool) *stateFuzz {
	t.Helper()
	seed := time.Now().UnixNano()
	if v := os.Getenv("AGENTRT_FUZZ_SEED"); v != "" {
		var err error
		seed, err = strconv.ParseInt(v, 10, 64)
		must(t, err)
	}
	n := 6
	if v := os.Getenv("AGENTRT_FUZZ_N"); v != "" {
		var err error
		n, err = strconv.Atoi(v)
		must(t, err)
	}
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	tx(func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-2", Title: "m2", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "m3", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		if err := store.MoveMission(ctx, tx, "controller", "M-3", "active", "test"); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "paused", "test")
	})
	for _, to := range []string{"blocked", "paused", ""} {
		tx(func(tx pgx.Tx) error {
			spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task "+to+"\n"))
			if err != nil {
				return err
			}
			id := store.NewTaskID()
			if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
				Class: "go", Title: "t " + to, SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
				return err
			}
			switch to {
			case "":
				return nil
			case "paused":
				if err := store.MarkReady(ctx, tx, "controller", id); err != nil {
					return err
				}
			}
			return store.MoveTask(ctx, tx, "controller", id, to, "test")
		})
	}
	must(t, store.RegisterHost(ctx, pool, "owner", "h2", "dev"))
	return &stateFuzz{pool: pool, r: rand.New(rand.NewSource(seed)), seed: seed, n: n, fields: requestFields(t),
		tally: map[string][3]int{}, moves: map[string]int{}}
}

// requestFields is every JSON field name the package and the store declare,
// read from their sources: what any route might read from a body.
func requestFields(t *testing.T) []string {
	t.Helper()
	tag := regexp.MustCompile("json:\"([a-z_0-9]+)")
	seen := map[string]bool{}
	for _, dir := range []string{".", "../store"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		must(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			src, err := os.ReadFile(file)
			must(t, err)
			for _, m := range tag.FindAllSubmatch(src, -1) {
				seen[string(m[1])] = true
			}
		}
	}
	for _, k := range fuzzStateKeys {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) < 50 {
		t.Fatalf("read only %d field names from the sources", len(out))
	}
	return out
}

type fuzzRoute struct{ method, pattern string }

// walkRoutes is every route Mount serves, as chi lists them.
func walkRoutes(t *testing.T) []fuzzRoute {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(path, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: fixture-walk\n"), 0o600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { agentrtapi.Mount(r, auth, &agentrtapi.API{}) })
	var out []fuzzRoute
	must(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.TrimPrefix(route, "/api/v1/agentrt/"), "/")
		out = append(out, fuzzRoute{method, route})
		return nil
	}))
	sort.Slice(out, func(i, j int) bool { return out[i].method+out[i].pattern < out[j].method+out[j].pattern })
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

func (f *stateFuzz) lastEvent(t *testing.T) int64 {
	t.Helper()
	var id int64
	must(t, f.pool.QueryRow(ctx, `SELECT COALESCE(max(event_id), 0) FROM agentrt.event`).Scan(&id))
	return id
}

// ids lists one column of a table, for paths and bodies.
func (f *stateFuzz) ids(t *testing.T, q string) []string {
	t.Helper()
	rows, err := f.pool.Query(ctx, q)
	must(t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	must(t, err)
	return out
}

// send is call, with the checks after it.
func (f *stateFuzz) send(t *testing.T, s *httptest.Server, caller, method, path string, body any, header ...string) (int, map[string]any) {
	t.Helper()
	f.route = method + " " + path
	before, since := f.snapshot(t), f.lastEvent(t)
	code, out := call(t, s, caller, method, path, body, append([]string{session, "fuzz"}, header...)...)
	f.requests++
	f.check(t, fmt.Sprintf("%s %s as %q (%d)", method, path, caller, code), code, before, since)
	return code, out
}

// check holds the request to T25: a refusal changed nothing; every move has
// its events.
func (f *stateFuzz) check(t *testing.T, what string, code int, before map[rowKey]string, since int64) {
	t.Helper()
	after, last := f.snapshot(t), f.lastEvent(t)
	c := f.tally[f.route]
	defer func() { f.tally[f.route] = c }()
	if code >= 400 || code == 0 {
		c[1]++
		if last != since {
			t.Fatalf("%s: refused, yet it appended %d events", what, last-since)
		}
		for k, st := range after {
			if old, ok := before[k]; !ok || old != st {
				t.Fatalf("%s: refused, yet %s %s moved %q -> %q", what, k.table, k.id, old, st)
			}
		}
		return
	}
	f.answered++
	c[0]++
	for k, st := range after {
		old, existed := before[k]
		if existed && old == st {
			continue
		}
		f.moved++
		c[2]++
		f.moves[fmt.Sprintf("%s %s -> %s", k.table, map[bool]string{true: old, false: "(new)"}[existed], st)]++
		if err := f.moveHasEvents(t, k, old, st, existed, since); err != nil {
			t.Fatalf("%s: %s %s %q -> %q: %v", what, k.table, k.id, old, st, err)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Fatalf("%s: %s %s is gone", what, k.table, k.id)
		}
	}
}

type fuzzEvent struct {
	id, txid int64
	typ      string
	body     map[string]any
}

// moveHasEvents checks one row's move against the events appended since.
func (f *stateFuzz) moveHasEvents(t *testing.T, k rowKey, old, now string, existed bool, since int64) error {
	t.Helper()
	col := k.table + "_id"
	rows, err := f.pool.Query(ctx, `SELECT event_id, txid, event_type, body_json FROM agentrt.event
	  WHERE event_id > $1 AND `+col+` = $2 ORDER BY event_id`, since, k.id)
	must(t, err)
	var evs []fuzzEvent
	for rows.Next() {
		var e fuzzEvent
		var raw []byte
		must(t, rows.Scan(&e.id, &e.txid, &e.typ, &raw))
		must(t, json.Unmarshal(raw, &e.body))
		evs = append(evs, e)
	}
	rows.Close()
	must(t, rows.Err())
	has := func(types ...string) bool {
		return slices.ContainsFunc(evs, func(e fuzzEvent) bool { return slices.Contains(types, e.typ) })
	}
	if k.table == "attempt" {
		if !existed && !has("attempt_leased") {
			return fmt.Errorf("a new attempt without attempt_leased (%v)", typesOf(evs))
		}
		if !has(fuzzAttemptBy[now]...) {
			return fmt.Errorf("no %v among %v", fuzzAttemptBy[now], typesOf(evs))
		}
		return nil
	}
	created, changed := map[string]string{"task": "task_proposed", "mission": "mission_proposed", "plan_node": "plan_node_created"}[k.table],
		k.table+"_state_changed"
	from := old
	if !existed {
		if !has(created) {
			return fmt.Errorf("a new row without %s (%v)", created, typesOf(evs))
		}
		from = map[string]string{"task": "proposed", "mission": "proposed"}[k.table]
	}
	var chain []fuzzEvent
	for _, e := range evs {
		if e.typ == changed {
			chain = append(chain, e)
		}
	}
	if len(chain) == 0 {
		if !existed && (k.table == "plan_node" || now == from) {
			return nil // created in this state
		}
		return fmt.Errorf("no %s (%v)", changed, typesOf(evs))
	}
	moves := map[string]map[string][]string{"task": fuzzTaskMoves, "mission": fuzzMissionMoves}[k.table]
	for i, e := range chain {
		ef, et := fmt.Sprint(e.body["from"]), fmt.Sprint(e.body["to"])
		if (i > 0 || existed || k.table != "plan_node") && ef != from {
			return fmt.Errorf("event %d moves from %q, the row was %q", e.id, ef, from)
		}
		if moves != nil && !slices.Contains(moves[ef], et) {
			return fmt.Errorf("event %d makes %q -> %q, which no transition table has", e.id, ef, et)
		}
		if !f.hasCause(t, e) {
			return fmt.Errorf("event %d (%s) has no reason and no other event in its transaction", e.id, e.typ)
		}
		from = et
	}
	if from != now {
		return fmt.Errorf("the events end at %q, the row is %q", from, now)
	}
	if k.table == "plan_node" {
		var at int64
		must(t, f.pool.QueryRow(ctx, `SELECT COALESCE(state_event_id, 0) FROM agentrt.plan_node WHERE plan_node_id = $1`, k.id).Scan(&at))
		if at != chain[len(chain)-1].id {
			return fmt.Errorf("state_event_id %d, the last move is event %d", at, chain[len(chain)-1].id)
		}
	}
	return nil
}

// hasCause: a state event says why, or another event of its transaction is
// the cause (attempt_started for running, route_decided for ready, …).
func (f *stateFuzz) hasCause(t *testing.T, e fuzzEvent) bool {
	t.Helper()
	for _, k := range []string{"reason", "cause"} {
		if v, ok := e.body[k].(string); ok && v != "" {
			return true
		}
	}
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE txid = $1 AND event_id <> $2
	  AND event_type NOT LIKE '%\_state\_changed'`, e.txid, e.id).Scan(&n))
	return n > 0
}

func typesOf(evs []fuzzEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.typ
	}
	return out
}

// round sends n bodies to every route from every caller.
func (f *stateFuzz) round(t *testing.T, s *httptest.Server, routes []fuzzRoute, what string) {
	t.Helper()
	for _, rt := range routes {
		n := f.n
		if rt.method != http.MethodGet {
			n *= 2
		}
		for _, caller := range fuzzCallers {
			for i := 0; i < n; i++ {
				path := f.path(t, rt.pattern)
				body, header := f.body(t, rt, caller, path)
				if b, ok := body.([]byte); ok && rt.method == http.MethodPut && f.r.Intn(2) == 0 {
					path = "artifacts/" + artifactID(b) // under its own hash, so the upload is weighed on what it is
				}
				f.route = rt.method + " " + rt.pattern
				before, since := f.snapshot(t), f.lastEvent(t)
				code, _ := call(t, s, caller, rt.method, path, body, append([]string{session, f.pick([]string{"s1", "s2", "fuzz"})}, header...)...)
				f.requests++
				f.check(t, fmt.Sprintf("round %q: %s %s as %q (%d), seed %d", what, rt.method, path, caller, code, f.seed), code, before, since)
			}
		}
	}
}

func (f *stateFuzz) pick(xs []string) string { return xs[f.r.Intn(len(xs))] }

// path fills a route's parameters with ids of the world and some that are not.
func (f *stateFuzz) path(t *testing.T, pattern string) string {
	t.Helper()
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if !strings.HasPrefix(p, "{") {
			continue
		}
		var pool []string
		switch p {
		case "{action}":
			pool = slices.Concat(fuzzAttemptActions, fuzzAttemptActions[:8]) // the routes' own, twice
		case "{rule_id}":
			pool = []string{"standing-envelope:dev", "job:implementer", "capability-facts", "card-auto-answer", "routing:dev", "x"}
		case "{job}":
			pool = []string{"chief", "implementer", "x"}
		default:
			switch parts[i-1] {
			case "attempts":
				pool = f.ids(t, `SELECT attempt_id FROM agentrt.attempt`)
			case "missions":
				pool = append(f.ids(t, `SELECT mission_id FROM agentrt.mission`), "M-99")
			case "tasks":
				pool = f.ids(t, `SELECT task_id FROM agentrt.task`)
			case "plan":
				pool = f.ids(t, `SELECT plan_node_id FROM agentrt.plan_node`)
			case "controls":
				pool = f.ids(t, `SELECT control_id FROM agentrt.control`)
			case "hosts":
				pool = []string{"h1", "h2", "h9"}
			case "pools":
				pool = f.ids(t, `SELECT vendor_pool_id FROM agentrt.vendor_pool`)
			case "artifacts":
				pool = f.ids(t, `SELECT artifact_id FROM agentrt.artifact`)
			}
		}
		// The world's ids three times over: most requests name a real row.
		pool = append(slices.Concat(pool, pool, pool), "x_nope", strings.Repeat("9", 40))
		parts[i] = f.pick(pool)
	}
	return strings.Join(parts, "/")
}

// value is a random value: a state, an id of the world, a number, a bool,
// text, nothing, or a list or object of those.
func (f *stateFuzz) value(t *testing.T, depth int) any {
	t.Helper()
	switch f.r.Intn(9) {
	case 0, 1:
		return f.pick(fuzzStates)
	case 2:
		ids := f.ids(t, `SELECT task_id FROM agentrt.task UNION ALL SELECT attempt_id FROM agentrt.attempt
		  UNION ALL SELECT mission_id FROM agentrt.mission UNION ALL SELECT plan_node_id FROM agentrt.plan_node`)
		return f.pick(append(ids, "h1", "h2", "owner", "controller"))
	case 3:
		return []int64{0, 1, -1, 2, 999, 1 << 40}[f.r.Intn(6)]
	case 4:
		return f.r.Intn(2) == 0
	case 5:
		return nil
	case 6:
		if depth > 0 {
			return f.pick([]string{"", "x", "../../etc", "'; DROP TABLE agentrt.task; --"})
		}
		return []any{f.value(t, depth+1), f.value(t, depth+1)}
	case 7:
		if depth > 0 {
			return "y"
		}
		return map[string]any{f.pick(fuzzStateKeys): f.pick(fuzzStates), f.pick(f.fields): f.value(t, depth+1)}
	default:
		return f.pick([]string{"", "x", "exec:cursor", "cursor-mix", pushedAt, commitA})
	}
}

// body is a request body for the route: nothing, bytes that are not one
// object, an object of random fields, or the route's own fields as a client
// sends them with state words beside them or in their place.
func (f *stateFuzz) body(t *testing.T, rt fuzzRoute, caller, path string) (any, []string) {
	t.Helper()
	header := []string{}
	if rt.method == http.MethodPut {
		header = append(header, "X-Artifact-Kind", f.pick([]string{"report", "log_tail", "task_spec", "verify", "x"}))
	}
	switch f.r.Intn(5) {
	case 0:
		return nil, header
	case 1:
		return []byte(f.pick([]string{`[]`, `"state"`, `{"state":`, `null`, `{"state":"accepted"}`, `{"to":"accepted","task_id":1}`})), header
	case 2:
		b := map[string]any{}
		for i := 0; i < 1+f.r.Intn(6); i++ {
			b[f.pick(f.fields)] = f.value(t, 0)
		}
		return b, header
	}
	b := f.template(t, rt, caller, path)
	switch f.r.Intn(3) {
	case 0: // as a client sends it
	case 1: // a state word beside the route's own fields
		for i := 0; i < 1+f.r.Intn(2); i++ {
			b[f.pick(fuzzStateKeys)] = f.pick(fuzzStates)
		}
	default: // one of its own fields made a state word, an id or noise
		own := make([]string, 0, len(b))
		for k := range b {
			own = append(own, k)
		}
		sort.Strings(own)
		b[f.pick(own)] = f.value(t, 0)
	}
	if rt.method == http.MethodPut {
		doc, err := json.Marshal(b)
		must(t, err)
		return doc, header
	}
	return b, header
}

// template is what a client of the route sends.
func (f *stateFuzz) template(t *testing.T, rt fuzzRoute, caller, path string) map[string]any {
	t.Helper()
	host := strings.TrimPrefix(caller, "agentd:")
	if host == caller {
		host = f.pick([]string{"h1", "h2"})
	}
	key := fmt.Sprintf("k%d", f.r.Intn(4))
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	switch {
	case rt.pattern == "claim":
		return map[string]any{"host_id": host, "free_slots": 1, "tools": []any{cursor}}
	case strings.HasPrefix(rt.pattern, "attempts/"):
		parts := strings.Split(path, "/")
		var epoch int64
		var ref string
		_ = f.pool.QueryRow(ctx, `SELECT lease_epoch, COALESCE(pushed_ref, '') FROM agentrt.attempt WHERE attempt_id = $1`, parts[1]).Scan(&epoch, &ref)
		arts := f.ids(t, `SELECT artifact_id FROM agentrt.artifact`)
		return map[string]any{"host_id": host, "lease_epoch": epoch, "model_provider": "cursor-mix", "checkpoint_seq": 1 + f.r.Intn(3),
			"artifacts": []string{f.pick(arts)}, "exit_code": 0, "end_how": f.pick([]string{"exit", "daemon_restart", "lease_lost"}),
			"conclusion": f.pick([]string{"done", "failed"}), "report": f.pick(arts), "pushed_ref": ref, "pushed_commit": pushedAt,
			"group_cleared": true}
	case rt.pattern == "missions":
		return map[string]any{"idempotency_key": key, "title": "m", "goal": "g", "success": []string{"it works"},
			"budget": map[string]any{"max_attempts": 4, "max_wall_hours": 2}}
	case rt.pattern == "missions/{id}/tasks":
		specs := f.ids(t, `SELECT artifact_id FROM agentrt.artifact WHERE artifact_kind = 'task_spec'`)
		return map[string]any{"idempotency_key": key, "tasks": []any{map[string]any{"ref": "a", "plan_node_id": "R2:1-1", "role": "implement",
			"task_class": "go", "title": "a", "spec_artifact_id": f.pick(specs), "effect_class": "branch", "repo": "bifrost-platform",
			"base_commit": commitA}}}
	case rt.pattern == "plan/proposals":
		return map[string]any{"idempotency_key": key, "root": "R2", "reason": "fuzz", "changes": []any{map[string]any{"op": "add_node",
			"parent": "R2:1", "kind": "step", "step_kind": "agent", "title": "next", "mission_id": "M-1"}}}
	case rt.pattern == "controls":
		tasks := f.ids(t, `SELECT task_id FROM agentrt.task`)
		return map[string]any{"idempotency_key": key, "scope": f.pick([]string{"task", "mission", "global", "host"}),
			"scope_ref": f.pick(append(tasks, "M-1", "h1")), "kind": f.pick([]string{"stop", "pause"}), "reason": "fuzz"}
	case rt.pattern == "goals":
		return map[string]any{"idempotency_key": key, "goal": "do this", "repo": "bifrost-platform"}
	case strings.HasPrefix(rt.pattern, "rules/"):
		text := "rule_id: " + strings.Split(path, "/")[1] + "\nrule_version: 1\ndirection: " + f.pick([]string{"initial", "tighten", "loosen"}) +
			"\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2027-01-15T00:00:00Z\ntenant: dev\nmax_action: branch\n" +
			"budget: {max_attempts: 4, max_wall_hours: 48}\nexecutors: [exec:cursor]\nhosts: [h1]\n"
		switch {
		case strings.HasSuffix(rt.pattern, "/sign"):
			return map[string]any{"text": text, "signature": "-----BEGIN SSH SIGNATURE-----\nAAAA\n-----END SSH SIGNATURE-----\n"}
		case strings.HasSuffix(rt.pattern, "/proposals"):
			return map[string]any{"idempotency_key": key, "text": text, "reason": "fuzz"}
		}
		return map[string]any{"text": text}
	case rt.pattern == "messages":
		return map[string]any{"to": f.pick([]string{"seat:chief_of_staff", "owner"}), "kind": f.pick([]string{"ask", "note", "answer"}), "body": "hi"}
	case rt.pattern == "messages/ack":
		return map[string]any{"message_ids": f.ids(t, `SELECT message_id FROM agentrt.message`)}
	}
	return map[string]any{"idempotency_key": key, "reason": "fuzz"}
}
