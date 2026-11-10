package agentrtapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestMissionLandingObservesEachRepositoryMain(t *testing.T) {
	t.Parallel()
	m0 := strings.Repeat("a", 40)
	moved := strings.Repeat("b", 40)
	for _, mode := range []string{"matching infra", "moved infra", "nil reader", "failed reader"} {
		t.Run(mode, func(t *testing.T) {
			st := store.MissionLandingStatus{InScope: true}
			for _, repo := range []string{"bifrost-platform", "bifrost-trade-infra"} {
				root := store.LandingRootStatus{Waiting: &store.LandingWaitStatus{Repo: repo, Phase: "awaiting_grant", M0: m0, ApprovalStatus: "pending"}}
				root.Repo = repo
				root.Root = repo + "-root"
				st.Roots = append(st.Roots, root)
			}
			calls := map[string]int{}
			a := &API{}
			if mode != "nil reader" {
				a.MainHead = func(_ context.Context, repo string) (string, error) {
					calls[repo]++
					if mode == "failed reader" {
						return m0, errors.New("unavailable")
					}
					if repo == "bifrost-trade-infra" && mode == "matching infra" {
						return m0, nil
					}
					return moved, nil
				}
			}
			w := httptest.NewRecorder()
			a.writeMissionLanding(w, httptest.NewRequest("GET", "/agentrt/landing/missions/test", nil), "test", st)
			var out struct {
				Roots []struct {
					Repo string                  `json:"repo"`
					Wait store.LandingWaitStatus `json:"authorization_wait"`
				} `json:"roots"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Roots) != 2 {
				t.Fatalf("roots: %s", w.Body.String())
			}
			for _, root := range out.Roots {
				want := store.LandingWaitStatus{Phase: "awaiting_grant", M0: m0, ApprovalStatus: "pending"}
				head := moved
				if root.Repo == "bifrost-trade-infra" && mode == "matching infra" {
					head = m0
				}
				if mode == "nil reader" || mode == "failed reader" {
					head = ""
				}
				want.ObserveMain(head)
				if !reflect.DeepEqual(root.Wait, want) {
					t.Fatalf("%s: got %+v, want %+v", root.Repo, root.Wait, want)
				}
			}
			wantCalls := map[string]int{}
			if mode != "nil reader" {
				wantCalls = map[string]int{"bifrost-platform": 1, "bifrost-trade-infra": 1}
			}
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf("main reads: %v, want %v", calls, wantCalls)
			}
		})
	}
}
