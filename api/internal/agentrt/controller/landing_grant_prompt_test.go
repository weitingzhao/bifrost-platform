package controller_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A granted landing pushes within a minute of its grant when its CI is
// green, though another operation of the repository is still in its CI: only
// an operation past its CI holds the slot against a granted landing. The
// controller's clock (Controller.Now) is the rig's event clock.

// promptRig is a rig with grants whose core root A waits in awaiting_grant,
// its pinned CI green; aR is its R.
func promptRig(t *testing.T) (r *batchRig, gsrc store.GrantSources, a, aR string) {
	t.Helper()
	r = newBatchRig(t, true)
	r.ctl.Now = r.now
	gsrc = r.pinnedGrants()
	aR = strings.Repeat("5", 40)
	a = r.pushedAlone(t, r.accepted(t, "api/internal/agentrt/store/prompt_a.go"), aR)
	r.mu.Lock()
	r.refs[r.state(t).Operations[a].Ref] = aR
	r.mu.Unlock()
	r.status = "Succeeded"
	for range 2 {
		r.clock(time.Minute)
		r.pass(t)
	}
	if o := r.state(t).Operations[a]; o.Phase != "awaiting_grant" {
		t.Fatalf("A: %s, want awaiting_grant", o.Phase)
	}
	return r, gsrc, a, aR
}

// running writes task's operation 1 to branch_pushed on rev, whose CI then
// reports running whatever the rig's status.
func (r *batchRig) running(t *testing.T, task, rev string) string {
	t.Helper()
	op := r.pushedAlone(t, task, rev)
	r.mu.Lock()
	r.refs[r.state(t).Operations[op].Ref] = rev
	r.mu.Unlock()
	ci := r.src.CI
	r.src.CI = func(ctx context.Context, repo, r string, after time.Time) (landing.CIRun, error) {
		run, err := ci(ctx, repo, r, after)
		if r == rev && run.Run != "" {
			run.Status = "Running"
		}
		return run, err
	}
	return op
}

// grant asks the Owner's card for op and writes its approval's grant.
func (r *batchRig) grant(t *testing.T, gsrc store.GrantSources, op string) {
	t.Helper()
	var asks []coresAsk
	var requests []store.LandingGrantRequest
	r.ctl.Reconcile = &controller.LandingReconcile{Repos: []string{"bifrost-platform"},
		Requests: func(context.Context) ([]store.LandingGrantRequest, error) { return slices.Clone(requests), nil },
		Grants:   func() store.GrantSources { return gsrc },
		RequestGrant: func(_ context.Context, action string, params map[string]any, reason string) (string, error) {
			asks = append(asks, coresAsk{action, params, reason})
			id := fmt.Sprintf("approval_prompt_%d", len(asks))
			requests = append(requests, store.LandingGrantRequest{Op: params["op"].(string), R: params["R"].(string), ApprovalID: id,
				ApprovalNumber: int64(len(asks)), Status: "pending", ParamsHash: strings.Repeat("a", 64), SeenAt: r.now()})
			return id, nil
		}}
	for range 2 {
		must(t, r.ctl.LandingRequestsPass(ctx))
		r.clock(time.Second)
	}
	if len(asks) != 1 || asks[0].params["op"] != op {
		t.Fatalf("cards asked: %+v", asks)
	}
	p, err := store.DecodeGrantParams(asks[0].params)
	must(t, err)
	_, err = store.WriteLandingGrant(ctx, r.pool, gsrc, store.GrantApproval{ID: "approval_prompt_1", Number: 1, Action: asks[0].action,
		ParamsHash: strings.Repeat("a", 64), ApprovedAt: r.now().Add(-time.Second)}, p)
	must(t, err)
}

