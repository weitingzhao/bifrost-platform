package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func acceptedDrill(t *testing.T) (*world, string, store.LandingSource) {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, landingRuleBody))
	ok(t, err)
	enabled := time.Now().Add(-time.Hour)
	restore := store.FreezeEventClock(enabled)
	err = store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", enabled)
		return e
	})
	restore()
	ok(t, err)
	task := w.ready(t, store.Task{PlanNode: s1, BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	src := store.LandingSource{Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
		return true, []rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}}, nil
	}, History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Never}, nil
	}}
	return w, task, src
}
func pushedDrill(t *testing.T, w *world, task string, src store.LandingSource) (string, time.Time) {
	t.Helper()
	op, d, e := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, e)
	if !d.Allowed || op == "" {
		t.Fatal(d)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	restore := store.FreezeEventClock(at)
	defer restore()
	ref := "refs/heads/land/stg/" + task + "/1"
	for _, r := range []map[string]any{newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true), newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	return op, at
}

// Serial: FreezeEventClock. S2 ①, actual persisted discard and automatic k+1.
func TestDrillScanMainMovementOpensNextOperation(t *testing.T) {
	w, task, src := acceptedDrill(t)
	op, at := pushedDrill(t, w, task, src)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "parked", "why", "main_red", "retry_at", at.Add(time.Minute).Format(time.RFC3339Nano))))
	remote := &branchObserved{}
	ctl := controller.Controller{DB: w.rt, Env: "stg", Now: func() time.Time { return at.Add(time.Minute) }, DrillGit: remote}
	ctl.Drill = &controller.DrillSources{Facts: src, Main: func(context.Context) (string, error) { return strings.Repeat("c", 40), nil }, CI: func(context.Context, string, time.Time) (landing.CIRun, error) {
		t.Fatal("polled old red main before discarding")
		return landing.CIRun{}, nil
	}, WindowBlocked: func(context.Context) (bool, error) { return false, nil }}
	// The fake Git stops at the new replay's Open; the durable claim must
	// already exist. Real Git replay/overlap tests live in landinggit.
	if e := ctl.LandingDrillPass(ctx); e == nil {
		t.Fatal("expected fixture Open refusal")
	}
	state := landingState(t, w, landingRepoStream)
	old := state.Operations[op]
	latest := state.LatestOperation(task)
	if old.Phase != "discarded" || old.Outcome != "main_changed" || old.Active() || old.BlocksClaim() || latest == op || state.Operations[latest].Phase != "claimed" || remote.opens != 1 {
		t.Fatal(old, latest, remote.opens)
	}
	_ = ctl.LandingDrillPass(ctx)
	if len(landingState(t, w, landingRepoStream).Operations) != 2 {
		t.Fatal("restart made duplicate operation")
	}
}

// Serial: FreezeEventClock. S2 ② plus Q3's fresh second gate and cleanup.
func TestDrillFinalGateAndRehearsalDatabase(t *testing.T) {
	for _, change := range []string{"none", "latest_ci", "main", "window"} {
		t.Run(change, func(t *testing.T) {
			w, task, src := acceptedDrill(t)
			op, at := pushedDrill(t, w, task, src)
			now := at.Add(5 * time.Minute)
			restore := store.FreezeEventClock(now)
			defer restore()
			mainReads, ciReads, windowReads := 0, 0, 0
			drill := controller.DrillSources{Facts: src, Main: func(context.Context) (string, error) {
				mainReads++
				if change == "main" && mainReads > 1 {
					return strings.Repeat("c", 40), nil
				}
				return commitA, nil
			}, CI: func(_ context.Context, sha string, _ time.Time) (landing.CIRun, error) {
				ciReads++
				status := "Succeeded"
				if change == "latest_ci" && ciReads > 1 {
					status = "Failed"
				}
				return landing.CIRun{R: sha, Run: "latest", Status: status, CreatedAt: at.Add(time.Minute)}, nil
			}, WindowBlocked: func(context.Context) (bool, error) { windowReads++; return change == "window" && windowReads > 1, nil }}
			ctl := controller.Controller{DB: w.rt, Env: "stg"}
			a, e := ctl.LandingDrillCI(ctx, drill, op, now)
			if a.Kind != "ready" {
				t.Fatal(a, e)
			}
			if (e == nil) != (change == "none") {
				t.Fatal(change, e)
			}
			o := landingState(t, w, landingRepoStream).Operations[op]
			if change != "none" {
				if o.Phase == "rehearsed" {
					t.Fatal("stale gate passed")
				}
				return
			}
			if o.Phase != "rehearsed" || o.EverLanded || o.Active() || o.BlocksClaim() || o.Reservable(task) {
				t.Fatal(o)
			}
			quota := landingState(t, w, "landing-quota:dev:"+now.UTC().Format("2006-01-02"))
			if len(quota.Reservations) != 0 {
				t.Fatal("drill spent quota")
			}
			prod := controller.Controller{DB: w.rt, Env: "prod"}
			_, d, e := prod.LandingDecision(ctx, src, task, "", "claim", now)
			ok(t, e)
			if !d.Allowed {
				t.Fatal("rehearsal blocked prod", d)
			}
			next, d, e := store.ClaimLandingDrill(ctx, w.rt, src, task, now)
			ok(t, e)
			if next != "" || d.Reason != "rehearsed" {
				t.Fatal("drill repeated", next, d)
			}
			deleted := false
			drill.Delete = func(_ context.Context, ref, sha string) error {
				if ref != o.Ref || sha != o.R {
					t.Fatal("unsafe deletion")
				}
				deleted = true
				return nil
			}
			_, e = ctl.LandingDrillCI(ctx, drill, op, now)
			ok(t, e)
			if !deleted {
				t.Fatal("no cleanup")
			}
		})
	}
}

