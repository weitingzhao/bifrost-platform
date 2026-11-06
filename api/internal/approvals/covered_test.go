package approvals

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

func TestCoveredOwnerCommandCreatesNoApproval(t *testing.T) {
	actions.SetActuationPolicy(&actuationpolicy.Policy{Apply: actuationpolicy.Apply{Repos: []actuationpolicy.Repo{{Name: "infra", Prefixes: []string{"k8s/cicd/tekton"}}}}})
	t.Cleanup(func() { actions.SetActuationPolicy(nil) })
	path := filepath.Join(t.TempDir(), "approvals")
	svc := New(path, nil)
	out := svc.create(context.Background(), "session", "owner_run_command", "install definitions", "", map[string]any{"command": "kubectl apply -f k8s/cicd/tekton/x.yaml", "reason": "install definitions"})
	want := "this command applies infra/k8s/cicd/tekton/x.yaml, which plan_manifest then apply_manifest cover: request plan_manifest with repo, path and commit instead"
	if out.Status != http.StatusBadRequest || out.Body["error"] != want {
		t.Fatalf("create = %d %#v", out.Status, out.Body)
	}
	if len(svc.Pending()) != 0 {
		t.Fatal("refused command created an approval")
	}
	if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), "owner_run_command") {
		t.Fatalf("approval persisted: %s", raw)
	}
}
