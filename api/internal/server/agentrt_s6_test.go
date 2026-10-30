package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// Serial: grantServer's environment and executor hook. The downstream
// material check is a passing fixture; request decoding is the real gate.
// T14 is separate from hard-path policy: changing only repo refuses before
// material/DB access, and the approval service writes no second row.
func TestS6T14InfraGrantRequestCreatesNoApproval(t *testing.T) {
	srv, do, _, _ := grantServer(t)
	materialReads := 0
	srv.approvals.SetCreateCheck(func(_ context.Context, action string, params map[string]any) error {
		if _, err := srv.landingGrantRequest(action, params); err != nil {
			return err
		}
		materialReads++
		return nil
	})
	p := grantParams()
	p["stg_release"] = false
	requestGrant(t, do, store.ActionGuardrailLanding, p)
	before := len(srv.approvals.Pending())
	p["repo"] = "bifrost-trade-infra"
	r := do(http.MethodPost, "/api/v1/approvals", map[string]any{"action": store.ActionGuardrailLanding, "params": p, "reason": "document scope", "rollback": "revert"}, "operator")
	if r.Code != http.StatusUnprocessableEntity || !strings.Contains(r.Body.String(), "grant_unsupported") {
		t.Fatal(r.Code, r.Body.String())
	}
	if materialReads != 1 || len(srv.approvals.Pending()) != before {
		t.Fatal("infra reached material or wrote an approval")
	}
}

// Serial: process environment. T62 through worker wiring: explicit empty
// wins over LIVE=on, platform-only constructs no infra reader/writer, and
// the S6-5 boundary can never reuse platform CI evidence for infra.
// Serial: process environment controls worker wiring.
func TestS6LiveRepoWorkerWiring(t *testing.T) {
	t.Setenv("AGENTRT_LANDING_LIVE", "on")
	t.Setenv("AGENTRT_LANDER_USERNAME_FILE", "/not-read/username")
	t.Setenv("AGENTRT_LANDER_PASSWORD_FILE", "/not-read/password")
	for _, tc := range []struct {
		repos string
		want  int
	}{{"bifrost-platform", 1}, {"", 0}, {"bifrost-platform,bifrost-trade-infra", 2}} {
		t.Run(tc.repos, func(t *testing.T) {
			t.Setenv("AGENTRT_LANDING_LIVE_REPOS", tc.repos)
			s := &Server{cluster: cluster.NewHandler(&config.Config{}, nil)}
			c := &controller.Controller{Env: "prod", DB: &startup704DB{signed: true}, LandingCards: completeCardQuestions()}
			s.wireLandingDrill(c)
			if len(c.DrillGit) != tc.want || c.Live != (tc.want > 0) {
				t.Fatal("wrong writers", c.DrillGit, c.Live)
			}
			if tc.want < 2 {
				_, err := c.Reconcile.MainEvidence(context.Background(), "bifrost-trade-infra", "task", "", strings.Repeat("a", 40))
				if err == nil || err.Error() != "unsupported or unconfigured landing repo" {
					t.Fatal("infra reader constructed", err)
				}
			} else {
				run, err := c.Drill.CI(context.Background(), "bifrost-trade-infra", strings.Repeat("a", 40), time.Time{})
				if err == nil || run.Status == "Succeeded" {
					t.Fatal("infra accepted foreign CI", run, err)
				}
				if err := c.Drill.EnsureCI(context.Background(), "bifrost-trade-infra", "same-name", strings.Repeat("a", 40)); err == nil {
					t.Fatal("infra reached platform CI creator")
				}
			}
		})
	}
	t.Setenv("AGENTRT_LANDING_LIVE_REPOS", "bifrost-platform")
	s := &Server{cluster: cluster.NewHandler(&config.Config{}, nil)}
	c := &controller.Controller{Env: "prod", DB: &lateDB{}, LandingCards: completeCardQuestions()}
	s.wireLandingDrill(c)
	if !c.Live || c.DrillGit["bifrost-platform"] == nil || c.Reconcile.Discover == nil {
		t.Fatal("failed discovery disabled platform or lost its retry")
	}
}
