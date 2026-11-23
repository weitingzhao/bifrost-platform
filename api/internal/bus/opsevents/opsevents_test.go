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
	for _, e := range []Event{EnvHealth("prod/redis", "fail", "checklist-prober", "dial\n tcp  refused"), PluginStatus("market-data", "ok", "checklist-prober", ""), PluginStatus("ib-gateway", "fail", "ib-auto-repair", "reconnect failed"), EnvHealth("prod/redis", "fail", "patrol-autopilot", "restart: HTTP 500")} {
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

// Origin: 2026-11-23, UI/Signals asked to read each plugin's latest state from
// GET /api/v1/bus/states; plugin.status.changed and env.health.changed are now
// state types. Their body is declared here, not in the register, so this test
// holds the register's subject_from and change_from to fields the body always
// carries, and a real event's body to the state the readout will show.
func TestTheStateTypesNameFieldsOfTheirBody(t *testing.T) {
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{TypePlugin, TypeEnv} {
		c := all["rocket"].Channels[typ]
		if c.SubjectFrom == "" || len(c.ChangeFrom) == 0 {
			t.Fatalf("%s: not a state type in the register", typ)
		}
		fields, ok := eventlog.BodyOf(typ)
		if !ok {
			t.Fatalf("%s: no body declared", typ)
		}
		required := map[string]bool{}
		for _, f := range fields {
			required[f.Name] = !f.Optional
		}
		for _, n := range append([]string{c.SubjectFrom, "subject", "change"}, c.ChangeFrom...) {
			if !required[n] {
				t.Errorf("%s: %q is not a required field of the declared body", typ, n)
			}
		}
	}
	b := Body(PluginStatus("market-data", "ok", "checklist", ""))
	if b["subject"] != "market-data" || b["change"] != "ok" {
		t.Fatalf("plugin body %v", b)
	}
}