type startupDrillGit struct{ opens atomic.Int32 }

func (g *startupDrillGit) Open(context.Context, string) (*landinggit.Workspace, error) {
	g.opens.Add(1)
	return nil, errors.New("fixture stop")
}
func (g *startupDrillGit) Ref(context.Context, string) (string, error)  { return "", nil }
func (g *startupDrillGit) Delete(context.Context, string, string) error { return nil }

// Serial: FreezeEventClock via acceptedDrill. Startup scans before the first
// hourly timer. Nil Drill leaves the loop disabled even on STG.
func TestDrillStartScansImmediately(t *testing.T) {
	w, task, src := acceptedDrill(t)
	g := &startupDrillGit{}
	ctl := controller.Controller{DB: w.rt, Env: "stg", LeaseEvery: time.Hour, FactsEvery: time.Hour, DrillGit: g, Drill: &controller.DrillSources{Facts: src}}
	runCtx, cancel := context.WithCancel(ctx)
	wait := ctl.Start(runCtx)
	defer func() { cancel(); wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for g.opens.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if g.opens.Load() == 0 {
		t.Fatal("no startup scan")
	}
	if landingState(t, w, landingRepoStream).LatestOperation(task) == "" {
		t.Fatal("no claim")
	}
	cancel()
	wait()
	g.opens.Store(0)
	ctl.Drill = nil
	runCtx, cancel = context.WithCancel(ctx)
	wait = ctl.Start(runCtx)
	cancel()
	wait()
	if g.opens.Load() != 0 {
		t.Fatal("disabled loop ran")
	}
}

// Serial: FreezeEventClock. Retry lifts the persisted claim block but does
// not bypass the real eligibility reader or reset the eight-attempt budget.
func TestDrillRetryClaimBudgetDatabase(t *testing.T) {
	for _, count := range []int{1, 8} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			w, task, src := acceptedDrill(t)
			first := time.Now().Add(-7 * time.Hour)
			restore := store.FreezeEventClock(first)
			defer restore()
			var latest string
			for i := 1; i <= count; i++ {
				latest = fmt.Sprintf("land:%s:%d", task, i)
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(latest, "claimed", "task", task, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
				if i == count {
					ok(t, appendLanding(w, landingRepoStream, newLandingRecord(latest, "failed", "why", "bound")))
					continue
				}
				ref := fmt.Sprintf("refs/heads/land/stg/%s/%d", task, i)
				for _, r := range []map[string]any{newLandingRecord(latest, "prepared", "R", commitB, "M0", commitA, "committer_date", first.Format(time.RFC3339Nano), "dry_run", true), newLandingRecord(latest, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(latest, "discarded", "why", "main_changed")} {
					ok(t, appendLanding(w, landingRepoStream, r))
				}
			}
			now := time.Now()
			store.FreezeEventClock(now)
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(latest, "retry", "root", task, "after_op", latest, "reason", "again", "by", "owner")))
			op, d, e := store.ClaimLandingDrill(ctx, w.rt, src, task, now)
			ok(t, e)
			if count == 1 {
				if op == "" || !d.Allowed {
					t.Fatal("retry stayed blocked", op, d)
				}
			} else if op != "" || d.Reason != "bound" {
				t.Fatal("ninth operation", op, d)
			}
		})
	}
}

// Serial: FreezeEventClock via acceptedDrill.
func TestDrillBranchCollisionAutomaticallyReclaims(t *testing.T) {
	w, task, src := acceptedDrill(t)
	op, d, e := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, e)
	if !d.Allowed {
		t.Fatal(d)
	}
	at := time.Now()
	ref := "refs/heads/land/stg/" + task + "/1"
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB)))
	remote := &branchObserved{tip: strings.Repeat("c", 40)}
	ctl := controller.Controller{DB: w.rt, Env: "stg", DrillGit: remote, Drill: &controller.DrillSources{Facts: src}}
	if ctl.LandingDrillPass(ctx) == nil {
		t.Fatal("expected new replay Open refusal")
	}
	state := landingState(t, w, landingRepoStream)
	if state.Operations[op].Phase != "discarded" || state.Operations[op].Outcome != "branch_collision" || state.LatestOperation(task) == op || remote.opens != 1 {
		t.Fatal(state, remote.opens)
	}
}

// Serial: FreezeEventClock via acceptedDrill. Removing the final gate's rule
// check would make this record a rehearsal after the Owner tightened the rule.
func TestDrillFinalGateReadsTightenedRule(t *testing.T) {
	w, _, src := acceptedDrill(t)
	tasks, e := store.LandingCandidates(ctx, w.rt)
	ok(t, e)
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	op, at := pushedDrill(t, w, tasks[0], src)
	body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
	_, e = store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, body)))
	ok(t, e)
	now := at.Add(time.Minute)
	restore := store.FreezeEventClock(now)
	defer restore()
	e = store.FinishLandingDrill(ctx, w.rt, src, op, now, func(context.Context, landing.Operation) (landing.DrillObservation, error) {
		return landing.DrillObservation{Main: commitA, Candidate: landing.CIRun{Run: "green", R: commitB, Status: "Succeeded", CreatedAt: at.Add(time.Second)}}, nil
	})
	if e == nil || landingState(t, w, landingRepoStream).Operations[op].Phase != "branch_pushed" {
		t.Fatal("tightened rule admitted", e)
	}
}
