package store_test

import (
	"context"
	"errors"
	"strings"
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

// CUT §2.2, §2.4: a controller call reaches the migrated DB, the accepted
// verification, current signed rule, enable note, and fresh trusted evidence.
// Serial: FreezeEventClock
func TestControllerLandingFactsWiredThroughStore(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, landingRuleBody))
	ok(t, err)
	enabled := time.Now().Add(-time.Hour)
	restore := store.FreezeEventClock(enabled)
	err = store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.ObserveLandingEnabled(ctx, tx, "stg", "dev", "bifrost-platform", enabled)
		return e
	})
	restore()
	ok(t, err)
	impl := w.ready(t, store.Task{PlanNode: s1, BaseCommit: commitB})
	w.passRound(t, impl, "h1", "h2", "h3")
	reads := 0
	src := store.LandingSource{
		Result: func(_ context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
			if repo != "bifrost-platform" || task != impl || attempt == "" || base != commitB || a != commitA {
				t.Fatal("wrong accepted result", repo, task, attempt, base, a)
			}
			return true, []rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}}, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			reads++
			return landing.MainHistory{State: landing.Never}, nil
		},
	}
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	f, d, err := ctl.LandingDecision(ctx, src, impl, "", "claim", time.Now())
	ok(t, err)
	if !d.Allowed || f.Rule == nil || f.Rule.Version != 1 || f.Attempt == "" || f.AcceptedAt.IsZero() {
		t.Fatal("controller store path did not admit accepted task", d, f)
	}
	op, claim, err := store.ClaimLandingDrill(ctx, w.rt, src, impl, time.Now())
	ok(t, err)
	if op == "" || !claim.Allowed {
		t.Fatal("store claim not wired", op, claim)
	}
	duplicate, decision, err := store.ClaimLandingDrill(ctx, w.rt, src, impl, time.Now())
	ok(t, err)
	if duplicate != "" || decision.Allowed {
		t.Fatal("duplicate active claim", duplicate, decision)
	}
	// Persist an intentional CI retry, then crash after the external create.
	clock := time.Now().Add(time.Minute)
	restore = store.FreezeEventClock(clock)
	put := func(kind string, kv ...any) {
		t.Helper()
		ok(t, appendLanding(w, landingRepoStream, landingRecord(op, kind, kv...)))
	}
	put("prepared", "R", commitB, "M0", commitA, "committer_date", clock.Format(time.RFC3339Nano), "dry_run", true)
	ref := "refs/heads/land/stg/" + impl + "/1"
	put("branch_intent", "ref", ref, "R", commitB)
	put("branch_pushed", "ref", ref, "R", commitB)
	restore()
	names := []string{}
	created := map[string]bool{}
	drill := controller.DrillSources{Facts: src,
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, revision string, _ time.Time) (landing.CIRun, error) {
			status := "Failed"
			if revision == commitA {
				status = "Succeeded"
			}
			return landing.CIRun{R: revision, Run: "ci-old", Status: status, CreatedAt: clock.Add(time.Second)}, nil
		},
		EnsureCI: func(_ context.Context, _ string, name, revision string) error {
			names = append(names, name)
			if created[name] {
				return nil
			}
			created[name] = true
			return errors.New("crash after create")
		},
	}
	restore = store.FreezeEventClock(clock.Add(time.Minute))
	action, crash := ctl.LandingDrillCI(ctx, drill, op, clock.Add(time.Minute))
	restore()
	if crash == nil || action.Kind != "ci_rerun" {
		t.Fatal("retry intent not persisted before create", action, crash)
	}
	restarted := controller.Controller{DB: w.rt, Env: "stg"}
	restore = store.FreezeEventClock(clock.Add(2 * time.Minute))
	action, err = restarted.LandingDrillCI(ctx, drill, op, clock.Add(2*time.Minute))
	restore()
	ok(t, err)
	if action.Kind != "wait" || len(created) != 1 || len(names) != 2 || names[0] != names[1] {
		t.Fatal("crash duplicated CI or consumed old failure", action, names)
	}
	beforeReads := reads
	// Persist a tightening. The next controller call must read its actual body,
	// rather than using the claim-time version/facts.
	body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
	_, err = store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, body)))
	ok(t, err)
	f, d, err = ctl.LandingDecision(ctx, src, impl, op, "push", time.Now())
	ok(t, err)
	if d.Allowed || d.Reason != "rule" || f.Rule.Version != 2 || reads != beforeReads+1 {
		t.Fatal("stale controller facts", d, f.Rule, reads)
	}
	src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{}, errors.New("offline")
	}
	_, d, err = ctl.LandingDecision(ctx, src, impl, op, "push", time.Now())
	if err == nil || d.Allowed || d.Reason != "facts_unreadable" {
		t.Fatal("unreadable history admitted", d, err)
	}
}

