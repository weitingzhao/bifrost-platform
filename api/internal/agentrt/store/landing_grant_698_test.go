package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Regressions of code review 698 that need the database (W-31 core C-5).

// grantPrepared is a core root of M-3, grants enabled, claimed and
// prepared at branch_pushed with no CI kicked yet.
func grantPrepared(t *testing.T) (*world, string, string, store.LandingSource, time.Time) {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Second)
	t.Cleanup(store.FreezeEventClock(at))
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "698", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	enableGrants(t, w, signer{t, rptest.NewKey(t)}, 1, true)
	src := c4Source(c5Core)
	store.FreezeEventClock(at.Add(-30 * time.Minute))
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("claim", d)
	}
	c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
	store.FreezeEventClock(at.Add(-28 * time.Minute))
	return w, root, op, src, at
}

type ciCalls struct{ legacy, pinned []string }

func pinSources(src store.LandingSource, calls *ciCalls, ci *landing.CIRun, mainCI string) controller.DrillSources {
	return controller.DrillSources{Facts: src,
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, r string, _ time.Time) (landing.CIRun, error) {
			if r == commitA {
				return landing.CIRun{Run: "main", R: commitA, Status: mainCI, CreatedAt: time.Now()}, nil
			}
			return *ci, nil
		},
		EnsureCI: func(_ context.Context, _, name, _ string) error {
			calls.legacy = append(calls.legacy, name)
			return nil
		},
		EnsurePinnedCI: func(_ context.Context, name string, _ store.GrantCIKey) error {
			calls.pinned = append(calls.pinned, name)
			return nil
		},
		SiblingHeads: func(context.Context) (map[string]string, error) {
			return map[string]string{"bifrost-ui": commitC, "bifrost-trade-infra": commitA}, nil
		}}
}

func liveController(w *world) controller.Controller {
	return controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}, OpenLive: func(context.Context, string, string) (controller.LiveWorkspace, error) {
		return &livePusher{outcome: landinggit.PushOK}, nil
	}}
}

// Serial: FreezeEventClock. B-01: the push webhook's run of R is green before
// any kick: the operation that needs a grant still gets its pinned run
// first (ci_kick with its key), and is not taken to the gate on the
// webhook's run. Its webhook red with main green, the kick already used by
// v1: the pinned run rides on the rerun, the first key accepted there.
// (On 082125dd: ready, awaiting_grant with no key; and "fixed at the first
// kick".)
func TestC5B01PinnedRunComesFirst(t *testing.T) {
	t.Run("webhook green", func(t *testing.T) {
		w, _, op, src, at := grantPrepared(t)
		var calls ciCalls
		webhook := landing.CIRun{Run: "webhook", R: commitB, Status: "Succeeded", CreatedAt: at.Add(-28 * time.Minute)}
		ctl := liveController(w)
		a, err := ctl.LandingDrillCI(ctx, pinSources(src, &calls, &webhook, "Succeeded"), op, at.Add(-28*time.Minute))
		ok(t, err)
		key, err := store.LandingCIKey(ctx, w.rt, op)
		ok(t, err)
		if a.Kind != "ci_kick" || len(calls.pinned) != 1 || key == nil {
			t.Fatalf("%+v calls %+v key %v", a, calls, key)
		}
		if p, _ := phaseOf(t, w, op); p != "branch_pushed" {
			t.Fatal("taken to the gate on the webhook's run:", p)
		}
	})
	t.Run("webhook red, kick used", func(t *testing.T) {
		w, _, op, src, at := grantPrepared(t)
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_kick", "try", 1)))
		var calls ciCalls
		webhook := landing.CIRun{Run: "webhook", R: commitB, Status: "Failed", CreatedAt: at.Add(-28 * time.Minute)}
		ctl := liveController(w)
		a, err := ctl.LandingDrillCI(ctx, pinSources(src, &calls, &webhook, "Succeeded"), op, at.Add(-28*time.Minute))
		ok(t, err)
		key, err := store.LandingCIKey(ctx, w.rt, op)
		ok(t, err)
		if a.Kind != "ci_rerun" || len(calls.pinned) != 1 || !strings.HasSuffix(calls.pinned[0], "-2") || key == nil {
			t.Fatalf("%+v calls %+v key %v", a, calls, key)
		}
	})
	t.Run("crash after the pinned kick's intent", func(t *testing.T) {
		w, _, op, src, at := grantPrepared(t)
		key := store.GrantCIKey{Repo: "bifrost-platform", Revision: commitB, Base: commitA, Siblings: map[string]string{"bifrost-ui": commitC, "bifrost-trade-infra": commitA}}
		ok(t, store.AppendCIKick(ctx, w.rt, op, "ci_kick", 1, key, "test: the intent, then a crash"))
		var calls ciCalls
		other := landing.CIRun{Run: "webhook", R: commitB, Status: "Succeeded", CreatedAt: at.Add(-28 * time.Minute)}
		ctl := liveController(w)
		_, err := ctl.LandingDrillCI(ctx, pinSources(src, &calls, &other, "Succeeded"), op, at.Add(-28*time.Minute))
		ok(t, err)
		if len(calls.pinned) == 0 || calls.pinned[0] != landing.CIName(op, 1) || len(calls.legacy) != 0 {
			t.Fatalf("the pinned run was not made sure of: %+v", calls)
		}
	})
}

