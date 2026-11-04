package store_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.5-19 on PostgreSQL: what keeps an active step from done, read from
// the same records and the same final classification as its rule's value.

// exActive puts step in progress with its state event.
func exActive(t *testing.T, w *world, step string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", root, step, "active", "test")
		return err
	}))
}

// exSteps is root R2's explanation now.
func exSteps(t *testing.T, w *world) map[string]any {
	t.Helper()
	steps, at, err := store.ExplainRoot(ctx, w.rt, root)
	ok(t, err)
	if at.IsZero() {
		t.Fatal("no read time")
	}
	return steps
}

func exStep(t *testing.T, steps map[string]any, id string) map[string]any {
	t.Helper()
	s, _ := steps[id].(map[string]any)
	if s == nil {
		t.Fatalf("no result for %s in %v", id, steps)
	}
	return s
}

// exLanded is the step_landed blockers of a result, by task, each checked
// against §7's contract.
func exLanded(t *testing.T, s map[string]any) map[string]store.Blocker {
	t.Helper()
	out := map[string]store.Blocker{}
	for _, b := range s["blockers"].([]store.Blocker) {
		if p := store.ExplainContractProblem(b); p != "" {
			t.Fatalf("contract: %s in %v", p, b)
		}
		if b["atom"] == plan.AtomStepLanded {
			id, _ := b["task_id"].(string)
			out[id] = b
		}
	}
	return out
}

// kleeneAll is all's value over these.
func kleeneAll(vs ...plan.Tri) string {
	out := plan.True
	for _, v := range vs {
		switch v {
		case plan.False:
			return plan.False.String()
		case plan.Unknown:
			out = plan.Unknown
		}
	}
	return out.String()
}

