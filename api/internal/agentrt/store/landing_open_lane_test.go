package store_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The open lane (rules.LandingOpen, checks version 3) through the store's
// claim and final gate, on the real database. Each world is c5's: PROD live,
// grants enabled, the root accepted.

// openLanePath is open and outside the floor: refusing it is rule, never floor.
const openLanePath = "console/vitest.config.ts"

// openLaneV2Core is open under checks version 3; under version 2 every file
// of the runtime store was core.
const openLaneV2Core = "api/internal/agentrt/store/planpace.go"

// openLaneSource reads one change per path, each adding added lines.
func openLaneSource(added int, paths ...string) store.LandingSource {
	src := c4Source(paths[0])
	src.Result = func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
		var out []rules.LandingChange
		for _, p := range paths {
			out = append(out, rules.LandingChange{OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: added, Deleted: 1, AddedText: "<p>Ready</p>"})
		}
		return true, out, nil
	}
	return src
}

func openLaneAccept(t *testing.T) func(*world, string) {
	return func(w *world, root string) { w.passRound(t, root, "h1", "h2", "h3") }
}

// openLaneIntent is what op's main_intent carries: landing_rule_clear and
// its checks_version, and whether a grant was consumed.
func openLaneIntent(t *testing.T, c *c5) (clear bool, checks int, use bool) {
	t.Helper()
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json ? 'landing_rule_clear', COALESCE((body_json->'landing_rule_clear'->>'checks_version')::int, 0),
	    body_json ? 'landing_grant_use' FROM agentrt.event
	  WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent' AND body_json->'landing'->>'op'=$2`, landingRepoStream, c.op).Scan(&clear, &checks, &use))
	return clear, checks, use
}

