package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Review 672: closing rounds keep their snapshot (B-01); a fence needs both
// enabled and the mode (B-02); the decision table row by row (R-01); a late
// reversal is told whatever the disposition (R-02); a park changes in one
// transaction (R-05); the status shows a park (R-07); a close keeps what it
// decided on (N-01).

// readOne reads main for one landing operation only.
func readOne(t *testing.T, w *world, op string, now time.Time, validity string) {
	t.Helper()
	store.FreezeEventClock(now)
	cands, err := store.MainCandidates(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	for _, c := range cands {
		if c.Op != op {
			continue
		}
		_, err := store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", c, store.MainEvidence{Head: commitB, HasR: true, Validity: validity, ReadAt: now})
		ok(t, err)
		return
	}
	t.Fatalf("%s is not a main candidate", op)
}

func closeRounds(t *testing.T, w *world) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'mission:M-3' AND body_json ? 'landing_close_round'`).Scan(&n))
	return n
}

// Serial: FreezeEventClock. B-01: two landed roots; each read in turn
// narrows the round, it does not reopen it; the mission closes in round 1,
// and the close keeps each root as decided (N-01).
func TestB4PartialEvidenceKeepsTheRound(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	a := liveRoot(t, w, "land:r672a:1", at, "main_intent", newLandingRecord("land:r672a:1", "main_result", "outcome", "ok"))
	liveRoot(t, w, "land:r672b:1", at, "main_intent", newLandingRecord("land:r672b:1", "main_result", "outcome", "ok"))
	effectiveRead(t, w, at.Add(-10*time.Minute)) // landed, before any round
	if st := closePass(t, w, at); st != "active" || closeRounds(t, w) != 1 {
		status, _ := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
		t.Fatalf("no first round %s: %+v", st, status)
	}
	readOne(t, w, "land:r672a:1", at.Add(time.Minute), "effective")
	if st := closePass(t, w, at.Add(2*time.Minute)); st != "active" || closeRounds(t, w) != 1 {
		t.Fatalf("one read reopened the round: %s, %d rounds", st, closeRounds(t, w))
	}
	readOne(t, w, "land:r672b:1", at.Add(3*time.Minute), "effective")
	if st := closePass(t, w, at.Add(4*time.Minute)); st != "done" || closeRounds(t, w) != 1 {
		t.Fatalf("not closed in its round: %s, %d rounds", st, closeRounds(t, w))
	}
	var lc map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing_close' FROM agentrt.event WHERE event_stream_id = 'mission:M-3' AND body_json ? 'landing_close'`).Scan(&lc))
	roots, _ := lc["roots"].([]any)
	if lc["to"] != "done" || lc["round"] != float64(1) || lc["delivered"] != float64(2) || len(roots) != 2 {
		t.Fatalf("close record: %v", lc)
	}
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if status.Closed == nil || status.Closed.To != "done" || status.Closed.Delivered != 2 || len(status.Closed.Roots) != 2 {
		t.Fatalf("status close: %+v", status.Closed)
	}
	_ = a
}

