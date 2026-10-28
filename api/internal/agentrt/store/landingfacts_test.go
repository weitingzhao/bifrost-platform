package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const landingRuleBody = `tenant: dev
repos:
  - repo: bifrost-platform
    allow: ["console/src/components/**/*.tsx"]
    deny: []
    stg_release: bifrost-deliver-platform
limits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}
`

// Serial: FreezeEventClock.
func TestLandingEnableObservationSurvivesRuleVersion(t *testing.T) {
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, until, landingRuleBody))
	ok(t, err)
	observed := at("2026-10-24T12:00:00Z")
	restore := store.FreezeEventClock(observed)
	defer restore()
	observe := func(now time.Time) time.Time {
		t.Helper()
		var enabled time.Time
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var e error
			enabled, e = store.ObserveLandingEnabled(ctx, tx, "stg", "dev", "bifrost-platform", now)
			return e
		}))
		return enabled
	}
	if got := observe(observed); !got.Equal(observed) {
		t.Fatal("signed midnight used as fence", got)
	}
	_, err = store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, landingRuleBody)))
	ok(t, err)
	if got := observe(observed.Add(time.Hour)); !got.Equal(observed) {
		t.Fatal("version moved enable fence", got)
	}
}
func TestLandingFactsUseAdoptionOrderAndFreshHistory(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	// Create the old planner first, but adopt it last. A creation-time lookup
	// would choose the new planner and wrongly refuse this still-wanted task.
	old := w.ready(t, store.Task{Role: "plan", Effect: "none"})
	w.accept(t, old)
	newer := w.ready(t, store.Task{Role: "plan", Effect: "none"})
	w.accept(t, newer)
	use := func(id string) {
		t.Helper()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, e := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller", MissionID: "M-1", TaskID: id, Body: map[string]any{"from": "submitted", "to": "accepted", "reason": "plan used: fixture"}})
			return e
		}))
	}
	use(newer)
	use(old)
	// passRound pushes commitA, so the task must start from a different base.
	impl := w.ready(t, store.Task{OffPlanReason: "planned by task " + old, BaseCommit: commitB})
	w.passRound(t, impl, "h1", "h2", "h3")
	reads := 0
	src := store.LandingSource{
		Result: func(_ context.Context, repo, task, attempt, base, commit string) (bool, []rules.LandingChange, error) {
			if task != impl || attempt == "" || commit != commitA || base != commitB {
				return false, nil, fmt.Errorf("wrong accepted result")
			}
			return true, nil, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			reads++
			return landing.MainHistory{State: landing.Never}, nil
		},
	}
	read := func() (landing.Facts, error) {
		var f landing.Facts
		err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var e error
			f, e = store.LandingFacts(ctx, tx, src, "prod", "live", impl, "", "", time.Now())
			return e
		})
		return f, err
	}
	f, err := read()
	ok(t, err)
	if f.LatestUsedPlan != old || f.ProducingPlan != old || !f.GoalTask || f.AcceptedAt.IsZero() {
		t.Fatal("wrong facts", f)
	}
	use(newer)
	f, err = read()
	ok(t, err)
	if f.LatestUsedPlan != newer || reads != 2 {
		t.Fatal("stale adoption or history", f, reads)
	}
	src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{}, errors.New("history unavailable")
	}
	if _, err = read(); err == nil {
		t.Fatal("unreadable history became never")
	}
}

func TestLandingFactsReadDroppedStep(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	// Bind the task when it is proposed; an accepted off-plan task cannot be
	// moved onto a step with a legacy import. Drop through the plan-change API.
	impl := w.ready(t, store.Task{PlanNode: s1, BaseCommit: commitB})
	w.passRound(t, impl, "h1", "h2", "h3")
	src := store.LandingSource{
		Result: func(_ context.Context, repo, task, attempt, base, commit string) (bool, []rules.LandingChange, error) {
			if task != impl || attempt == "" || commit != commitA || base != commitB {
				return false, nil, fmt.Errorf("wrong accepted result")
			}
			return true, nil, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			return landing.MainHistory{State: landing.Never}, nil
		},
	}
	read := func() landing.Facts {
		t.Helper()
		var f landing.Facts
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			f, err = store.LandingFacts(ctx, tx, src, "prod", "live", impl, "", "", time.Now())
			return err
		}))
		return f
	}
	before := read()
	if before.StepState != "planned" || before.TaskState != "accepted" {
		t.Fatal("wrong initial step facts", before)
	}
	a := &asker{}
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "landing-drop", root, "test", []store.PlanChange{{Op: "drop_step", Node: s1}}, a.ask)
	ok(t, err)
	a.answer(out["question_id"].(string), store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)
	if n != 1 {
		t.Fatalf("decided %d plan changes; want 1", n)
	}
	f := read()
	if f.StepState != "dropped" || f.StepVersion <= before.StepVersion || f.TaskState != "accepted" {
		t.Fatal("dropped step hidden", f)
	}
}