// Serial: FreezeEventClock. Each root's state from landing()'s own final
// classification: in the first-claim handoff, on its way, declined, failed,
// landed (no blocker), reverted. The rule's value is Eval's over the same
// atoms; STG waits for landing. Every other step gets its own result.
func TestExplainRootLandingStates(t *testing.T) {
	w, c := dbWorld(t)
	unclaimed := dbAccepted(t, w, c, s2, "bifrost-platform")
	pending := dbAccepted(t, w, c, s2, "bifrost-platform")
	declined := dbAccepted(t, w, c, s2, "bifrost-platform")
	failed := dbAccepted(t, w, c, s2, "bifrost-platform")
	landed := dbAccepted(t, w, c, s2, "bifrost-platform")
	reverted := dbAccepted(t, w, c, s2, "bifrost-platform")
	pendingOp := dbOp(t, w, c, pending, dbOldA, "intent")
	declinedOp := dbOp(t, w, c, declined, dbNewA, "declined")
	failedOp := dbOp(t, w, c, failed, dbThirdA, "overlap")
	dbOp(t, w, c, landed, rwA1, "landed")
	revertedOp := dbOp(t, w, c, reverted, rwA2, "reverted")
	short := w.ready(t, store.Task{MissionID: "M-3", PlanNode: s2, BaseCommit: commitB}) // a task step_tasks still waits on
	c.dbTick()

	w.rule(t, s2, `{"all":[{"step_tasks":{"state":"accepted","min":9}},{"step_landed":{}},{"step_stg_settled":{}}]}`)
	exActive(t, w, s2)
	w.rule(t, s1, `{"step_landed":{}}`) // planned
	exActive(t, w, s3)                  // no rule
	w.rule(t, dbStep, `{"any":[{"step_landed":{}},{"owner_signoff":"TD-1"}]}`)
	exActive(t, w, dbStep)

	steps := exSteps(t, w)
	s := exStep(t, steps, s2)
	ev := stepEvidence(t, w, s2, "accepted", 9)
	if s["kind"] != "explained" || s["rule"] != kleeneAll(ev.Tasks, ev.Landed, ev.STG) || s["rule"] != "false" {
		t.Fatalf("s2: %v beside evidence %+v", s, ev)
	}
	bs := s["blockers"].([]store.Blocker)
	cands := bs[0]["candidates"].([]map[string]any)
	if bs[0]["state"] != "tasks_short" || bs[0]["have"] != int64(6) || bs[0]["need"] != int64(9) || len(cands) != 1 ||
		cands[0]["task_id"] != short || cands[0]["task_state"] != "ready" {
		t.Fatalf("tasks: %v", bs[0])
	}
	// R5:5.5-20: each candidate says since when it is in its state: the time of the move that put it there.
	var entered time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed' AND body_json->>'to' = 'ready'
	  ORDER BY event_id DESC LIMIT 1`, short).Scan(&entered))
	since, _ := cands[0]["since"].(string)
	if got, err := time.Parse(time.RFC3339Nano, since); err != nil || !got.Equal(entered) {
		t.Fatalf("a candidate's since %q, want %s (its move to ready): %v", since, entered, err)
	}
	// A change of the task's record that moves no state (planchange.go rebind_step and move_task set updated_at)
	// leaves its since as it was.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: a record change with no move"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET updated_at = updated_at + interval '2 days', version = version + 1 WHERE task_id = $1`, short)
		return err
	}))
	again := exStep(t, exSteps(t, w), s2)["blockers"].([]store.Blocker)[0]["candidates"].([]map[string]any)
	if again[0]["since"] != since {
		t.Fatalf("a record change with no move moved since: %v, was %s", again[0]["since"], since)
	}
	if last := bs[len(bs)-1]; last["state"] != "stg_needs_landing" {
		t.Fatalf("STG before landing: %v", last)
	}
	got := exLanded(t, s)
	if len(got) != 5 || got[landed] != nil {
		t.Fatalf("landed blockers %v: one each but the landed root", got)
	}
	if b := got[unclaimed]; b["state"] != "landing_unclaimed" || b["accepted_at"] == nil || b["op"] != nil {
		t.Fatalf("unclaimed: %v", b)
	}
	if b := got[pending]; b["state"] != "landing_pending" || b["op"] != pendingOp || b["phase"] != "branch_intent" || b["last_record_at"] == nil ||
		b["grant"] != nil || b["cards"] == nil {
		t.Fatalf("pending: %v", b)
	}
	wantRework := map[string]any{"seen": nil, "not_asked": false, "card": nil}
	if b := got[declined]; b["state"] != "landing_declined" || b["op"] != declinedOp || !reflect.DeepEqual(b["rework"], wantRework) {
		t.Fatalf("declined with no rework record: %v", b)
	}
	if b := got[failed]; b["state"] != "landing_failed" || b["op"] != failedOp || b["outcome"] != "overlap" {
		t.Fatalf("failed: %v", b)
	}
	if b := got[reverted]; b["state"] != "landing_failed" || b["op"] != revertedOp || b["outcome"] != "reverted" {
		t.Fatalf("reverted: %v", b)
	}
	// R5:5.5-20: an ended root says since when: the time of its operation's last record.
	for _, id := range []string{declined, failed, reverted} {
		if since, _ := got[id]["since"].(string); since == "" {
			t.Errorf("%s ended with no since: %v", got[id]["state"], got[id])
		}
	}
	for id, want := range map[string]map[string]any{
		s1:     {"kind": "not_started"},
		s3:     {"kind": "no_done_rule"},
		dbStep: {"kind": "rule_not_supported", "atoms": []string{"owner_signoff"}},
	} {
		r := exStep(t, steps, id)
		delete(r, "node_version")
		if !reflect.DeepEqual(r, want) {
			t.Errorf("%s: %v, want %v", id, r, want)
		}
	}
	if _, has := steps[item]; has {
		t.Errorf("an item has a result: %v", steps[item])
	}
	var version int64
	ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.plan_node WHERE plan_node_id = $1`, s2).Scan(&version))
	if s["node_version"] != version {
		t.Errorf("node_version %v, want %d", s["node_version"], version)
	}

	// Bounds: a root over its step bound, its rows bound or its tasks bound.
	lim := store.DefaultExplainBounds()
	lim.Steps = 0
	capped, _, err := store.ExplainRootWithin(ctx, w.rt, root, lim)
	ok(t, err)
	if r := exStep(t, capped, s2); r["kind"] != "not_explained" || r["reason"] != "over_cap" {
		t.Fatalf("over the step bound: %v", r)
	}
	for which, set := range map[string]func(*store.ExplainBounds){
		"rows":  func(l *store.ExplainBounds) { l.Rows = 1 },
		"bytes": func(l *store.ExplainBounds) { l.Bytes = 1 },
		"tasks": func(l *store.ExplainBounds) { l.Tasks = 1 },
	} {
		lim := store.DefaultExplainBounds()
		set(&lim)
		_, _, err := store.ExplainRootWithin(ctx, w.rt, root, lim)
		var inc *store.ErrExplainIncomplete
		if !errors.As(err, &inc) || inc.Reason != "input_too_large" || inc.Which != which {
			t.Errorf("%s over its bound: %v", which, err)
		}
	}
	if _, _, err := store.ExplainRoot(ctx, w.rt, "R404"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("no such root: %v", err)
	}
	// A deadline cancels the connection's query: on a connection of its own.
	lim = store.DefaultExplainBounds()
	lim.Deadline = time.Nanosecond
	_, _, err = store.ExplainRootWithin(ctx, w.db.Connect(t, w.db.Runtime), root, lim)
	var inc *store.ErrExplainIncomplete
	if !errors.As(err, &inc) || inc.Reason != "timeout" {
		t.Errorf("past its deadline: %v", err)
	}
}

// Serial: FreezeEventClock. A declined root's rework records (§4.3), as the
// runtime writes them: none; the set recorded (current); its card bound;
// the Owner's still_owed handled. Then count_replaced on a new world: the
// root no longer blocks, and a step whose rule holds reads true with no
// blocker while its record is still in progress.
func TestExplainReworkRecords(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	r.w.rule(t, s2, `{"step_landed":{}}`)
	exActive(t, r.w, s2)
	rework := func(why string) map[string]any {
		t.Helper()
		b := exLanded(t, exStep(t, exSteps(t, r.w), s2))[r.root]
		if b["state"] != "landing_declined" {
			t.Fatalf("%s: %v", why, b)
		}
		return b["rework"].(map[string]any)
	}
	if rw := rework("nothing recorded"); rw["seen"] != nil || rw["card"] != nil || rw["not_asked"] != false {
		t.Fatalf("nothing recorded: %v", rw)
	}
	for range 3 {
		r.rwPass(t)
	}
	rw := rework("recorded")
	seen, _ := rw["seen"].(map[string]any)
	if seen == nil || seen["current"] != true || seen["trigger_seq"].(int64) <= 0 || seen["set_key"] == "" || rw["card"] != nil || rw["not_asked"] != false {
		t.Fatalf("recorded: %v", rw)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	rw = rework("asked")
	cm, _ := rw["card"].(map[string]any)
	if cm == nil || cm["prep"] != card.Prep || cm["question_id"] != card.QuestionID || cm["ask_started"] != true || cm["closed"] != false ||
		cm["n"] != card.N || cm["outcome"] != nil {
		t.Fatalf("asked: %v beside %+v", cm, card)
	}
	f.answer(card.QuestionID, letterOf(t, card, "still_owed"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))
	cm = rework("still owed")["card"].(map[string]any)
	out, _ := cm["outcome"].(map[string]any)
	if out == nil || out["seq"].(int64) <= 0 || out["outcome"] == nil {
		t.Fatalf("still owed: %v", cm)
	}

	q := newRWWorld(t, &deliveredGit{})
	q.w.rule(t, s2, `{"step_landed":{}}`)
	exActive(t, q.w, s2)
	for range 3 {
		q.rwPass(t)
	}
	g := newFakeCards(func() time.Time { return q.now })
	cardPass(t, q.w, g, q.now)
	qc := onlyCard(t, q.w, store.CardReworkLanded)
	g.answer(qc.QuestionID, letterOf(t, qc, "count_replaced"), q.now.Add(time.Minute))
	cardPass(t, q.w, g, q.now.Add(2*time.Minute))
	s := exStep(t, exSteps(t, q.w), s2)
	if s["kind"] != "explained" || s["rule"] != "true" || len(s["blockers"].([]store.Blocker)) != 0 {
		t.Fatalf("counted delivered: %v", s)
	}
	if q.w.stepState(t, s2) != "active" {
		t.Fatalf("fixture: the step moved before an evaluation")
	}
}

// countDB counts the statements an explanation sends; hold, when set, makes
// the statement numbered n wait that long first and then run to success as
// if nothing had stopped it (work that cannot be cancelled).
type countDB struct {
	conn *pgx.Conn
	n    atomic.Int64
	hold func(n int64) time.Duration
}

func (d *countDB) BeginTx(c context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.conn.BeginTx(c, o)
	if err != nil {
		return nil, err
	}
	return &countTx{Tx: tx, d: d}, nil
}

type countTx struct {
	pgx.Tx
	d *countDB
}

func (t *countTx) next(c context.Context) context.Context {
	n := t.d.n.Add(1)
	if t.d.hold != nil {
		if wait := t.d.hold(n); wait > 0 {
			time.Sleep(wait)
			return context.WithoutCancel(c)
		}
	}
	return c
}
func (t *countTx) Query(c context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.Tx.Query(t.next(c), sql, args...)
}
func (t *countTx) QueryRow(c context.Context, sql string, args ...any) pgx.Row {
	return t.Tx.QueryRow(t.next(c), sql, args...)
}
func (t *countTx) Exec(c context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.Tx.Exec(t.next(c), sql, args...)
}

// statements is how many statements one explanation within lim sends.
func (d *countDB) statements(t *testing.T, lim store.ExplainBounds) int64 {
	t.Helper()
	d.n.Store(0)
	_, _, err := store.ExplainRootWithin(ctx, d, root, lim)
	ok(t, err)
	return d.n.Load()
}

// Serial: FreezeEventClock. Review round 11 M3, M4, M5: a step past the
// step bound reads nothing; roots of steps not explained cost no statement
// (the card triggers and the batched reads cover the explained roots
// only); and work that runs past the deadline is not kept, even when its
// last read then succeeds.
func TestExplainRootReadsOnlyWhatItExplains(t *testing.T) {
	w, c := dbWorld(t)
	declined := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, declined, dbOldA, "declined")
	c.dbTick()
	w.rule(t, s2, `{"step_landed":{}}`)
	exActive(t, w, s2)
	d := &countDB{conn: w.db.Connect(t, w.db.Runtime)}
	none := store.DefaultExplainBounds()
	none.Steps = 0
	all := store.DefaultExplainBounds()
	base, one := d.statements(t, none), d.statements(t, all)
	if one <= base {
		t.Fatalf("explaining a step sent %d statements, no more than %d for none", one, base)
	}
	// Roots on a step not explained (dbStep stays planned), declined and landed.
	for i, end := range []string{"declined", "declined", "landed", "intent"} {
		r := dbAccepted(t, w, c, dbStep, "bifrost-platform")
		dbOp(t, w, c, r, []string{rwA1, rwA2, rwA3, dbThirdA}[i], end)
	}
	c.dbTick()
	if got := d.statements(t, all); got != one {
		t.Fatalf("roots of a step not explained changed the statements: %d, was %d", got, one)
	}
	if got := d.statements(t, none); got != base {
		t.Fatalf("a step past the bound read: %d statements, was %d", got, base)
	}
	// The last statement runs past the deadline and then succeeds: the
	// result is dropped as a timeout, never kept.
	lim := store.DefaultExplainBounds()
	lim.Deadline = 2 * time.Second
	d.n.Store(0)
	d.hold = func(n int64) time.Duration {
		if n == one {
			return 2500 * time.Millisecond
		}
		return 0
	}
	_, _, err := store.ExplainRootWithin(ctx, d, root, lim)
	var inc *store.ErrExplainIncomplete
	if !errors.As(err, &inc) || inc.Reason != "timeout" {
		t.Fatalf("past the deadline: %v", err)
	}
}

// Serial: FreezeEventClock. Review round 12 M2: the bytes bound counts the
// JSON the reads decode, not its stored size: a stream of notes whose long,
// repetitive text compresses well stays under the bound when stored and
// goes over it when read, and is refused before anything is read.
func TestExplainBytesBoundIsTheDecodedJSON(t *testing.T) {
	w, c := dbWorld(t)
	r := dbAccepted(t, w, c, s2, "bifrost-platform")
	dbOp(t, w, c, r, dbOldA, "intent")
	for range 8 {
		c.dbTick()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller",
				Body: map[string]any{"text": strings.Repeat("rework notes ", 20_000)}})
			return err
		}))
	}
	w.rule(t, s2, `{"step_landed":{}}`)
	exActive(t, w, s2)
	var stored, decoded int64
	ok(t, w.rt.QueryRow(ctx, `SELECT sum(pg_column_size(body_json)), sum(octet_length(body_json::text)) FROM agentrt.event
	  WHERE event_stream_id = ANY($1)`, []string{landingRepoStream, store.CardsStream("bifrost-platform")}).Scan(&stored, &decoded))
	if stored*4 > decoded {
		t.Fatalf("fixture: stored %d, decoded %d: not compressed enough to tell them apart", stored, decoded)
	}
	lim := store.DefaultExplainBounds()
	lim.Bytes = (stored + decoded) / 2
	_, _, err := store.ExplainRootWithin(ctx, w.rt, root, lim)
	var inc *store.ErrExplainIncomplete
	if !errors.As(err, &inc) || inc.Reason != "input_too_large" || inc.Which != "bytes" {
		t.Fatalf("stored %d under the bound %d, decoded %d over it: %v", stored, lim.Bytes, decoded, err)
	}
	lim.Bytes = decoded + 1
	_, _, err = store.ExplainRootWithin(ctx, w.rt, root, lim)
	ok(t, err)
}