// CUT §3 S2 ③: waiting on the window consumes the six-hour budget even if
// every external source is unavailable. Restart a new controller from notes.
// Serial: FreezeEventClock
func TestDrillWindowBoundFreesActiveSlotInDatabase(t *testing.T) {
	w := newWorld(t)
	first := time.Now().Add(-6 * time.Hour)
	restore := store.FreezeEventClock(first)
	defer restore()
	op := "land:window:1"
	put := func(kind string, kv ...any) {
		t.Helper()
		ok(t, appendLanding(w, landingRepoStream, landingRecord(op, kind, kv...)))
	}
	put("claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1)
	put("prepared", "R", commitB, "M0", commitA, "committer_date", first.Format(time.RFC3339Nano), "dry_run", true)
	ref := "refs/heads/land/stg/" + landingTask + "/1"
	put("branch_intent", "ref", ref, "R", commitB)
	put("branch_pushed", "ref", ref, "R", commitB)
	put("parked", "why", "window", "retry_at", first.Add(time.Minute).Format(time.RFC3339Nano))
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	// A fresh controller with no I/O sources still expires the operation.
	// Time passes on the clock the gates decide at (CUT §9).
	store.FreezeEventClock(first.Add(6 * time.Hour))
	a, err := ctl.LandingDrillCI(ctx, controller.DrillSources{}, op, first.Add(6*time.Hour))
	ok(t, err)
	if a.Kind != "failed" || a.Why != "bound" {
		t.Fatal(a)
	}
	state := landingState(t, w, landingRepoStream)
	o := state.Operations[op]
	if o.Active() || !o.Terminal() || o.Phase != "failed" || o.Outcome != "bound" {
		t.Fatal(o)
	}
	again := controller.Controller{DB: w.rt, Env: "stg"}
	store.FreezeEventClock(first.Add(7 * time.Hour))
	a, err = again.LandingDrillCI(ctx, controller.DrillSources{}, op, first.Add(7*time.Hour))
	ok(t, err)
	if a.Kind != "cleanup" {
		t.Fatal("restart repeated failure", a)
	}
}

type branchObserved struct {
	tip   string
	opens int
}

func (b *branchObserved) Open(context.Context, string) (*landinggit.Workspace, error) {
	b.opens++
	return nil, errors.New("must reconcile before replay")
}
func (b *branchObserved) Ref(context.Context, string) (string, error)  { return b.tip, nil }
func (b *branchObserved) Delete(context.Context, string, string) error { return nil }

func TestDrillControllerRecoversBranchIntentWithoutReplaying(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	at := time.Now()
	op := "land:" + landingTask + ":1"
	put := func(kind string, kv ...any) {
		t.Helper()
		ok(t, appendLanding(w, landingRepoStream, landingRecord(op, kind, kv...)))
	}
	put("claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1)
	put("prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true)
	ref := "refs/heads/land/stg/" + landingTask + "/1"
	put("branch_intent", "ref", ref, "R", commitB)
	// No rule, mission or accepted-result fixture is needed for reconciliation:
	// the branch was already pushed, and expiry must not erase that fact.
	remote := &branchObserved{tip: commitB}
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	ok(t, ctl.LandingDrillBranch(ctx, remote, op, at.Add(time.Minute)))
	o := landingState(t, w, landingRepoStream).Operations[op]
	if o.Phase != "branch_pushed" || o.R != commitB || remote.opens != 0 {
		t.Fatal(o, remote.opens)
	}
	// Repeating after another restart does no external write or replay.
	again := controller.Controller{DB: w.rt, Env: "stg"}
	ok(t, again.LandingDrillBranch(ctx, remote, op, at.Add(2*time.Minute)))
	if remote.opens != 0 {
		t.Fatal("recovery replayed")
	}
}
