package actions

import (
	"context"
	"slices"
	"testing"
)

// TD-460: the two B5 sample actions are Tier C, executed by the platform
// after the Owner's approval on the PROD Console, and have no direct route.
func TestSTGSampleActionsAreApprovedTierC(t *testing.T) {
	t.Parallel()
	for id, required := range map[string][]string{
		"stg_release_sample_create":   {"env", "revision", "ui_revision", "infra_revision"},
		"stg_release_sample_finalize": {"env", "run", "uid"},
	} {
		a, ok := ByID(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if a.Tier != TierC || a.TierOf(context.Background(), map[string]any{"env": "stg"}) != TierC || !a.Tier.NeedsApproval() {
			t.Fatalf("%s tier %s", id, a.Tier)
		}
		if a.Method != "" || a.Pattern != "" || a.Passthrough || a.View().Runner != RunnerPlatform {
			t.Fatalf("%s must have no direct route and run on the platform: %+v", id, a)
		}
		var names []string
		for _, p := range a.Params {
			names = append(names, p.Name)
			if !p.Required || p.In != "body" {
				t.Fatalf("%s param %+v", id, p)
			}
		}
		if !slices.Equal(names, required) {
			t.Fatalf("%s params %v", id, names)
		}
		if missing := a.Missing(map[string]any{"env": "stg"}); missing == "" {
			t.Fatalf("%s accepted a request without its commits or run", id)
		}
		if d := a.Describe(context.Background(), map[string]any{"env": "stg", "revision": "0123456789abcdef0123456789abcdef01234567", "run": "stgrel-9-1-01234567"}, ""); d.Env != "stg" || d.Summary == "" {
			t.Fatalf("%s card %+v", id, d)
		}
	}
}