// Serial: FreezeEventClock. B-01: a root that joins the mission after the
// round opened changes the snapshot: a new round, whose reads count; the
// earlier ones do not.
func TestB4SnapshotChangeReopensTheRound(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r672c:1", at, "main_intent", newLandingRecord("land:r672c:1", "main_result", "outcome", "ok"))
	effectiveRead(t, w, at.Add(-10*time.Minute))
	if st := closePass(t, w, at); st != "active" || closeRounds(t, w) != 1 {
		t.Fatal("no first round", st)
	}
	readOne(t, w, "land:r672c:1", at.Add(time.Minute), "effective")
	// A second root, ended declined: disposed, but a new identity.
	store.FreezeEventClock(at.Add(90 * time.Second))
	d := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "joins late", BaseCommit: commitB})
	w.passRound(t, d, "h1", "h2", "h3")
	prodChain(t, w, d, "land:r672d:1", at.Add(91*time.Second), "branch_pushed")
	store.FreezeEventClock(at.Add(2 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:r672d:1", "failed", "why", "declined")))
	if st := closePass(t, w, at.Add(3*time.Minute)); st != "active" || closeRounds(t, w) != 2 {
		t.Fatalf("a changed snapshot kept its round: %s, %d rounds", st, closeRounds(t, w))
	}
	readOne(t, w, "land:r672c:1", at.Add(4*time.Minute), "effective")
	if st := closePass(t, w, at.Add(5*time.Minute)); st != "done" {
		t.Fatal("not closed in the new round", st)
	}
}

// Serial: FreezeEventClock. B-02: the mode alone puts no root in scope
// (claim, close and settle agree); PROD records enabled only after its mode;
// with both, the fence is the later.
func TestB4FenceNeedsEnabledAndMode(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, landingRuleBody))
	ok(t, err)
	store.FreezeEventClock(at.Add(-3 * time.Hour))
	enabled := func(env string) time.Time {
		var fence time.Time
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var e error
			fence, e = store.ObserveLandingEnabled(ctx, tx, env, "dev", "bifrost-platform", store.DecisionAt())
			return e
		}))
		return fence
	}
	if f := enabled("prod"); !f.IsZero() {
		t.Fatal("PROD recorded enabled before its mode", f)
	}
	if st := landingState(t, w, landingRepoStream); !st.EnabledAt.IsZero() {
		t.Fatal("enabled written on PROD without the mode")
	}
	store.FreezeEventClock(at.Add(-2 * time.Hour))
	mode, err := store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if st := landingState(t, w, landingRepoStream); !st.Fence().IsZero() {
		t.Fatal("the mode alone made a fence", st.Fence())
	}
	// A mission and its root accepted with only the mode.
	store.FreezeEventClock(at.Add(-90 * time.Minute))
	w.goalMission(t, "M-3")
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "mode only", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if status.InScope {
		t.Fatalf("a root in scope on the mode alone: %+v", status)
	}
	settled, err := store.SettleUnclaimed(ctx, w.rt, "bifrost-platform", at.Add(7*time.Hour))
	ok(t, err)
	if len(settled) != 0 {
		t.Fatal("settled a root outside the scope", settled)
	}
	if st := closePass(t, w, at); st != "done" {
		t.Fatal("a mission outside the scope did not close as before", st)
	}
	store.FreezeEventClock(at.Add(-time.Hour))
	f := enabled("prod")
	st := landingState(t, w, landingRepoStream)
	if st.EnabledAt.IsZero() || !f.Equal(st.EnabledAt) || !f.After(mode) {
		t.Fatalf("fence %s, enabled %s, mode %s", f, st.EnabledAt, mode)
	}
}

// Serial: FreezeEventClock. R-01: the Owner's disposition stands over an
// effective read outside any round: the mission stops, no round opens.
func TestB4DispositionOverEvidenceOutsideTheRound(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r672e:1", at, "main_intent", newLandingRecord("land:r672e:1", "main_result", "outcome", "ok"))
	readOne(t, w, "land:r672e:1", at.Add(-10*time.Minute), "unknown")
	f := newFakeCards(func() time.Time { return at })
	cardPass(t, w, f, at.Add(2*time.Hour))
	f.answer("q1", "a", at.Add(2*time.Hour+time.Minute)) // not delivered
	cardPass(t, w, f, at.Add(2*time.Hour+2*time.Minute))
	readOne(t, w, "land:r672e:1", at.Add(2*time.Hour+3*time.Minute), "effective")
	if st := closePass(t, w, at.Add(2*time.Hour+4*time.Minute)); st != "stopped" || closeRounds(t, w) != 0 {
		t.Fatalf("an effective read outside a round overrode the Owner: %s, %d rounds", st, closeRounds(t, w))
	}
}

