package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// S3 B4 (CUT §11.8 v3): a mission in the live landing scope closes from
// landing evidence, never on a timer.

// liveWorld is a PROD-like stream: a landing rule (stg_release as given),
// landing enabled, the live mode recorded, then mission M-3 active.
func liveWorld(t *testing.T, stgRelease string, at time.Time) *world {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	body := strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: "+stgRelease, 1)
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, body))
	ok(t, err)
	store.FreezeEventClock(at.Add(-2 * time.Hour))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", at)
		return e
	}))
	store.FreezeEventClock(at.Add(-time.Hour))
	_, err = store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	store.FreezeEventClock(at.Add(-50 * time.Minute))
	w.goalMission(t, "M-3")
	return w
}

// liveRoot is an accepted implement root of M-3 with an operation run to
// last (prodChain's phases), then the records given.
func liveRoot(t *testing.T, w *world, op string, at time.Time, last string, then ...map[string]any) string {
	t.Helper()
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "live " + op, BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	prodChain(t, w, root, op, at.Add(-30*time.Minute), last)
	for i, r := range then {
		store.FreezeEventClock(at.Add(-20*time.Minute + time.Duration(i)*time.Minute))
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	return root
}

func closePass(t *testing.T, w *world, now time.Time) string {
	t.Helper()
	store.FreezeEventClock(now)
	_, err := store.CloseMissions(ctx, w.rt)
	ok(t, err)
	return w.state(t, "mission", "M-3")
}

func effectiveRead(t *testing.T, w *world, now time.Time) {
	t.Helper()
	store.FreezeEventClock(now)
	_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true, Validity: "effective"}))
	ok(t, err)
}

// Serial: FreezeEventClock. A landed root: the first pass opens a closing
// round and holds; a read after the round's start lets it close done; a read
// before it does not.
func TestB4ClosesALandingOnFreshEvidence(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:b4a:1", at, "main_intent", newLandingRecord("land:b4a:1", "main_result", "outcome", "ok"))
	effectiveRead(t, w, at.Add(-10*time.Minute)) // landed, effective, but before any round
	if st := closePass(t, w, at); st != "active" {
		t.Fatal("closed without a closing round", st)
	}
	var rounds int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'mission:M-3' AND body_json ? 'landing_close_round'`).Scan(&rounds))
	if rounds != 1 {
		t.Fatal("rounds", rounds)
	}
	if st := closePass(t, w, at.Add(time.Minute)); st != "active" {
		t.Fatal("closed on evidence read before the round", st)
	}
	effectiveRead(t, w, at.Add(2*time.Minute))
	if st := closePass(t, w, at.Add(3*time.Minute)); st != "done" {
		t.Fatal("not closed on fresh evidence", st)
	}
	var why string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE event_stream_id = 'mission:M-3' AND event_type = 'mission_state_changed' ORDER BY seq DESC LIMIT 1`).Scan(&why))
	if !strings.Contains(why, "1 root(s) delivered") || !strings.Contains(why, "stg_release none") {
		t.Fatal(why)
	}
}

