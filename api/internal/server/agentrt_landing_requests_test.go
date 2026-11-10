package server

import "testing"

func TestObserveLandingRequest(t *testing.T) {
	for _, repo := range []string{"bifrost-platform", "bifrost-trade-infra"} {
		for _, action := range []string{"approve_floor_landing", "approve_guardrail_landing", "approve_core_landing"} {
			want := repo == "bifrost-trade-infra" && action == "approve_floor_landing" ||
				repo == "bifrost-platform" && action != "approve_floor_landing"
			if got := observeLandingRequest(action, repo, "prod", "D", false); got != want {
				t.Errorf("%s %s: %v", repo, action, got)
			}
			for _, tc := range []struct {
				env, tier string
				drill     bool
			}{
				{"stg", "D", false}, {"dev", "D", false}, {"prod", "C", false}, {"prod", "D", true},
			} {
				if observeLandingRequest(action, repo, tc.env, tc.tier, tc.drill) {
					t.Errorf("kept excluded approval: %s %s %+v", repo, action, tc)
				}
			}
		}
	}
}
