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

// Origin: 2026-11-18, the list of problem channels: a probe that turned red
// was seen only by whoever opened the checklist page.
func TestStateEventsCarrySourceSeverityAndCause(t *testing.T) {
	for _, e := range []Event{EnvHealth("prod/redis", "fail", "checklist-prober", "dial\n tcp  refused"), PluginStatus("market-data", "ok", "checklist-prober", "")} {
		if err := eventlog.CheckBody(e.Type, Body(e)); err != nil {
			t.Errorf("%+v: %v", e, err)
		}
		if err := registry.Agree(e.Type); err != nil {
			t.Errorf("%s: %v", e.Type, err)
		}
	}
	b := Body(EnvHealth("prod/redis", "fail", "checklist-prober", "dial\n tcp  refused"))
	if b["severity"] != "critical" || b["cause"] != "dial tcp refused" || b["ref"] != "/api/v1/checklist/signals" {
		t.Errorf("body %+v", b)
	}
	b["severity"] = "info"
	if eventlog.CheckBody(TypeEnv, b) == nil {
		t.Error("a failure called info was taken")
	}
	if Valid(EnvHealth("prod/redis", "unknown", "checklist-prober", "")) {
		t.Error("unknown is not a state to announce")
	}
	if Valid(EnvHealth("redis", "fail", "checklist-prober", "")) || Valid(PluginStatus("market-data", "fail", "", "")) {
		t.Error("an event without its environment or source was taken")
	}
}