// Serial: FreezeEventClock. R-02: a root the Owner counted not delivered,
// then read effective in a round another root opened, is delivered; its
// reversal within the day is told whatever the disposition.
func TestB4LateRevertAfterADisposition(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r672f:1", at, "main_intent", newLandingRecord("land:r672f:1", "main_result", "outcome", "ok"))
	readOne(t, w, "land:r672f:1", at.Add(-10*time.Minute), "unknown")
	f := newFakeCards(func() time.Time { return at })
	cardPass(t, w, f, at.Add(2*time.Hour))
	f.answer("q1", "a", at.Add(2*time.Hour+time.Minute))
	cardPass(t, w, f, at.Add(2*time.Hour+2*time.Minute))
	// A second landed root opens a round that reads both.
	store.FreezeEventClock(at.Add(2*time.Hour + 3*time.Minute))
	g := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "second", BaseCommit: commitB})
	w.passRound(t, g, "h1", "h2", "h3")
	prodChain(t, w, g, "land:r672g:1", at.Add(2*time.Hour+4*time.Minute), "main_intent")
	store.FreezeEventClock(at.Add(2*time.Hour + 12*time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:r672g:1", "main_result", "outcome", "ok")))
	readOne(t, w, "land:r672g:1", at.Add(2*time.Hour+13*time.Minute), "effective")
	readOne(t, w, "land:r672f:1", at.Add(2*time.Hour+13*time.Minute), "effective")
	if st := closePass(t, w, at.Add(2*time.Hour+14*time.Minute)); st != "active" || closeRounds(t, w) != 1 {
		t.Fatal("no round", st)
	}
	readOne(t, w, "land:r672f:1", at.Add(2*time.Hour+15*time.Minute), "effective")
	readOne(t, w, "land:r672g:1", at.Add(2*time.Hour+15*time.Minute), "effective")
	if st := closePass(t, w, at.Add(2*time.Hour+16*time.Minute)); st != "done" {
		t.Fatal("not closed", st)
	}
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	delivered := 0
	for _, r := range status.Closed.Roots {
		if r["class"] == store.RootDelivered {
			delivered++
		}
	}
	if delivered != 2 {
		t.Fatalf("the disposed root read in the round was not delivered: %+v", status.Closed.Roots)
	}
	store.FreezeEventClock(at.Add(4 * time.Hour))
	cands, err := store.MainCandidates(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	for _, c := range cands {
		if c.Op == "land:r672f:1" {
			_, err := store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", c, store.MainEvidence{Head: commitA, HasR: true, Validity: "reverted", RevertedBy: commitA, ReadAt: at.Add(4 * time.Hour)})
			ok(t, err)
		}
	}
	if tr, found := triggersAt(t, w, at.Add(4*time.Hour))["land:r672f:1"]; !found || tr.Kind != store.CardLateRevert {
		t.Fatalf("a late reversal after a disposition was not told: %+v", tr)
	}
}

// Serial: FreezeEventClock. R-05, R-07: a park changes reason in one
// transaction, and only from the park observed; the status shows the park,
// its since and when it would be told.
func TestB4ParkShowsAndChangesWhole(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r672h:1", at, "branch_pushed",
		newLandingRecord("land:r672h:1", "parked", "why", "window", "retry_at", at.Add(time.Hour).Format(time.RFC3339Nano)))
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	r := status.Roots[0]
	if r.Reason != "parked:window" || r.ParkedSince.IsZero() || !r.RemindAt.Equal(r.ParkedSince.Add(store.ParkedLongAfter)) || r.Shown != "" {
		t.Fatalf("park status: %+v", r)
	}
	seen := landingState(t, w, landingRepoStream).Operations["land:r672h:1"].LastAt
	store.FreezeEventClock(at.Add(time.Minute))
	if done, err := store.ReparkLanding(ctx, w.rt, "bifrost-platform", "land:r672h:1", "rule", seen, "ci_stuck", at.Add(2*time.Minute), "test "); err != nil || done {
		t.Fatal("re-parked from a park not observed", done, err)
	}
	done, err := store.ReparkLanding(ctx, w.rt, "bifrost-platform", "land:r672h:1", "window", seen, "ci_stuck", at.Add(2*time.Minute), "test ")
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations["land:r672h:1"]; !done || o.Phase != "parked" || o.Why != "ci_stuck" {
		t.Fatalf("not re-parked: %v %+v", done, o)
	}
	status, err = store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if r := status.Roots[0]; r.Reason != "parked:ci_stuck" || r.Shown != store.CardCIStuck || !r.RemindAt.IsZero() || !r.ParkedSince.Equal(at.Add(time.Minute)) {
		t.Fatalf("re-park status: %+v", r)
	}
}

// Serial: FreezeEventClock. R-04: with no way to ask, an existing question
// is still taken up by its key.
func TestB4LookupWithoutAsk(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r672i:1", at, "branch_pushed", newLandingRecord("land:r672i:1", "failed", "why", "stale"))
	f := newFakeCards(func() time.Time { return at })
	store.FreezeEventClock(at)
	prepared, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", at)
	ok(t, err)
	if len(prepared) != 1 {
		t.Fatal(prepared)
	}
	// The ask reaches the store, its reply is lost: no binding.
	f.loseReply = true
	if _, err := store.AskLandingCard(ctx, w.rt, f.source(), "bifrost-platform", prepared[0], at, true); err == nil {
		t.Fatal("a lost reply bound")
	}
	src := f.source()
	src.AskRaw = nil
	id, err := store.AskLandingCard(ctx, w.rt, src, "bifrost-platform", prepared[0], at.Add(time.Minute), true)
	ok(t, err)
	if id != "q1" || f.asks != 1 {
		t.Fatalf("not taken up by its key: %q, %d asks", id, f.asks)
	}
	_ = context.Background
}

// Serial: FreezeEventClock. R-03: a disposition without its applied answer,
// an outcome naming another kind or a wrong reminder, and a second record
// for a handled question are refused.
func TestB4CardRecordsBindTheirAnswer(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	root := liveRoot(t, w, "land:r672j:1", at, "branch_pushed", newLandingRecord("land:r672j:1", "failed", "why", "stale"))
	f := newFakeCards(func() time.Time { return at })
	cardPass(t, w, f, at)
	var c store.CardRecord
	for _, x := range cardsNow(t, w) {
		c = x
	}
	if !c.Bound || c.QuestionID != "q1" {
		t.Fatalf("%+v", c)
	}
	put := func(key string, body map[string]any) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: root, Body: map[string]any{"text": "forged", key: body}})
			return err
		})
	}
	answered := at.Add(time.Minute).UTC()
	outcome := func(kind, choice, out, why string, extra ...any) map[string]any {
		b := map[string]any{"v": "1", "question_id": "q1", "prep": c.Prep, "kind": kind, "trigger_seq": c.TriggerSeq, "letter": "b", "choice": choice,
			"by": "owner", "answered_at": answered.Format(time.RFC3339Nano), "channel": "console", "root": root, "op": c.Op, "outcome": out, "why": why}
		for i := 0; i+1 < len(extra); i += 2 {
			b[extra[i].(string)] = extra[i+1]
		}
		return b
	}
	if err := put("landing_disposed", map[string]any{"v": "1", "root": root, "op": c.Op, "trigger_seq": c.TriggerSeq, "question_id": "q1", "why": "owner"}); err == nil {
		t.Error("a disposition with no answer was accepted")
	}
	if err := put("landing_card_outcome", outcome(store.CardFloor, "wait", "held", "wait", "remind_at", answered.Add(store.CardHoldFor).Format(time.RFC3339Nano))); err == nil {
		t.Error("an outcome naming another kind was accepted")
	}
	if err := put("landing_card_outcome", outcome(c.Kind, "wait", "held", "wait", "remind_at", answered.Add(time.Hour).Format(time.RFC3339Nano))); err == nil {
		t.Error("a hold with a wrong reminder was accepted")
	}
	if err := put("landing_card_outcome", outcome(c.Kind, "do_not_merge", "acknowledged", "ack")); err == nil {
		t.Error("an outcome not following its choice was accepted")
	}
	f.answer("q1", "b", answered) // do_not_merge: §8's landing_answer
	cardPass(t, w, f, at.Add(2*time.Minute))
	if err := put("landing_card_outcome", outcome(c.Kind, "do_not_merge", "moot", "trigger_gone")); err == nil {
		t.Error("a second record for a handled question was accepted")
	}
	v, found, err := store.ReadLandingCard(ctx, w.rt, "bifrost-platform", "q1")
	ok(t, err)
	if !found || v.Outcome == nil {
		t.Fatalf("view: %+v", v)
	}
}

