package landingci

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func TestInfraLandingAllowParam(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		allow []string
		want  string
	}{
		{"ordered", []string{"z/**", "a/*.md"}, `["z/**","a/*.md"]`},
		{"empty", []string{}, `[]`},
		{"no rule", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _ := drillService(t)
			calls := 0
			c.InfraLandingAllow = func(context.Context) ([]string, error) { calls++; return tc.allow, nil }
			key := infraKey()
			name := landing.InfraCIName(key.Revision, key.Base, key.Siblings["bifrost-platform"], key.Generation)
			if err := c.Ensure(context.Background(), name, key); err != nil {
				t.Fatal(err)
			}
			params, _ := fullParams(t, c, name)
			got, exists := params["landing-allow-json"]
			if got != tc.want || exists != (tc.allow != nil) || calls != 1 {
				t.Fatalf("params=%v calls=%d", params, calls)
			}
			platformName := landing.CIName("allow-platform", 1)
			if err := c.Ensure(context.Background(), platformName, pinnedKey()); err != nil {
				t.Fatal(err)
			}
			platform, _ := fullParams(t, c, platformName)
			if _, exists := platform["landing-allow-json"]; exists || calls != 1 {
				t.Fatalf("platform params=%v calls=%d", platform, calls)
			}
		})
	}
}
