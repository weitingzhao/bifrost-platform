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
			enabled, e = store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", now)
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
	impl := w.ready(t, store.Task{OffPlanReason: "planned by task " + old, BaseCommit: commitA})
	w.passRound(t, impl, "h1", "h2", "h3")
	reads := 0
	src := store.LandingSource{
		Result: func(_ context.Context, repo, task, attempt, base, commit string) (bool, []rules.LandingChange, error) {
			if task != impl || attempt == "" || commit == "" || base != commitA {
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
	w.legacy(t, fmt.Sprintf(`UPDATE agentrt.task SET off_plan=false,off_plan_reason=NULL,plan_node_id='%s' WHERE task_id='%s'`, s1, impl), fmt.Sprintf(`UPDATE agentrt.plan_node SET state='dropped',version=version+1 WHERE plan_node_id='%s'`, s1))
	src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Never}, nil
	}
	f, err = read()
	ok(t, err)
	if f.StepState != "dropped" || f.StepVersion == 0 {
		t.Fatal("dropped step hidden", f)
	}
}