// Serial: FreezeEventClock. Review 675 R-05: a park observed, then changed
// to ci_stuck and back to window by another caller, is not the park the old
// observation saw: the old caller writes nothing.
func TestB4ReparkRefusesABA(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r675a:1", at, "branch_pushed",
		newLandingRecord("land:r675a:1", "parked", "why", "window", "retry_at", at.Add(time.Hour).Format(time.RFC3339Nano)))
	old := landingState(t, w, landingRepoStream).Operations["land:r675a:1"].LastAt
	store.FreezeEventClock(at.Add(time.Minute))
	ok1, err := store.ReparkLanding(ctx, w.rt, "bifrost-platform", "land:r675a:1", "window", old, "ci_stuck", at.Add(2*time.Minute), "test ")
	ok(t, err)
	mid := landingState(t, w, landingRepoStream).Operations["land:r675a:1"].LastAt
	store.FreezeEventClock(at.Add(2 * time.Minute))
	ok2, err := store.ReparkLanding(ctx, w.rt, "bifrost-platform", "land:r675a:1", "ci_stuck", mid, "window", at.Add(3*time.Minute), "test ")
	ok(t, err)
	if !ok1 || !ok2 {
		t.Fatal("the two re-parks of the other caller", ok1, ok2)
	}
	store.FreezeEventClock(at.Add(3 * time.Minute))
	stale, err := store.ReparkLanding(ctx, w.rt, "bifrost-platform", "land:r675a:1", "window", old, "ci_stuck", at.Add(4*time.Minute), "test ")
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations["land:r675a:1"]; stale || o.Why != "window" {
		t.Fatalf("an old observation moved a later park: %v %+v", stale, o)
	}
}