func TestLandingGrantPushesWithinAMinuteBesideCI(t *testing.T) {
	r, gsrc, a, aR := promptRig(t)
	// B: an operation of the same repository in branch_pushed, its CI
	// running.
	b := r.running(t, r.accepted(t, "console/src/components/PromptB.tsx"), strings.Repeat("6", 40))
	r.clock(15 * time.Second)
	r.pass(t)
	if o := r.state(t).Operations[b]; o.Phase != "branch_pushed" || !slices.Contains(r.ensured, landing.CIName(b, 1)) {
		t.Fatalf("B: %s, CI %v; want branch_pushed with its CI kicked", o.Phase, r.ensured)
	}
	r.grant(t, gsrc, a)
	granted := r.now()
	before := len(r.state(t).Operations)
	for r.now().Sub(granted) <= time.Minute && !r.state(t).Operations[a].AtMain() {
		r.clock(15 * time.Second)
		r.pass(t)
	}
	s := r.state(t)
	if o := s.Operations[a]; !o.AtMain() || r.now().Sub(granted) > time.Minute {
		t.Fatalf("A: %s %s %s after its grant, want main_intent recorded", o.Phase, o.Outcome, r.now().Sub(granted))
	}
	if !slices.Equal(r.pushes, []string{aR + "<-" + batchM0}) {
		t.Fatal("pushes", r.pushes)
	}
	if len(s.Operations) != before {
		t.Fatal("a new claim while A landed", len(s.Operations), before)
	}
	// B waits (parked behind A's push to main); it does not end.
	if o := s.Operations[b]; o.Terminal() {
		t.Fatalf("B: %s %s before main moved", o.Phase, o.Outcome)
	}
	// Main moved to A's R: B's next pass discards it.
	r.src.Main = func(context.Context, string) (string, error) { return aR, nil }
	r.clock(15 * time.Second)
	_ = r.ctl.LandingDrillPass(ctx) // the k+1 the discard opens is not this test's
	if o := r.state(t).Operations[b]; o.Phase != "discarded" || o.Outcome != "main_changed" {
		t.Fatalf("B: %s %s, want discarded main_changed", o.Phase, o.Outcome)
	}
}

// A's grant is refused for the limit while C holds main_intent. C ends
// (never sent, main still M0) and B is claimed into its CI in that same
// pass: the pass after, A is tried and pushes. B in its CI does not hold the
// slot against a granted landing, in the probe as in the final gate.
func TestLandingGrantRefusedForMainIntentTriedAfterItEnds(t *testing.T) {
	r, gsrc, a, aR := promptRig(t)
	cR := strings.Repeat("7", 40)
	c := r.pushedAlone(t, r.accepted(t, "console/src/components/PromptC.tsx"), cR)
	for _, rec := range []map[string]any{
		{"kind": "ci_seen", "run": landing.CIName(c, 1), "status": "Succeeded", "R": cR, "created_at": r.now().UTC().Format(time.RFC3339)},
		{"kind": "main_intent", "R": cR, "M0": batchM0},
	} {
		r.clock(time.Second)
		rec["v"], rec["op"] = landing.Version, c
		must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller",
				TaskID: r.state(t).Operations[c].Task, Body: map[string]any{"text": "test: " + rec["kind"].(string), "landing": rec}})
			return err
		}))
	}
	r.grant(t, gsrc, a)
	r.clock(15 * time.Second)
	_ = r.ctl.LandingDrillPass(ctx)
	if o := r.state(t).Operations[a]; o.Phase != "awaiting_grant" || len(r.pushes) != 0 {
		t.Fatalf("A: %s, pushes %v; want refused while C holds main_intent", o.Phase, r.pushes)
	}
	// C was never sent: past its fence, main still M0 without its R.
	r.clock(store.MainFence + time.Minute)
	_, err := store.ReconcileMain(ctx, r.pool, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
		return store.MainEvidence{Head: batchM0, Validity: "effective"}, nil
	})
	must(t, err)
	if o := r.state(t).Operations[c]; !o.Terminal() {
		t.Fatalf("C: %s %s, want ended", o.Phase, o.Outcome)
	}
	b := r.running(t, r.accepted(t, "console/src/components/PromptB.tsx"), strings.Repeat("6", 40))
	r.clock(15 * time.Second)
	_ = r.ctl.LandingDrillPass(ctx) // C's root opens its k+1, not this test's
	if o := r.state(t).Operations[a]; !o.AtMain() || !slices.Equal(r.pushes, []string{aR + "<-" + batchM0}) {
		t.Fatalf("A: %s %s, pushes %v; want tried the pass after C ended", o.Phase, o.Outcome, r.pushes)
	}
	if o := r.state(t).Operations[b]; o.Terminal() {
		t.Fatalf("B: %s %s", o.Phase, o.Outcome)
	}
}
