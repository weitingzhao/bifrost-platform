package opsevents

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; before the bus's database connects, Announce errs (the prober keeps
// the flip) and Say does not panic; a probed thing is said as its own topic.
func TestSpeakerBeforeTheDatabaseConnects(t *testing.T) {
	s := Speaker{DB: func() eventlog.DB { return nil }}
	if s.Announce(context.Background(), Job("clone-1", true)) == nil {
		t.Fatal("announced with no database")
	}
	s.Say(context.Background(), Job("clone-1", true))
	if e := Probed(true, "market-data", "ok", "why"); e.Type != TypePlugin || e.Source != "checklist-prober" {
		t.Fatalf("%+v", e)
	}
	if e := Probed(false, "prod/api", "ok", "why"); e.Type != TypeEnv || e.Source != "checklist-prober" {
		t.Fatalf("%+v", e)
	}
}