// Serial: FreezeEventClock. A round past its 30 minutes does not count; a
// new round opens.
func TestB4RoundExpires(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:b4b:1", at, "main_intent", newLandingRecord("land:b4b:1", "main_result", "outcome", "ok"))
	effectiveRead(t, w, at.Add(-10*time.Minute))
	closePass(t, w, at)
	effectiveRead(t, w, at.Add(time.Minute))
	if st := closePass(t, w, at.Add(store.CloseRoundFor+time.Minute)); st != "active" {
		t.Fatal("closed after its round ended", st)
	}
	var rounds int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'mission:M-3' AND body_json ? 'landing_close_round'`).Scan(&rounds))
	if rounds != 2 {
		t.Fatal("rounds", rounds)
	}
}

// Serial: FreezeEventClock. Unresolved roots hold the mission whatever the
// time; disposed ones let it stop; a rule that is not none holds a landing.
func TestB4HoldsAndStops(t *testing.T) {
	at := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:b4c:1", at, "branch_pushed", newLandingRecord("land:b4c:1", "failed", "why", "stale"))
	for _, d := range []time.Duration{0, 7 * time.Hour, 30 * time.Hour} {
		if st := closePass(t, w, at.Add(d)); st != "active" {
			t.Fatalf("a failed root unanswered closed the mission after %s", d)
		}
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:b4c:1", "failed", "why", "declined")))
	if st := closePass(t, w, at.Add(31*time.Hour)); st != "stopped" {
		t.Fatal("all disposed did not stop", st)
	}

	w2 := liveWorld(t, "bifrost-deliver-platform", at)
	liveRoot(t, w2, "land:b4d:1", at, "main_intent", newLandingRecord("land:b4d:1", "main_result", "outcome", "ok"))
	effectiveRead(t, w2, at.Add(-10*time.Minute))
	for _, d := range []time.Duration{0, time.Minute, 2 * time.Hour} {
		effectiveRead(t, w2, at.Add(d))
		if st := closePass(t, w2, at.Add(d+time.Second)); st != "active" {
			t.Fatal("a landing closed under a rule that is not none", st)
		}
	}

	w3 := liveWorld(t, "none", at)
	liveRoot(t, w3, "land:b4e:1", at, "main_intent", newLandingRecord("land:b4e:1", "main_result", "outcome", "ok"),
		newLandingRecord("land:b4e:1", "landed", "R", commitB), newLandingRecord("land:b4e:1", "reverted", "R", commitB, "by", strings.Repeat("f", 40)))
	if st := closePass(t, w3, at); st != "stopped" {
		t.Fatal("a reverted landing alone did not stop", st)
	}
}

// Serial: FreezeEventClock. A mission without the live mode closes as
// before (the STG drill and old missions are unchanged).
func TestB4LeavesMissionsOutsideTheScope(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "no landing", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	var mode int
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE body_json->'landing'->>'kind' = 'mode'`).Scan(&mode)
	}))
	if mode != 0 {
		t.Fatal("a mode in a world without one")
	}
	if st := closePass(t, w, at); st != "done" {
		t.Fatal("a mission outside the live scope did not close as before", st)
	}
}

// Serial: FreezeEventClock. CUT §11.8 v3 item 6: with the live mode on the
// stream, the reconciler's own loop ends a root at its six hours with the
// live switch off and no Git: failed{bound}, which the cards then ask about.
func TestB4BoundsSettleWithoutTheLiveLoop(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:b4f:1", at, "branch_pushed")
	ctl := controller.Controller{DB: w.rt, Env: "prod", Now: func() time.Time { return at.Add(6 * time.Hour) }}
	ctl.Reconcile = &controller.LandingReconcile{}
	store.FreezeEventClock(at.Add(6 * time.Hour))
	ok(t, ctl.LandingReconcilePass(ctx))
	if p, o := phaseOf(t, w, "land:b4f:1"); p != "failed" || o != "bound" {
		t.Fatalf("not ended at its bound: %s %s", p, o)
	}
}