// Serial: FreezeEventClock. R-01: a waiting operation with no grant is
// rechecked under a rule version until the gate's stable answer (soft
// failures remain) — a failed workspace open is tried again after a while,
// not dropped for the version; once the stable answer is in, nothing more
// until the next rule version, which pushes it. (On 082125dd the version was
// spent on the failed open: one open until a restart.)
func TestC5R01RecheckRetriesUntilItsStableAnswer(t *testing.T) {
	const page = "console/src/pages/C5Page.tsx"
	c := c5Wait(t, page, true, true)
	p := &livePusher{outcome: landinggit.PushOK}
	opens, fail := 0, true
	clock := c.at
	src := controller.DrillSources{Facts: c.src,
		Main:          func(context.Context, string) (string, error) { return c.main, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI:            func(context.Context, string, string, time.Time) (landing.CIRun, error) { return c.ci, nil },
		EnsureCI:      func(context.Context, string, string, string) error { return nil }}
	ctl := controller.Controller{DB: c.w.rt, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}, Drill: &src, Now: func() time.Time { return clock },
		OpenLive: func(context.Context, string, string) (controller.LiveWorkspace, error) {
			opens++
			if fail {
				fail = false
				return nil, errors.New("gitea unreachable")
			}
			return p, nil
		}}
	if err := ctl.LandingDrillPass(ctx); err == nil || opens != 1 {
		t.Fatal("the failing open", err, opens)
	}
	clock = c.at.Add(5 * time.Minute)
	_ = ctl.LandingDrillPass(ctx)
	if opens != 1 {
		t.Fatal("retried at once", opens)
	}
	clock = c.at.Add(11 * time.Minute)
	ok(t, ctl.LandingDrillPass(ctx))
	if opens != 2 || len(p.pushes) != 0 || c.phase(t) != "awaiting_grant" {
		t.Fatal("not retried after a while", opens, p.pushes)
	}
	clock = c.at.Add(40 * time.Minute)
	ok(t, ctl.LandingDrillPass(ctx))
	if opens != 2 {
		t.Fatal("rechecked after the stable answer", opens)
	}
	looser := strings.Replace(c5RuleBody, `allow: ["console/src/components/**/*.tsx"]`, `allow: ["console/src/components/**/*.tsx", "console/src/pages/*.tsx"]`, 1)
	ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "loosen", since, until, looser))))
	ok(t, ctl.LandingDrillPass(ctx))
	if len(p.pushes) != 1 {
		t.Fatal("the new rule version did not push it", opens, p.pushes)
	}
}

// Serial: FreezeEventClock. R-02: a newer run of R than the one the grant
// names — running or red — ends the grant on the record: no push, the
// status says grant_refused, the grant is not valid, W runs again. (On
// 082125dd the gate stopped at the ordinary CI check: still authorized.)
func TestC5R02NewerRunEndsTheGrant(t *testing.T) {
	for _, status := range []string{"Running", "Failed"} {
		t.Run(status, func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			c.grant(t, store.ActionCoreLanding)
			c.ci.Run, c.ci.UID, c.ci.Status, c.ci.CreatedAt = "newer", "uid-newer", status, c.ci.CreatedAt.Add(time.Minute)
			if _, d := c.gate(t); d.Allowed || d.Reason != "grant_stale" || c.reservations(t) != 0 {
				t.Fatalf("%+v", d)
			}
			st, err := store.ReadLandingWait(ctx, c.w.rt, c.op)
			ok(t, err)
			if st.Shown != "grant_refused" || st.Grants[0].Valid || !st.Grants[0].Refused {
				t.Fatalf("%+v", st)
			}
			before := st.WaitingSeconds
			store.FreezeEventClock(c.at.Add(time.Hour))
			if st, _ = store.ReadLandingWait(ctx, c.w.rt, c.op); st.WaitingSeconds != before+3600 {
				t.Fatal("W paused by a refused grant", before, st.WaitingSeconds)
			}
		})
	}
}

// Serial: FreezeEventClock. R-03: main moved, the operation is discarded:
// its unspent grant is shown not valid, ended operation_discarded — never
// "authorized" beside a discarded operation. (On 082125dd valid stayed true.)
func TestC5R03DiscardedOperationsGrantIsNotValid(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	c.grant(t, store.ActionCoreLanding)
	c.main = commitC
	if _, d := c.gate(t); d.Allowed || c.phase(t) != "discarded" {
		t.Fatal(d, c.phase(t))
	}
	st, err := store.ReadLandingWait(ctx, c.w.rt, c.op)
	ok(t, err)
	var grant map[string]any
	raw, err := json.Marshal(st.Grants)
	ok(t, err)
	var grants []map[string]any
	ok(t, json.Unmarshal(raw, &grants))
	if len(grants) == 1 {
		grant = grants[0]
	}
	if st.Shown == "authorized" || grant == nil || grant["valid"] != false || grant["ended"] != "operation_discarded" {
		t.Fatalf("%s %v", st.Shown, grant)
	}
}
