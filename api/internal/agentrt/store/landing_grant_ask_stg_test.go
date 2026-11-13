package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The card the platform asks for
// itself (R5:5.7) carries stg_release true: the create check accepts it as
// it does the coordinator's, the grant written from its approval has
// STGRelease true, and the final gate pushes on it. Both actions.
func TestGrantAskSTGControllerCardWritesSTGGrant(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	for _, tc := range []struct {
		name, path, action string
		pinned             bool
	}{
		{"guardrail", c5Soft, store.ActionGuardrailLanding, true},
		{"core", c5Core, store.ActionCoreLanding, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := c5Wait(t, tc.path, tc.pinned, true)
			type ask struct {
				action string
				params map[string]any
			}
			var asks []ask
			ctl := controller.Controller{DB: c.w.rt, Env: "prod", Reconcile: &controller.LandingReconcile{
				Requests: func(context.Context) ([]store.LandingGrantRequest, error) { return nil, nil },
				Grants:   func() store.GrantSources { return c.gsrc },
				RequestGrant: func(_ context.Context, action string, params map[string]any, _ string) (string, error) {
					asks = append(asks, ask{action, params})
					return "appr_ask_stg", nil
				}}}
			ok(t, ctl.LandingRequestsPass(ctx))
			if len(asks) != 1 || asks[0].action != tc.action {
				t.Fatalf("asks %+v, want one %s", asks, tc.action)
			}
			p, err := store.DecodeGrantParams(asks[0].params)
			ok(t, err)
			if !p.STGRelease {
				t.Fatalf("params %+v: stg_release false", p)
			}
			if _, err := store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, tc.action, p); err != nil {
				t.Fatal("create check refused the controller's card:", err)
			}
			g, err := store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(tc.action, time.Minute), p)
			ok(t, err)
			if !g.STGRelease || g.Op != c.op || g.MaterialDigest != p.MaterialDigest || g.Action != tc.action {
				t.Fatalf("grant %+v", g)
			}
			if _, d := c.gate(t); !d.Allowed {
				t.Fatal("final gate", d)
			}
		})
	}
}