// Serial: FreezeEventClock. CUT §11.8 v3 item 4: every unresolved root has
// an Owner's exit. A root already in main asks to be counted; counted
// delivered, its mission closes done. A landing main has not shown in
// effect for two hours asks too; counted not delivered, the mission stops.
func TestB4OwnerCountsAndTheMissionCloses(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "in main", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:"+root+":1", "failed", "why", "in_main")))
	if st := closePass(t, w, at); st != "active" {
		t.Fatal("an in-main root closed the mission by itself", st)
	}
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if !status.InScope || len(status.Roots) != 1 || status.Roots[0].Class != store.RootUnresolved || status.Roots[0].Reason != "in_main_unverified" || !strings.Contains(status.Rule, "stg_release none") {
		t.Fatalf("status: %+v", status)
	}
	f := newFakeCards(func() time.Time { return at })
	cardPass(t, w, f, at)
	var c store.CardRecord
	for _, x := range cardsNow(t, w) {
		c = x
	}
	if c.Kind != store.CardInMain || c.QuestionID != "q1" || c.Options["a"] != "count_as_delivered" {
		t.Fatalf("%+v", c)
	}
	f.answer("q1", "a", at.Add(time.Minute))
	cardPass(t, w, f, at.Add(2*time.Minute))
	if st := closePass(t, w, at.Add(3*time.Minute)); st != "done" {
		t.Fatal("counted delivered did not close done", st)
	}

	w2 := liveWorld(t, "none", at)
	liveRoot(t, w2, "land:b4g:1", at, "main_intent", newLandingRecord("land:b4g:1", "main_result", "outcome", "ok"))
	store.FreezeEventClock(at.Add(-10 * time.Minute))
	_, err = store.ReconcileMain(ctx, w2.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true})) // landed, validity unknown
	ok(t, err)
	f2 := newFakeCards(func() time.Time { return at })
	cardPass(t, w2, f2, at.Add(time.Hour))
	if f2.asks != 0 {
		t.Fatal("asked before two hours", f2.asks)
	}
	cardPass(t, w2, f2, at.Add(2*time.Hour))
	if f2.asks != 1 {
		t.Fatal("not asked after two hours", f2.asks)
	}
	f2.answer("q1", "a", at.Add(2*time.Hour+time.Minute)) // count it as not delivered
	cardPass(t, w2, f2, at.Add(2*time.Hour+2*time.Minute))
	if st := closePass(t, w2, at.Add(2*time.Hour+3*time.Minute)); st != "stopped" {
		t.Fatal("counted not delivered did not stop", st)
	}
}

// Serial: FreezeEventClock. CUT §11.8 v3 items 2, 4: after its mission
// closed done, a landing is still read for a day; a reversal then is told
// on a late_revert card, and the mission stays done.
func TestB4ReversalAfterTheCloseIsTold(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:b4h:1", at, "main_intent", newLandingRecord("land:b4h:1", "main_result", "outcome", "ok"))
	effectiveRead(t, w, at.Add(-10*time.Minute))
	closePass(t, w, at)
	effectiveRead(t, w, at.Add(time.Minute))
	if st := closePass(t, w, at.Add(2*time.Minute)); st != "done" {
		t.Fatal(st)
	}
	revert := strings.Repeat("f", 40)
	store.FreezeEventClock(at.Add(3 * time.Hour))
	_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: revert, HasR: true, Validity: "reverted", RevertedBy: revert}))
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations["land:b4h:1"]; !o.Reverted {
		t.Fatal("a reversal after the close was not seen")
	}
	if tr, ok := triggersAt(t, w, at.Add(3*time.Hour))["land:b4h:1"]; !ok || tr.Kind != store.CardLateRevert {
		t.Fatalf("no late_revert: %+v", tr)
	}
	if st := w.state(t, "mission", "M-3"); st != "done" {
		t.Fatal("the mission was reopened", st)
	}
}

// Serial: FreezeEventClock. Review 670 R-07: a card that ended with its
// answer unknown (lost) is shown on its root in the mission's status.
func TestB4StatusShowsALostCard(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:b4i:1", at, "branch_pushed", newLandingRecord("land:b4i:1", "failed", "why", "conflict"))
	f := newFakeCards(func() time.Time { return at })
	cardPass(t, w, f, at)
	delete(f.byID, "q1")
	cardPass(t, w, f, at.Add(time.Minute))
	st, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if len(st.Roots) != 1 || !strings.HasSuffix(st.Roots[0].CardAlert, " lost") {
		t.Fatalf("%+v", st.Roots)
	}
}
