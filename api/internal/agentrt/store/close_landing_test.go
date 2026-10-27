package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// goalDrill is the STG drill's real shape: a mission bound to no plan step
// (an operator's ad-hoc mission), its implementation accepted after the
// landing rule was first seen.
func goalDrill(t *testing.T, mission string) (*world, string, store.LandingSource) {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, landingRuleBody))
	ok(t, err)
	enabled := time.Now().Add(-time.Hour)
	restore := store.FreezeEventClock(enabled)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", enabled)
		return e
	}))
	restore()
	w.goalMission(t, mission)
	task := w.ready(t, store.Task{MissionID: mission, OffPlanReason: "an operator's drill", BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	src := store.LandingSource{Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
		return true, []rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}}, nil
	}, History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Never}, nil
	}}
	return w, task, src
}

// Serial: FreezeEventClock. S2 found on STG (M-114, 2026-10-22): a mission
// bound to no plan step closed 34 ms after its last task was accepted, so a
// drill claimed in between would end failed{stale} at its next gate. An
// operation in flight now holds the mission; its end releases it.
func TestCloseMissionsWaitsForLandingInFlight(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	if w.state(t, "task", task) != "accepted" {
		t.Fatal("fixture: task not accepted")
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("claim refused", d)
	}
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || len(closed) != 0 || w.state(t, "mission", "M-3") != "active" {
		t.Fatalf("closed under a claimed operation: %v %v", closed, err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	restore := store.FreezeEventClock(at)
	ref := "refs/heads/land/stg/" + task + "/1"
	for _, r := range []map[string]any{newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true), newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	restore()
	if closed, err = store.CloseMissions(ctx, w.rt); err != nil || len(closed) != 0 {
		t.Fatalf("closed while CI runs: %v %v", closed, err)
	}
	// Discarded on a moved main: the replay k+1 is due, the mission still holds.
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
	if closed, err = store.CloseMissions(ctx, w.rt); err != nil || len(closed) != 0 {
		t.Fatalf("closed between discard and replay: %v %v", closed, err)
	}
	// The bound gives the discarded root its terminal successor; then it closes.
	later := time.Now().Add(7 * time.Hour)
	restore = store.FreezeEventClock(later)
	_, err = store.SettleLandingDrillBounds(ctx, w.rt, later)
	restore()
	ok(t, err)
	if s := landingState(t, w, landingRepoStream); s.Operations[s.LatestOperation(task)].Phase != "failed" {
		t.Fatal("no failed{bound} successor", s.LatestOperation(task))
	}
	closed, err = store.CloseMissions(ctx, w.rt)
	if err != nil || len(closed) != 1 || w.state(t, "mission", "M-3") != "done" {
		t.Fatalf("not closed after the landing ended: %v %v %s", closed, err, w.state(t, "mission", "M-3"))
	}
}

// Serial: FreezeEventClock. The other order: the close decided first, so the
// claim reads the mission done and refuses, never an operation under a closed
// mission.
func TestLandingClaimAfterCloseIsRefused(t *testing.T) {
	w, task, src := goalDrill(t, "M-4")
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || len(closed) != 1 {
		t.Fatalf("fixture: %v %v", closed, err)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if d.Allowed || op != "" || d.Reason != "stale" {
		t.Fatal("claimed under a closed mission", op, d)
	}
}
