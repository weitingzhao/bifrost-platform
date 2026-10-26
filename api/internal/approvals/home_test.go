package approvals

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

func TestHumanApprovalHome(t *testing.T) {
	for _, env := range []string{"stg", "dev", "dev-local", "", "unknown", "prod"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("OPS_VIEWER_ENV", env)
			svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
			for _, in := range []createInput{
				{Action: "cordon_node", Params: map[string]any{"name": "n1"}},
				{Action: "owner_run_command", Params: map[string]any{"command": "true", "reason": "test", "rollback": "none"}},
				{Action: "cordon_node", Params: map[string]any{"name": "n1"}, Drill: true},
			} {
				in.Requester, in.Reason = "test", "test home"
				got := svc.createWith(context.Background(), in)
				if env == "prod" {
					if got.Status != http.StatusCreated {
						t.Fatalf("PROD: %d %v", got.Status, got.Body)
					}
				} else if got.Status != http.StatusForbidden || !strings.Contains(got.Body["error"].(string), "ops.bifrost.lan") {
					t.Fatalf("%s: %d %v", in.Action, got.Status, got.Body)
				}
			}
			if env != "prod" && len(svc.Pending()) != 0 {
				t.Fatal("refused requests were stored")
			}
			// Neither tier B nor a signed-policy decision needs a new human decision.
			b := svc.create(context.Background(), "test", "wake_compute_node", "test", "", map[string]any{"name": "n1"})
			if b.Status != 400 || b.Body["error"] != "call directly" {
				t.Fatalf("B: %+v", b)
			}
			svc.SetAutoApprover(func(context.Context, string, actions.Tier, map[string]any) string { return "signed-policy" })
			c := svc.create(context.Background(), "test", "cordon_node", "test", "", map[string]any{"name": "n1"})
			if c.Status != 400 || c.Body["auto_approved_by"] != "signed-policy" {
				t.Fatalf("policy: %+v", c)
			}
		})
	}
}
