package controller_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.7 batch of cores: a batch of core roots asks the Owner one card,
// its lead's, and after that one approval lands every member with one push.

type coresAsk struct {
	action string
	params map[string]any
	reason string
}

// pinnedGrants wires the rig's CI to pin a waiting lead's run (C-5) and
// its material's Git and CI reads.
func (r *batchRig) pinnedGrants() store.GrantSources {
	r.src.SiblingHeads = func(context.Context) (map[string]string, error) {
		return map[string]string{"bifrost-ui": strings.Repeat("c", 40), "bifrost-trade-infra": strings.Repeat("d", 40)}, nil
	}
	r.src.EnsurePinnedCI = func(ctx context.Context, name string, key store.GrantCIKey) error {
		return r.src.EnsureCI(ctx, "bifrost-platform", name, key.Revision, key.Base)
	}
	r.src.Facts.GrantPatch = func(context.Context, string, string, string) (store.GrantPatch, error) {
		return store.GrantPatch{SHA256: strings.Repeat("7", 64), Shown: "diff --git a/x b/x\n", Size: 19, Match: true}, nil
	}
	r.src.Facts.GrantCI = func(_ context.Context, key store.GrantCIKey, after time.Time) (store.GrantCIEvidence, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, run := range r.runs {
			if run.R == key.Revision && run.CreatedAt.After(after) {
				return store.GrantCIEvidence{Pipeline: "bifrost-ci-platform", Run: run.Run, UID: "uid-" + run.Run, CreatedAt: run.CreatedAt,
					Status: r.status, Candidate: key.Revision, Siblings: key.Siblings}, nil
			}
		}
		return store.GrantCIEvidence{}, nil
	}
	return store.GrantSources{Facts: r.src.Facts, Main: func(context.Context) (string, error) { return batchM0, nil }}
}

// Two waiting roots each touching a permission path (core, grants
// enabled) form one batch: one landing CI, one card (the lead's, for the
// core action, its material the batch's), and after that one approval R is
// pushed to main once and both members land.
func TestBatchCoresOneCardOnePush(t *testing.T) {
	testBatchCoresOneCardOnePush(t, 2)
}

func testBatchCoresOneCardOnePush(t *testing.T, members int) {
	t.Helper()
	r := newBatchRig(t, true)
	gsrc := r.pinnedGrants()
	var roots []string
	for i := range members {
		roots = append(roots, r.accepted(t, fmt.Sprintf("api/internal/agentrt/store/cores_%d.go", i)))
	}
	b := r.formed(t, roots)
	if len(b.Members) != members || b.Lead != r.state(t).LatestOperation(roots[0]) {
		t.Fatalf("not one batch of all cores: %+v", b)
	}
	r.clock(time.Minute)
	r.status = "Succeeded"
	r.pass(t)
	for i, op := range r.latest(t, roots) {
		want := "branch_pushed"
		if i == 0 {
			want = "awaiting_grant"
		}
		if op.Phase != want {
			t.Fatalf("member %d phase = %s, want %s", i, op.Phase, want)
		}
	}
	var asks []coresAsk
	var requests []store.LandingGrantRequest
	r.ctl.Reconcile = &controller.LandingReconcile{Repos: []string{"bifrost-platform"},
		Requests: func(context.Context) ([]store.LandingGrantRequest, error) { return slices.Clone(requests), nil },
		Grants:   func() store.GrantSources { return gsrc },
		RequestGrant: func(_ context.Context, action string, params map[string]any, reason string) (string, error) {
			asks = append(asks, coresAsk{action, params, reason})
			id := fmt.Sprintf("approval_cores_%d", len(asks))
			requests = append(requests, store.LandingGrantRequest{Op: params["op"].(string), R: params["R"].(string), ApprovalID: id,
				ApprovalNumber: int64(len(asks)), Status: "pending", ParamsHash: strings.Repeat("a", 64), SeenAt: r.now()})
			return id, nil
		}}
	for range 2 {
		must(t, r.ctl.LandingRequestsPass(ctx))
		r.clock(time.Second)
	}
	if len(asks) != 1 || asks[0].action != store.ActionCoreLanding || asks[0].params["op"] != b.Lead || !strings.Contains(asks[0].reason, b.Batch) {
		t.Fatalf("cards asked: %+v", asks)
	}
	p, err := store.DecodeGrantParams(asks[0].params)
	must(t, err)
	g, err := store.WriteLandingGrant(ctx, r.pool, gsrc, store.GrantApproval{ID: "approval_cores_1", Number: 1, Action: asks[0].action,
		ParamsHash: strings.Repeat("a", 64), ApprovedAt: r.now().Add(-time.Second)}, p)
	must(t, err)
	if g.Batch == nil || !slices.Equal(g.Batch.Members, b.Members) || !slices.Equal(g.Waives, []string{"core", "rule_paths"}) {
		t.Fatalf("grant: %+v %v", g.Batch, g.Waives)
	}
	r.clock(time.Second)
	r.pass(t)
	if !slices.Equal(r.pushes, []string{b.R + "<-" + batchM0}) {
		t.Fatal("pushes", r.pushes)
	}
	for i, o := range r.latest(t, roots) {
		if o.Phase != "main_result" || o.Outcome != "ok" {
			t.Fatal("member", i, o.Phase, o.Outcome)
		}
	}
	r.clock(time.Minute)
	_, err = store.ReconcileMain(ctx, r.pool, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
		return store.MainEvidence{Head: b.R, HasR: true, Validity: "effective"}, nil
	})
	must(t, err)
	for i, op := range r.latest(t, roots) {
		if op.Phase != "landed" {
			t.Fatalf("member %d phase = %s, want landed", i, op.Phase)
		}
	}
	r.pass(t)
	must(t, r.ctl.LandingRequestsPass(ctx))
	if len(r.pushes) != 1 || len(asks) != 1 {
		t.Fatal("more after landing", r.pushes, len(asks))
	}
	// One landing CI: the lead's pinned run (made sure of again by name).
	if !slices.Equal(slices.Compact(slices.Clone(r.ensured)), []string{landing.CIName(b.Lead, 1)}) || len(r.runs) != 1 {
		t.Fatal("CI kicked", r.ensured)
	}
}