// openLaneApprovals counts the grants and approval observations on the stream.
func openLaneApprovals(t *testing.T, c *c5) int {
	t.Helper()
	return c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1
	  AND (body_json ? 'landing_grant' OR body_json ? 'landing_grant_request')`, landingRepoStream)
}

// Serial: FreezeEventClock. An open-only change with green CI lands with no
// approval, regardless of the legacy size values.
func TestOpenLaneGreenLandsWithoutApproval(t *testing.T) {
	t.Run("within limits", func(t *testing.T) {
		c := c5Prepared(t, openLaneSource(1, openLanePath), false, true, openLaneAccept(t))
		store.FreezeEventClock(c.at)
		auth, d := c.gate(t)
		if !d.Allowed || auth.R != commitB || c.phase(t) != "main_intent" || c.reservations(t) != 1 {
			t.Fatal(d, auth, c.phase(t))
		}
		if clear, _, use := openLaneIntent(t, c); clear || use || openLaneApprovals(t, c) != 0 {
			t.Fatal("main_intent", clear, use)
		}
	})
	t.Run("large change", func(t *testing.T) {
		c := c5Prepared(t, openLaneSource(10000, openLanePath), false, true, openLaneAccept(t))
		store.FreezeEventClock(c.at)
		if _, d := c.gate(t); !d.Allowed || c.phase(t) != "main_intent" || openLaneApprovals(t, c) != 0 {
			t.Fatal(d, c.phase(t))
		}
	})

}

// Serial: FreezeEventClock. An open change whose CI is red does not land:
// no main_intent, no quota, the branch stays pushed.
func TestOpenLaneRedCIDoesNotLand(t *testing.T) {
	c := c5Prepared(t, openLaneSource(1, openLanePath), false, true, openLaneAccept(t))
	c.ci.Status = "Failed"
	store.FreezeEventClock(c.at)
	auth, d := c.gate(t)
	if d.Allowed || d.Reason != "ci" || auth.Op != "" || c.phase(t) != "branch_pushed" || c.reservations(t) != 0 {
		t.Fatal(d, auth, c.phase(t))
	}
}

// Serial: FreezeEventClock. An open path beside a permission (core) path
// does not open the change: it waits on core and asks approve_core_landing,
// naming only the core path.
func TestOpenLaneMixedAsksCore(t *testing.T) {
	c := c5Prepared(t, openLaneSource(1, openLanePath, c5Core), false, true, openLaneAccept(t))
	store.FreezeEventClock(c.at.Add(-20 * time.Minute))
	if _, d := c.gate(t); d.Reason != "awaiting_grant" {
		t.Fatal("mixed change not waiting", d)
	}
	store.FreezeEventClock(c.at)
	m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
	ok(t, err)
	if m.Action != store.ActionCoreLanding || !slices.Contains(m.SoftAtRequest, "core") || len(m.Candidate.HardCorePaths) != 1 {
		t.Fatalf("material: %+v", m)
	}
	if p := m.Candidate.HardCorePaths[0]; !strings.HasPrefix(c5Core, p.Entry) || p.Entry == "" {
		t.Fatalf("core path: %+v", p)
	}
}

// Serial: FreezeEventClock. A signed deny still refuses an open path, as
// rule: the claim prepares nothing, even with grants enabled; without the
// deny the same change claims.
func TestOpenLaneDenyRefusedAsRule(t *testing.T) {
	for _, deny := range []bool{false, true} {
		c := c5Ready(t, openLaneSource(1, openLanePath), false, true, openLaneAccept(t))
		if deny {
			denied := strings.Replace(c5RuleBody, "deny: []", `deny: ["`+openLanePath+`"]`, 1)
			ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "tighten", since, until, denied))))
		}
		store.FreezeEventClock(c.at.Add(-30 * time.Minute))
		op, d, err := store.ClaimLanding(ctx, c.w.rt, c.src, "prod", c.root, c.at)
		ok(t, err)
		if !deny {
			if !d.Allowed || op == "" {
				t.Fatal("control", d)
			}
			continue
		}
		if d.Allowed || d.Reason != "rule" || op != "" {
			t.Fatal(d, op)
		}
		if len(landingState(t, c.w, "landing-quota:dev:"+c.at.Format("2006-01-02")).Reservations) != 0 {
			t.Fatal("refusal reserved quota")
		}
	}
}

// Serial: FreezeEventClock. An operation that waited on core under checks
// version 2 (its awaiting_grant written as that gate wrote it) with a
// pending approve_core_landing card, whose change is open-only under version
// 3: the recheck lands it on the rule (landing_rule_clear, checks_version 3)
// with no grant, and the controller's next requests pass withdraws the
// pending card from the Owner's list, once; while it waited, it did not.
func TestOpenLaneV2CoreWaitClearsAndSettlesItsApproval(t *testing.T) {
	c := c5Prepared(t, openLaneSource(1, openLaneV2Core), false, true, openLaneAccept(t))
	store.FreezeEventClock(c.at.Add(-20 * time.Minute))
	ok(t, appendLanding(c.w, landingRepoStream, landing.CIRecord(c.op, c.ci)))
	ok(t, appendLanding(c.w, landingRepoStream, newLandingRecord(c.op, "awaiting_grant", "soft", []any{"core"}, "files", 1, "lines", 2,
		"material", "sha256:"+strings.Repeat("c", 64))))
	if c.phase(t) != "awaiting_grant" {
		t.Fatal("v2 wait", c.phase(t))
	}
	store.FreezeEventClock(c.at.Add(-10 * time.Minute))
	pending := store.LandingGrantRequest{Op: c.op, R: commitB, ApprovalID: "appr_open_v2", ApprovalNumber: 900, Status: "pending",
		ParamsHash: strings.Repeat("d", 64), SeenAt: c.at.Add(-10 * time.Minute)}
	wrote, err := store.RecordLandingGrantRequest(ctx, c.w.rt, pending)
	ok(t, err)
	if !wrote {
		t.Fatal("pending card not recorded")
	}

	approvals := []store.LandingGrantRequest{pending}
	var settled []string
	ctl := controller.Controller{DB: c.w.rt, Env: "prod", Reconcile: &controller.LandingReconcile{
		Requests: func(context.Context) ([]store.LandingGrantRequest, error) { return approvals, nil },
		SettleGrant: func(_ context.Context, id, reason string) error {
			settled = append(settled, id)
			if !strings.Contains(reason, c.op) {
				t.Error("reason", reason)
			}
			return nil
		}}}
	ok(t, ctl.LandingRequestsPass(ctx))
	if len(settled) != 0 {
		t.Fatal("a waiting operation's card withdrawn", settled)
	}

	store.FreezeEventClock(c.at)
	_, d, err := store.RecheckWaitingLanding(ctx, c.w.rt, c.src, c.op, c.observe())
	ok(t, err)
	if !d.Allowed || c.phase(t) != "main_intent" || c.reservations(t) != 1 {
		t.Fatal(d, c.phase(t))
	}
	clear, checks, use := openLaneIntent(t, c)
	grants := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant'`, landingRepoStream)
	if !clear || checks != 3 || use || grants != 0 {
		t.Fatal("main_intent", clear, checks, use, grants)
	}

	if got := store.SettledLandingGrantRequests(landingState(t, c.w, landingRepoStream), approvals); len(got) != 1 || got[0].ApprovalID != pending.ApprovalID {
		t.Fatal("settled", got)
	}
	ok(t, ctl.LandingRequestsPass(ctx))
	if !slices.Equal(settled, []string{pending.ApprovalID}) {
		t.Fatal("pending card left on the Owner's list", settled)
	}
	// The approval service now has it expired: the pass records that and
	// withdraws nothing more.
	expired := pending
	expired.Status, expired.SeenAt = "expired", c.at
	approvals = []store.LandingGrantRequest{expired}
	ok(t, ctl.LandingRequestsPass(ctx))
	if len(settled) != 1 {
		t.Fatal("withdrawn twice", settled)
	}
	if n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing_grant_request'->>'approval_id'=$2
	  AND body_json->'landing_grant_request'->>'status'='expired'`, landingRepoStream, pending.ApprovalID); n != 1 {
		t.Fatal("expiry not recorded", n)
	}
}
