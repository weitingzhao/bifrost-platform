package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7 goal 2 (store/landing_batch.go): a batch of operations runs one
// landing CI. Its record is a controller note beside the protocol, checked
// when written; a red batch sends back the members it blames and lets the
// others land later, or is split; the batch gate writes every member's
// intent or none.

// batchRoots are n roots of M-3 on liveWorld, each accepted on submission
// (its own host), the clock 30 minutes before at.
func batchRoots(t *testing.T, n int) (*world, []string, time.Time) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	w := liveWorld(t, "none", at)
	var roots []string
	for i := range n {
		store.FreezeEventClock(at.Add(-40*time.Minute + time.Duration(i)*time.Second))
		root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: fmt.Sprintf("batch %d", i), BaseCommit: commitB})
		if c := w.run(t, fmt.Sprintf("h%d", i+1), cursor, "done"); c.TaskID != root {
			t.Fatalf("claimed %s, not %s", c.TaskID, root)
		}
		if _, accepted, err := store.StartVerificationWith(ctx, w.rt, root, store.ConcludeOptions{}); err != nil || !accepted {
			t.Fatal("not accepted on submission", err)
		}
		roots = append(roots, root)
	}
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	return w, roots, at
}

// batchClaims claims roots for one batch, in order.
func batchClaims(t *testing.T, w *world, roots []string, at time.Time) []string {
	t.Helper()
	var ops []string
	for _, root := range roots {
		op, d, err := store.ClaimLandingInBatch(ctx, w.rt, c4Source(precheckPath), "prod", root, at, ops)
		ok(t, err)
		if !d.Allowed || op == "" {
			t.Fatal("batch claim", root, d)
		}
		ops = append(ops, op)
	}
	return ops
}

func batchOf(ops []string, at time.Time) store.LandingBatch {
	return store.LandingBatch{Batch: landing.BatchID(ops[0]), Lead: ops[0], Members: ops, M0: commitA, R: commitB, CommitterDate: at}
}

// batchWorld is batchRoots' roots claimed, recorded as one batch on M0
// commitA with R commitB, and prepared; every member's branch pushed but the
// last's when last is false.
func batchWorld(t *testing.T, n int, last bool) (*world, []string, []string, time.Time) {
	t.Helper()
	w, roots, at := batchRoots(t, n)
	ops := batchClaims(t, w, roots, at)
	ok(t, store.AppendLandingBatch(ctx, w.rt, "bifrost-platform", batchOf(ops, at.Add(-30*time.Minute))))
	for i, op := range ops {
		c4Prepare(t, w, roots[i], op, at.Add(-29*time.Minute), last || i < len(ops)-1)
	}
	store.FreezeEventClock(at.Add(-20 * time.Minute))
	return w, roots, ops, at
}

func batchNote(w *world, actor string, body map[string]any) error {
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: actor, Body: body})
		return err
	})
}

func batchBody(b store.LandingBatch) map[string]any {
	members := []any{}
	for _, m := range b.Members {
		members = append(members, m)
	}
	return map[string]any{"text": "batch", "landing_batch": map[string]any{"v": "1", "batch": b.Batch, "lead": b.Lead, "members": members,
		"M0": b.M0, "R": b.R, "committer_date": b.CommitterDate.UTC().Format(time.RFC3339Nano), "after": b.After}}
}

