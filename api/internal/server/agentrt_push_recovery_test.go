package server

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"net/http"
	"strings"
	"testing"
)

func TestPushRecoveryRequiresConsoleApproval(t *testing.T) {
	_, do, _, _ := grantServer(t)
	called := 0
	saved := recoverBatchPush
	t.Cleanup(func() { recoverBatchPush = saved })
	recoverBatchPush = func(_ context.Context, _ store.DB, _ store.GrantSources, _ store.PushRecoveryParams, id string) error {
		if id == "" {
			t.Fatal("execution lacks approval")
		}
		called++
		return nil
	}
	p := map[string]any{"env": "prod", "repo": "bifrost-platform", "op": "land:t_01JA0000000000000000000000:1", "R": strings.Repeat("b", 40), "M0": strings.Repeat("a", 40), "grant_id": "lg_original", "ci_run": "original-green"}
	if _, err := actions.Execute(actions.WithCreateAttempt(context.Background(), "appr_forged", 1), store.RecoverBatchPushAction, p); err == nil {
		t.Fatal("forged approval accepted")
	}
	row := requestGrant(t, do, store.RecoverBatchPushAction, p)
	if approvalOpenKey(store.RecoverBatchPushAction, p) != "landing-push-recovery:"+p["op"].(string) {
		t.Fatal("missing recovery open-request key")
	}
	if code, _ := decide(do, row, "console", false); code != http.StatusBadRequest {
		t.Fatal("accepted missing confirmation", code)
	}
	if code, _ := decide(do, row, "chat", true); code != http.StatusForbidden {
		t.Fatal("accepted chat", code)
	}
	if called != 0 {
		t.Fatal("executed before Console approval")
	}
	if code, out := decide(do, row, "console", true); code != http.StatusOK || out.Status != "executed" {
		t.Fatalf("execution failed: %d %+v", code, out)
	}
	if called != 1 {
		t.Fatal("wrong execution count", called)
	}
}
