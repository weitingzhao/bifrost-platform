package server

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"strings"
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

// Review 663 B-02: main is read in STG and PROD whatever the switches say;
// only the switch (with an identity and delivery) adds a writing loop. With
// no identity the reader fails visibly instead of being absent.
func TestLandingMainReaderIsWiredApartFromTheSwitch(t *testing.T) {
	for _, tc := range []struct {
		env, drill, live string
		creds, delivery  bool
		reads, writes    bool
	}{
		{"prod", "", "", true, true, true, false},
		{"prod", "", "off", true, true, true, false},
		{"prod", "", "on", true, false, true, false},
		{"prod", "", "on", true, true, true, true},
		{"prod", "", "on", false, true, false, false},
		{"stg", "", "", true, true, true, false},
		{"stg", "on", "", true, true, true, true},
		{"stg", "on", "", false, true, false, false},
		{"dev", "on", "on", true, true, false, false},
	} {
		t.Run(tc.env+"/"+tc.drill+tc.live, func(t *testing.T) {
			t.Setenv("AGENTRT_LANDING_DRILL", tc.drill)
			t.Setenv("AGENTRT_LANDING_LIVE", tc.live)
			u, p := "/not-read/username", "/not-read/password"
			if !tc.creds {
				u, p = "relative", "relative"
			}
			for _, k := range []string{"AGENTRT_LANDING_USERNAME_FILE", "AGENTRT_LANDER_USERNAME_FILE"} {
				t.Setenv(k, u)
			}
			for _, k := range []string{"AGENTRT_LANDING_PASSWORD_FILE", "AGENTRT_LANDER_PASSWORD_FILE"} {
				t.Setenv(k, p)
			}
			s := &Server{}
			if tc.delivery {
				s.delivery = delivery.NewHandlerForTest(delivery.NewService(&config.ClusterEntry{ID: "test"}), nil)
			}
			c := &controller.Controller{Env: tc.env}
			s.wireLandingDrill(c)
			if c.Reconcile == nil || (c.Reconcile.MainEvidence != nil) != (tc.env != "dev") {
				t.Fatal("main reader wiring", tc)
			}
			if tc.env != "dev" && !tc.creds {
				if _, err := c.Reconcile.MainEvidence(context.Background(), "bifrost-platform", "t", "", strings.Repeat("a", 40)); err == nil {
					t.Fatal("a reader with no identity did not fail")
				}
			}
			writes := c.Drill != nil && c.DrillGit != nil
			if writes != tc.writes || c.Live != (tc.writes && tc.env == "prod") {
				t.Fatal("writing loop wiring", tc, writes, c.Live)
			}
			if tc.env == "prod" && c.Drill != nil && c.Drill.Delete != nil {
				t.Fatal("PROD wired a branch delete")
			}
		})
	}
}