// Serial: FreezeEventClock. The claims of one batch do not count each
// other in flight (the rule allows one); the note is refused, with nothing
// written, unless its members are each root's latest operation, claimed or
// prepared with its M0 and R, in no other batch, after names a split red
// batch, and the controller writes it alone; the fold does not read it, and
// with it a member's facts count no mate in flight, while a root outside
// the batch is refused for the limit.
func TestBatchNoteAppendCheck(t *testing.T) {
	w, roots, at := batchRoots(t, 4)
	if op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", roots[0], at); err != nil || op == "" {
		t.Fatal("first claim", d, err)
	}
	if op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", roots[1], at); err != nil || op != "" || d.Reason != "limit" {
		t.Fatal("a second claim outside a batch is not refused for the limit", op, d, err)
	}
	ops := []string{"land:" + roots[0] + ":1"}
	for _, root := range roots[1:3] {
		op, d, err := store.ClaimLandingInBatch(ctx, w.rt, c4Source(precheckPath), "prod", root, at, ops)
		ok(t, err)
		if op == "" {
			t.Fatal("a batch claim refused", d)
		}
		ops = append(ops, op)
	}
	before := landingState(t, w, landingRepoStream)
	date := at.Add(-30 * time.Minute)
	good := batchOf(ops, date)
	refused := map[string]error{}
	one := good
	one.Members, one.Batch, one.Lead = ops[:1], landing.BatchID(ops[0]), ops[0]
	refused["one member"] = batchNote(w, "controller", batchBody(one))
	unknown := good
	unknown.Members = []string{ops[0], "land:t_00000000000000000000000099:1"}
	refused["not an operation"] = batchNote(w, "controller", batchBody(unknown))
	lead := good
	lead.Lead = ops[1]
	refused["lead not first"] = batchNote(w, "controller", batchBody(lead))
	after := good
	after.After = "batch:land:nothing:1"
	refused["after no red batch"] = batchNote(w, "controller", batchBody(after))
	refused["the owner's"] = batchNote(w, "owner", batchBody(good))
	extra := batchBody(good)
	extra["landing_refusal"] = map[string]any{}
	refused["another record"] = batchNote(w, "controller", extra)
	// A member prepared on another R.
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(ops[2], "prepared", "R", commitB, "M0", commitA,
		"committer_date", date.Format(time.RFC3339Nano), "dry_run", false)))
	other := good
	other.R = strings.Repeat("e", 40)
	refused["prepared on another R"] = batchNote(w, "controller", batchBody(other))
	for name, err := range refused {
		if err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if n := rwNotes(t, w, "landing_batch"); n != 0 {
		t.Fatal("a refused note was written", n)
	}
	store.FreezeEventClock(at.Add(-28 * time.Minute))
	ok(t, store.AppendLandingBatch(ctx, w.rt, "bifrost-platform", good))
	again := batchOf([]string{ops[1], ops[0]}, date)
	if err := batchNote(w, "controller", batchBody(again)); !errors.Is(err, store.ErrBatchRefused) {
		t.Fatal("a member in two batches", err)
	}
	after2 := landingState(t, w, landingRepoStream)
	for id, o := range before.Operations {
		if id != ops[2] && after2.Operations[id].Phase != o.Phase {
			t.Fatal("the note moved an operation", id)
		}
	}
	b, found, err := store.LandingBatchOf(ctx, w.rt, "bifrost-platform", ops[1])
	ok(t, err)
	if !found || b.Batch != good.Batch || !slices.Equal(b.Members, ops) || b.R != commitB || !b.CommitterDate.Equal(date) {
		t.Fatalf("read back: %+v", b)
	}
	open, err := store.OpenLandingBatches(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if len(open) != 1 || open[0].Batch != good.Batch {
		t.Fatal("open batches", open)
	}
	var f landing.Facts
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var e error
		f, e = store.LandingFacts(ctx, tx, c4Source(precheckPath), "prod", "live", roots[1], ops[1], "", at)
		return e
	}))
	if f.OtherInFlight != 0 {
		t.Fatal("a mate counted in flight", f.OtherInFlight)
	}
	if op, d, err := store.ClaimLanding(ctx, w.rt, c4Source(precheckPath), "prod", roots[3], at); err != nil || op != "" || d.Reason != "limit" {
		t.Fatal("a root outside the batch is not refused for the limit", op, d, err)
	}
}

