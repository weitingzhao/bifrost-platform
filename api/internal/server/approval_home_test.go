package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestHumanApprovalHomeOnRouter(t *testing.T) {
	for _, env := range []string{"stg", "dev", "", "unknown", "prod"} {
		t.Run(env, func(t *testing.T) {
			cfg := newTestConfig(t)
			t.Setenv("OPS_VIEWER_ENV", env)
			t.Setenv("AGENTRT_ENV", env)
			t.Setenv("AGENTRT_DSN", "")
			authPath := filepath.Join(t.TempDir(), "auth.yaml")
			if err := os.WriteFile(authPath, []byte("tokens:\n  - name: operator\n    role: operator\n    token: test-operator\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg.PlatformAuthPath = authPath
			srv := newTestServer(t, cfg)
			h := srv.Router()
			for _, drill := range []bool{false, true} {
				raw, _ := json.Marshal(map[string]any{"action": "cordon_node", "params": map[string]any{"name": "node-a"}, "reason": "home test", "drill": drill})
				req := httptest.NewRequest(http.MethodPost, "http://ops.bifrost.lan/api/v1/approvals", strings.NewReader(string(raw)))
				req.Header.Set("Authorization", "Bearer test-operator")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if env == "prod" {
					if rec.Code != 201 {
						t.Fatalf("PROD: %d %s", rec.Code, rec.Body.String())
					}
				} else if rec.Code != 403 || !strings.Contains(rec.Body.String(), "ops.bifrost.lan") {
					t.Fatalf("non-PROD: %d %s", rec.Code, rec.Body.String())
				}
			}
			if env != "prod" && len(srv.approvals.Pending()) != 0 {
				t.Fatal("refused request stored")
			}
		})
	}
}

func TestSTGRuleCardKeepsWorkingWithoutOwnerRequests(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "stg")
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	rt.takeCardsByRule(true)
	card := store.Card{Key: strings.Repeat("cd", 16), Title: "Blocked in M-3", Body: "test", Options: []string{store.BlockedRetry, store.BlockedDrop}, Recommended: 2, RecommendedWhy: "test", Meanwhile: "wait", OwnerDuty: "goal", ByRule: true}
	ctx := context.Background()
	id, err := rt.agentRuntimeAsk()(ctx, card)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.agentRuntimeRuleAnswer()(ctx, id, "b", "rule card-auto-answer v1", time.Now().Add(time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	q, ok := rt.agentRuntimeQuestion()(ctx, id)
	if !ok || q.Status != store.QuestionAnswered || q.Channel != "rule" {
		t.Fatalf("rule result: %+v", q)
	}
	card.Key, card.ByRule = strings.Repeat("ef", 16), false
	card.Title = "An Owner decision without a rule"
	if _, err := rt.agentRuntimeAsk()(ctx, card); err == nil || !strings.Contains(err.Error(), "ops.bifrost.lan") {
		t.Fatalf("human card: %v", err)
	}
}
