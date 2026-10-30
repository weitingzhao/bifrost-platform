package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock. These use the real PostgreSQL migration/runtime
// role harness; without AGENTRT_TEST_PG they skip, never simulate a database.
func td462Root(t *testing.T, c *c5) string {
	t.Helper()
	root := c.w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "TD-462", BaseCommit: commitB})
	c.w.passRound(t, root, "h1", "h2", "h3")
	return root
}

func td462Controller(c *c5, p *livePusher) *controller.Controller {
	var calls ciCalls
	src := pinSources(c.src, &calls, &c.ci, "Succeeded")
	ctl := liveController(c.w)
	ctl.Drill, ctl.Now = &src, func() time.Time { return store.DecisionAt() }
	ctl.OpenLive = func(context.Context, string, string) (controller.LiveWorkspace, error) { return p, nil }
	return &ctl
}

// Serial: FreezeEventClock.
func TestTD462GrantedWaitingPrecedesNewClaimPG(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	c.grant(t, store.ActionCoreLanding)
	root := td462Root(t, c)
	p := &livePusher{outcome: landinggit.PushOK}
	ctl := td462Controller(c, p)
	blocked := true
	ctl.Drill.WindowBlocked = func(context.Context, string) (bool, error) { return blocked, nil }
	ok(t, ctl.LandingDrillPass(ctx))
	if len(p.pushes) != 0 || c.reservations(t) != 0 || landingState(t, c.w, landingRepoStream).LatestOperation(root) != "" {
		t.Fatal("new root claimed or quota consumed while an authorized operation waited")
	}
	// Window opens without any landing-stream transition: the next scan
	// must push the approved operation, with the new root still unclaimed.
	blocked = false
	store.FreezeEventClock(c.at.Add(time.Second))
	ok(t, ctl.LandingDrillPass(ctx))
	if len(p.pushes) != 1 || c.phase(t) != "main_result" || c.reservations(t) != 1 {
		t.Fatal("authorized operation did not push next scan", p.pushes, c.phase(t))
	}
	if landingState(t, c.w, landingRepoStream).LatestOperation(root) != "" {
		t.Fatal("new claim displaced the authorized operation")
	}
	if usable, err := store.UsableLandingGrant(ctx, c.w.rt, c.op); err != nil || usable != "" {
		t.Fatal("the gate did not consume its grant", usable, err)
	}
	// Reconciliation releases the active slot; the consumed grant does
	// not hold claims after the operation lands.
	ok(t, appendLanding(c.w, landingRepoStream, newLandingRecord(c.op, "landed", "R", commitB)))
	op, d, err := store.ClaimLanding(ctx, c.w.rt, c.src, "prod", root, store.DecisionAt())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("landed grant kept the new root waiting", d)
	}
}

// Serial: FreezeEventClock.
func TestTD462GrantedRetryAfterAwaitingGrantPG(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	root := td462Root(t, c)
	// Another claim wins before the approval is written.
	op, d, err := store.ClaimLanding(ctx, c.w.rt, c.src, "prod", root, c.at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("in-flight claim", d)
	}
	prepared := c.at
	c4Prepare(t, c.w, root, op, prepared, true)
	ok(t, store.AppendCIKick(ctx, c.w.rt, op, "ci_kick", 1, c.key, "TD-462: in-flight CI"))
	c.grant(t, store.ActionCoreLanding)
	p := &livePusher{outcome: landinggit.PushOK}
	ctl := td462Controller(c, p)
	opens := 0
	ctl.OpenLive = func(context.Context, string, string) (controller.LiveWorkspace, error) {
		opens++
		return p, nil
	}
	ctl.Drill.CI = func(_ context.Context, _, r string, after time.Time) (landing.CIRun, error) {
		if after.Equal(prepared) {
			return landing.CIRun{Run: landing.CIName(op, 1), R: r, Status: "Running", CreatedAt: prepared}, nil
		}
		return c.ci, nil
	}
	ok(t, ctl.LandingDrillPass(ctx))
	if opens != 1 || len(p.pushes) != 0 || c.reservations(t) != 0 {
		t.Fatal("the in-flight limit was bypassed", opens, p.pushes)
	}
	store.FreezeEventClock(c.at.Add(time.Second))
	ok(t, ctl.LandingDrillPass(ctx))
	if opens != 1 {
		t.Fatal("retried while the same operation remained in flight", opens)
	}
	// Its successful CI reaches the unchanged final gate and enters
	// awaiting_grant, which is no longer Active in the store's count.
	store.FreezeEventClock(c.at.Add(2 * time.Minute))
	_, d, err = store.AuthorizeLandingPush(ctx, c.w.rt, c.src, op, observed(prepared))
	ok(t, err)
	if d.Reason != "awaiting_grant" {
		t.Fatal("second operation did not free its slot", d)
	}
	store.FreezeEventClock(c.at.Add(2*time.Minute + time.Second))
	ok(t, ctl.LandingDrillPass(ctx))
	if len(p.pushes) != 1 || c.phase(t) != "main_result" || c.reservations(t) != 1 {
		t.Fatal("released slot did not wake the approved operation next scan", p.pushes, c.phase(t))
	}
}

// Serial: FreezeEventClock.
func TestTD462PriorityEndsWithGrantUsabilityPG(t *testing.T) {
	for _, end := range []string{"ungranted", "revoked", "expired", "disabled"} {
		t.Run(end, func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			if end != "ungranted" {
				g := c.grant(t, store.ActionCoreLanding)
				switch end {
				case "revoked":
					_, err := store.RevokeLandingGrant(ctx, c.w.rt, g.GrantID)
					ok(t, err)
				case "expired":
					expires, err := time.Parse(time.RFC3339Nano, g.ExpiresAt)
					ok(t, err)
					store.FreezeEventClock(expires)
				case "disabled":
					enableGrants(t, c.w, c.signer, 2, false)
				}
			}
			root := td462Root(t, c)
			src := c4Source("console/src/components/TD462.tsx")
			op, d, err := store.ClaimLanding(ctx, c.w.rt, src, "prod", root, store.DecisionAt())
			ok(t, err)
			if op == "" || !d.Allowed {
				t.Fatal("unusable grant blocked a new root", d)
			}
		})
	}
}

// Serial: FreezeEventClock.
func TestTD462PriorityRecheckedAfterExternalReadsPG(t *testing.T) {
	c := c5Wait(t, c5Core, false, true)
	root := td462Root(t, c)
	src := c4Source("console/src/components/TD462.tsx")
	read := src.Result
	src.Result = func(ctx context.Context, repo, task, attempt, a, base string) (bool, []rules.LandingChange, error) {
		c.grant(t, store.ActionCoreLanding)
		return read(ctx, repo, task, attempt, a, base)
	}
	op, d, err := store.ClaimLanding(ctx, c.w.rt, src, "prod", root, c.at)
	ok(t, err)
	if op != "" || d.Allowed || d.Reason != "limit" {
		t.Fatal("approval during external reads lost priority", op, d)
	}
}
