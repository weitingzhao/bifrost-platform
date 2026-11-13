package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A granted landing's final gate counts only landings past their CI as
// holding the slot (PROD 2026-11-06: a granted landing waited 17 minutes
// behind one claimed after it, still in CI).

// c5Other is c5Wait with a second root of the repository claimed and its
// branch pushed while the first waits without a grant (the wait holds no
// slot), then the first root's grant written. It answers the second
// operation and its plain source.
func c5Other(t *testing.T) (*c5, string, store.LandingSource) {
	t.Helper()
	ctx := ctxOf(t)
	c := c5Wait(t, c5Core, false, true)
	other := c.w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "C5 other root", BaseCommit: commitB})
	c.w.passRound(t, other, "h1", "h2", "h3")
	plain := c4Source("console/src/components/A.tsx")
	op2, d, err := store.ClaimLanding(ctx, c.w.rt, plain, "prod", other, c.at)
	ok(t, err)
	if !d.Allowed || op2 == "" {
		t.Fatal("second claim", d)
	}
	c4Prepare(t, c.w, other, op2, c.at, true)
	ok(t, store.AppendCIKick(ctx, c.w.rt, op2, "ci_kick", 1, c.key, "test: second root's kick"))
	// Claims count every active operation: a third root is refused the
	// slot the second holds while still in CI.
	third := c.w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "C5 third root", BaseCommit: commitB})
	c.w.passRound(t, third, "h1", "h2", "h3")
	if op, d, err := store.ClaimLanding(ctx, c.w.rt, plain, "prod", third, c.at); err != nil || op != "" || d.Reason != "limit" {
		t.Fatal("a claim did not count the operation in CI", op, d, err)
	}
	c.grant(t, store.ActionCoreLanding)
	freezeClock(t, c.at.Add(2*time.Minute))
	return c, op2, plain
}

// inFlight is OtherInFlight of the facts every caller but the final gate
// reads for op.
func (c *c5) inFlight(t *testing.T) int {
	t.Helper()
	ctx := ctxOf(t)
	var f landing.Facts
	ok(t, store.InTx(ctx, c.w.rt, func(tx pgx.Tx) error {
		var err error
		f, err = store.LandingFacts(ctx, tx, c.src, "prod", "live", c.root, c.op, "", store.DecisionAt(ctx))
		return err
	}))
	return f.OtherInFlight
}

// The second operation's CI runs (branch_pushed,
// kicked): the granted one is allowed and pushes; other readers still count
// the second operation.
func TestGrantSlotNotHeldByLandingInCI(t *testing.T) {
	t.Parallel()
	ownClock(t)
	c, op2, _ := c5Other(t)
	if p, _ := phaseOf(t, c.w, op2); p != "branch_pushed" {
		t.Fatal(p)
	}
	if n := c.inFlight(t); n != 1 {
		t.Fatal("facts outside the final gate stopped counting the landing in CI", n)
	}
	auth, d := c.gate(t)
	if !d.Allowed || auth.Op != c.op || c.phase(t) != "main_intent" {
		t.Fatalf("granted landing yielded to one in CI: %+v %+v", d, auth)
	}
}

// The second operation waits for its own card:
// the granted one is allowed.
func TestGrantSlotNotHeldByLandingAwaitingCard(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	c, op2, _ := c5Other(t)
	if _, d, err := store.AuthorizeLandingPush(ctx, c.w.rt, c4Source(c5Core), op2, observed(c.at)); err != nil || d.Reason != "awaiting_grant" {
		t.Fatal("second root not waiting", d, err)
	}
	auth, d := c.gate(t)
	if !d.Allowed || auth.Op != c.op || c.phase(t) != "main_intent" {
		t.Fatalf("granted landing yielded to one awaiting its card: %+v %+v", d, auth)
	}
}

// The second operation is pushing to main
// (main_intent): the granted one is refused for the limit and keeps waiting.
func TestGrantSlotHeldByLandingAtMain(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	c, op2, plain := c5Other(t)
	if auth, d, err := store.AuthorizeLandingPush(ctx, c.w.rt, plain, op2, observed(c.at)); err != nil || !d.Allowed || auth.Op != op2 {
		t.Fatal("second root's final gate", d, err)
	}
	auth, d := c.gate(t)
	if d.Allowed || auth.Op != "" || d.Reason != "limit" || c.phase(t) != "awaiting_grant" {
		t.Fatalf("granted landing passed one at main: %+v %+v", d, auth)
	}
}
