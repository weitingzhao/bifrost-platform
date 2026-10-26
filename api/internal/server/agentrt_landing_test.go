package server

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"testing"
)

func TestLandingDrillWorkerOptIn(t *testing.T) {
	for _, tc := range []struct {
		env, flag string
		enabled   bool
	}{{"stg", "", false}, {"stg", "off", false}, {"prod", "on", false}, {"dev", "on", false}, {"stg", "true", false}, {"stg", "on", true}} {
		t.Run(tc.env+tc.flag, func(t *testing.T) {
			t.Setenv("AGENTRT_LANDING_DRILL", tc.flag)
			// Files need not exist: constructing adapters must perform no I/O.
			t.Setenv("AGENTRT_LANDING_USERNAME_FILE", "/not-read/username")
			t.Setenv("AGENTRT_LANDING_PASSWORD_FILE", "/not-read/password")
			s := &Server{delivery: delivery.NewHandlerForTest(delivery.NewService(&config.ClusterEntry{ID: "test"}), nil)}
			c := &controller.Controller{Env: tc.env}
			s.wireLandingDrill(c)
			if (c.Drill != nil) != tc.enabled || (c.DrillGit != nil) != tc.enabled {
				t.Fatal("unsafe opt-in", tc)
			}
		})
	}
}
