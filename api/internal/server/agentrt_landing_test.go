package server

import (
	"time"

	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
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
		noCards          bool
	}{
		{"prod", "", "", true, true, true, false, false},
		{"prod", "", "off", true, true, true, false, false},
		{"prod", "", "on", true, false, true, false, false},
		{"prod", "", "on", true, true, true, true, false},
		// Without the landing cards' reach into the question store, PROD
		// starts no live landing (CUT §11.6 v3 item 9).
		{"prod", "", "on", true, true, true, false, true},
		{"prod", "", "on", false, true, false, false, false},
		{"stg", "", "", true, true, true, false, false},
		{"stg", "on", "", true, true, true, true, false},
		{"stg", "on", "", false, true, false, false, false},
		{"dev", "on", "on", true, true, false, false, false},
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
			if !tc.noCards {
				c.LandingCards = completeCardQuestions()
			}
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

// completeCardQuestions is a card source with every reach present (never
// called by the wiring).
func completeCardQuestions() store.CardQuestions {
	return store.CardQuestions{
		AskRaw: func(context.Context, string, map[string]any) (string, time.Time, error) { return "", time.Time{}, nil },
		LookupKey: func(context.Context, string, map[string]any) (store.Question, time.Time, bool, error) {
			return store.Question{}, time.Time{}, false, nil
		},
		Lookup:   func(context.Context, string) (store.Question, bool, error) { return store.Question{}, false, nil },
		Withdraw: func(context.Context, string, string) (string, error) { return "", nil },
		Ack:      func(context.Context, string) (bool, error) { return false, nil },
	}
}

// S3 (review 670 R-04): PROD's live landing says on /health why it is off,
// and which reach of the landing cards it has.
func TestLandingLiveOffSaysWhy(t *testing.T) {
	s := &Server{}
	c := &controller.Controller{Env: "prod"}
	t.Setenv("AGENTRT_LANDING_LIVE", "")
	if got := s.landingLiveOff(c); got != "AGENTRT_LANDING_LIVE is not on" {
		t.Fatal(got)
	}
	t.Setenv("AGENTRT_LANDING_LIVE", "on")
	if got := s.landingLiveOff(c); got != "the landing cards cannot reach the question store" {
		t.Fatal(got)
	}
	c.LandingCards = completeCardQuestions()
	if got := s.landingLiveOff(c); got != "delivery unavailable" {
		t.Fatal(got)
	}
}