// Serial: FreezeEventClock. Review 675 R-07: a card prepared and not asked
// shows as pending; once the Owner answers wait, the root shows held until a
// day after the answer, on that question.
func TestB4StatusShowsHeldAndPendingCards(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:r675h:1", at, "main_intent", newLandingRecord("land:r675h:1", "main_result", "outcome", "ok"))
	readOne(t, w, "land:r675h:1", at.Add(-10*time.Minute), "unknown")
	store.FreezeEventClock(at.Add(2 * time.Hour))
	_, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", at.Add(2*time.Hour))
	ok(t, err)
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if r := status.Roots[0]; !r.CardPending || !r.HeldUntil.IsZero() {
		t.Fatalf("prepared card not pending: %+v", r)
	}
	f := newFakeCards(func() time.Time { return at.Add(2 * time.Hour) })
	cardPass(t, w, f, at.Add(2*time.Hour))
	answered := at.Add(2*time.Hour + time.Minute)
	f.answer("q1", "b", answered) // wait
	cardPass(t, w, f, at.Add(2*time.Hour+2*time.Minute))
	store.FreezeEventClock(at.Add(3 * time.Hour))
	status, err = store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	r := status.Roots[0]
	if r.CardPending || r.HeldQuestion != "q1" || !r.HeldUntil.Equal(answered.Add(store.CardHoldFor)) {
		t.Fatalf("held not shown: %+v", r)
	}
}
