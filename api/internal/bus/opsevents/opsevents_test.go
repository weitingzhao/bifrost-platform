package opsevents

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Origin: 2026-11-18, step 3's remaining publishers: these topics were
// registered and nothing wrote them, so their readers kept polling.
func TestTheEventsKeepTheirContract(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, e := range []Event{Loop("platform/prod/patrol-autopilot", false), Loop("platform/prod/patrol-autopilot", true),
		Mirror("bifrost-platform", sha, true), Mirror("bifrost-platform", sha, false), Job("clone-1", true), Job("clone-1", false)} {
		if err := eventlog.CheckBody(e.Type, Body(e)); err != nil {
			t.Errorf("%+v: %v", e, err)
		}
		if err := registry.Agree(e.Type); err != nil {
			t.Errorf("%s: %v", e.Type, err)
		}
	}
	if Valid(Mirror("bifrost-platform", "main", true)) {
		t.Error("a mirror event without a commit was taken")
	}
	b := Body(Job("clone-1", true))
	b["change"] = "healthy"
	if eventlog.CheckBody(TypeJob, b) == nil {
		t.Error("a job event with a loop's change was taken")
	}
}