// Serial: FreezeEventClock. A red batch blaming one member: that one goes
// back for its next round (no card), the others fail ci_red and are retried
// by the controller (no card), and no half waits. Recorded once.
func TestBatchRedBlamedAndOthers(t *testing.T) {
	w, roots, ops, at := batchWorld(t, 3, true)
	reworked, err := store.RecordBatchRed(ctx, w.rt, "bifrost-platform", landing.BatchID(ops[0]), redCI, []string{ops[1]}, "test: failed", store.PrecheckCI)
	ok(t, err)
	if !slices.Equal(reworked, []string{ops[1]}) || w.state(t, "task", roots[1]) != "ready" {
		t.Fatal("blamed not reworked", reworked, w.state(t, "task", roots[1]))
	}
	s := landingState(t, w, landingRepoStream)
	for i, op := range ops {
		o := s.Operations[op]
		if o.Phase != "failed" || o.Outcome != "ci_red" || o.Retried != (i != 1) {
			t.Fatalf("%s: %s %s retried %v", op, o.Phase, o.Outcome, o.Retried)
		}
		if choices := rootFailed(t, w, roots[i], at); choices != nil {
			t.Fatal("a card for", op, choices)
		}
	}
	if i := slices.Index(verificationsOf(t, w, roots[1]), "landing_ci/fail"); i < 0 {
		t.Fatal("the failure is not attached to the blamed root")
	}
	if v := verificationsOf(t, w, roots[0]); slices.Contains(v, "landing_ci/fail") {
		t.Fatal("an unblamed root was told it failed", v)
	}
	half, red, err := store.PendingBatchHalf(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if half != nil || red != "" {
		t.Fatal("a half waits after blame", half, red)
	}
	again, err := store.RecordBatchRed(ctx, w.rt, "bifrost-platform", landing.BatchID(ops[0]), redCI, nil, "test: failed", store.PrecheckCI)
	ok(t, err)
	if again != nil || rwNotes(t, w, "landing_batch_red") != 1 {
		t.Fatal("recorded twice")
	}
	// The unblamed roots claim again, free, and may form the next batch.
	store.FreezeEventClock(at.Add(-15 * time.Minute))
	next := batchClaims(t, w, []string{roots[0], roots[2]}, at)
	ok(t, store.AppendLandingBatch(ctx, w.rt, "bifrost-platform", batchOf(next, at.Add(-15*time.Minute))))
}

// Serial: FreezeEventClock. A red batch blaming none: every member fails
// ci_red and is retried, no card; its halves succeed it, first half first:
// the first half's batch names it as after, then the second half waits.
// The red note itself is checked: not twice, halves as Split's.
func TestBatchRedSplitsWhenNothingMaps(t *testing.T) {
	w, roots, ops, at := batchWorld(t, 3, true)
	id := landing.BatchID(ops[0])
	if err := batchNote(w, "controller", map[string]any{"text": "red", "landing_batch_red": map[string]any{"v": "1", "batch": id,
		"blamed": []any{}, "halves": []any{[]any{ops[0]}, []any{ops[1], ops[2]}}}}); !errors.Is(err, store.ErrBatchRefused) {
		t.Fatal("halves not Split's", err)
	}
	reworked, err := store.RecordBatchRed(ctx, w.rt, "bifrost-platform", id, landing.CIFailure{Run: "r", ReadError: "forbidden"}, nil, "test: failed", store.PrecheckCI)
	ok(t, err)
	if len(reworked) != 0 {
		t.Fatal("reworked with no blame", reworked)
	}
	s := landingState(t, w, landingRepoStream)
	for i, op := range ops {
		if o := s.Operations[op]; o.Phase != "failed" || o.Outcome != "ci_red" || !o.Retried || w.state(t, "task", roots[i]) != "accepted" {
			t.Fatalf("%s: %s %s retried %v", op, o.Phase, o.Outcome, o.Retried)
		}
		if choices := rootFailed(t, w, roots[i], at); choices != nil {
			t.Fatal("a card for", op, choices)
		}
	}
	if err := batchNote(w, "controller", map[string]any{"text": "red", "landing_batch_red": map[string]any{"v": "1", "batch": id,
		"blamed": []any{}, "halves": []any{[]any{ops[0], ops[1]}, []any{ops[2]}}}}); !errors.Is(err, store.ErrBatchRefused) {
		t.Fatal("red twice", err)
	}
	half, red, err := store.PendingBatchHalf(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if !slices.Equal(half, roots[:2]) || red != id {
		t.Fatal("first half", half, red)
	}
	store.FreezeEventClock(at.Add(-15 * time.Minute))
	next := batchClaims(t, w, half, at)
	b := batchOf(next, at.Add(-15*time.Minute))
	b.After = red
	ok(t, store.AppendLandingBatch(ctx, w.rt, "bifrost-platform", b))
	half, red, err = store.PendingBatchHalf(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if !slices.Equal(half, roots[2:]) || red != id {
		t.Fatal("second half", half, red)
	}
}

func batchObserved(prepared time.Time) func(context.Context, landing.Operation) (store.LiveObservation, error) {
	return func(context.Context, landing.Operation) (store.LiveObservation, error) {
		return store.LiveObservation{Main: commitA, CI: landing.CIRun{Run: "batch-green", R: commitB, Status: "Succeeded", CreatedAt: prepared.Add(time.Minute)}}, nil
	}
}

// Serial: FreezeEventClock. The batch gate is all or nothing: a member not
// ready refuses the batch, naming it, and nothing is written, no intent;
// once every member is ready, every member's main_intent on one R and M0, each resting on the one green run; the
// reconciler lands each, as any operation.
func TestBatchGateAllOrNothing(t *testing.T) {
	w, roots, ops, at := batchWorld(t, 3, false)
	store.FreezeEventClock(at.Add(-10 * time.Minute))
	auths, d, err := store.AuthorizeBatchPush(ctx, w.rt, c4Source(precheckPath), ops, batchObserved(at.Add(-29*time.Minute)))
	ok(t, err)
	if d.Allowed || auths != nil || d.Detail["op"] != ops[2] {
		t.Fatalf("a batch with a member not ready: %+v %v", d, auths)
	}
	for _, op := range ops {
		if p, _ := phaseOf(t, w, op); p == "main_intent" {
			t.Fatal("an intent on a refusal", op)
		}
	}
	if auths, d, err := store.AuthorizeBatchPush(ctx, w.rt, c4Source(precheckPath), ops[1:], batchObserved(at.Add(-29*time.Minute))); err != nil || d.Allowed || auths != nil {
		t.Fatal("authorized operations that are not a batch", d, err)
	}
	parts := strings.Split(ops[2], ":")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(ops[2], "branch_pushed", "ref", "refs/heads/land/prod/"+roots[2]+"/"+parts[len(parts)-1], "R", commitB)))
	auths, d, err = store.AuthorizeBatchPush(ctx, w.rt, c4Source(precheckPath), ops, batchObserved(at.Add(-29*time.Minute)))
	ok(t, err)
	if !d.Allowed || len(auths) != 3 {
		t.Fatalf("not authorized: %+v", d)
	}
	for i, a := range auths {
		if a.Op != ops[i] || a.R != commitB || a.M0 != commitA || a.Root != roots[i] {
			t.Fatalf("authorization %d: %+v", i, a)
		}
		if p, _ := phaseOf(t, w, ops[i]); p != "main_intent" {
			t.Fatal("no intent", ops[i], p)
		}
		current, err := store.AuthorizationCurrent(ctx, w.rt, a)
		ok(t, err)
		if !current {
			t.Fatal("not current", a.Op)
		}
	}
	var runs []string
	rows, err := w.rt.Query(ctx, `SELECT body_json->'landing_gate'->>'ci_run' FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_gate' ORDER BY seq`, landingRepoStream)
	ok(t, err)
	runs, err = pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if !slices.Equal(runs, []string{"batch-green", "batch-green", "batch-green"}) {
		t.Fatal("intents rest on", runs)
	}
	for _, a := range auths {
		wrote, err := store.RecordPushResult(ctx, w.rt, a, "ok", "")
		ok(t, err)
		if !wrote {
			t.Fatal("push result not recorded", a.Op)
		}
	}
	effectiveRead(t, w, at.Add(-5*time.Minute))
	for _, op := range ops {
		if p, _ := phaseOf(t, w, op); p != "landed" {
			t.Fatal("not landed", op, p)
		}
	}
}
