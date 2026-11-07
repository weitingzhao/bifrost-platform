package agentd_test

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// TestConfigDefaults: without any AGENTD_* setting each keeps the default the
// code had before it was a setting.
func TestConfigDefaults(t *testing.T) {
	c, err := agentd.ConfigFromEnv(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{
		agentd.EnvSetupLimit: 10 * time.Minute, agentd.EnvTestDBLimit: 2 * time.Minute, agentd.EnvDeliverMaxFails: 3,
		agentd.EnvPruneEvery: 10 * time.Minute, agentd.EnvPruneWork: 2 * time.Minute, agentd.EnvPruneReport: 30 * time.Second,
		agentd.EnvCollectEvery: 2 * time.Second, agentd.EnvSpecLimit: 15 * time.Minute, agentd.EnvBenchLimit: 30 * time.Minute,
		agentd.EnvBenchCheckLimit: 15 * time.Minute,
	}
	if len(c) != len(want) || len(agentd.Settings) != len(want) {
		t.Fatalf("settings %v, want %v", c, want)
	}
	for k, v := range want {
		if c.Duration(k) != v {
			t.Errorf("%s = %v, want %v", k, c.Duration(k), v)
		}
	}
	if c.Count(agentd.EnvDeliverMaxFails) != 3 {
		t.Errorf("max fails %d", c.Count(agentd.EnvDeliverMaxFails))
	}
	// The package's own values are the defaults until Apply.
	if agentd.SetupLimit != c.Duration(agentd.EnvSetupLimit) || agentd.PruneEvery != c.Duration(agentd.EnvPruneEvery) ||
		agentd.SpecLimit != c.Duration(agentd.EnvSpecLimit) {
		t.Errorf("package defaults %v %v %v differ from the settings'", agentd.SetupLimit, agentd.PruneEvery, agentd.SpecLimit)
	}
	for _, s := range agentd.Settings {
		if !strings.HasPrefix(s.Env, "AGENTD_") || s.Default < s.Min || s.Default > s.Max {
			t.Errorf("%+v: an AGENTD_ name, its default inside its bounds", s)
		}
	}
}

// TestEnvSettings: a setting given is read, an empty one is unset, and one
// that does not read or is out of bounds is an error naming it.
func TestEnvSettings(t *testing.T) {
	c, err := agentd.ConfigFromEnv(envOf(map[string]string{
		agentd.EnvSetupLimit: "20m", agentd.EnvDeliverMaxFails: "5", agentd.EnvPruneReport: "", agentd.EnvTestDBLimit: "90s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Duration(agentd.EnvSetupLimit) != 20*time.Minute || c.Count(agentd.EnvDeliverMaxFails) != 5 ||
		c.Duration(agentd.EnvPruneReport) != 30*time.Second || c.Duration(agentd.EnvTestDBLimit) != 90*time.Second {
		t.Fatalf("read %v", c)
	}
	for env, raw := range map[string]string{
		agentd.EnvSetupLimit:      "ten minutes",
		agentd.EnvDeliverMaxFails: "3.5",
		agentd.EnvCollectEvery:    "1ms",
		agentd.EnvPruneEvery:      "48h",
		agentd.EnvBenchCheckLimit: "-1m",
	} {
		if _, err := agentd.ConfigFromEnv(envOf(map[string]string{env: raw})); err == nil || !strings.HasPrefix(err.Error(), env+":") {
			t.Errorf("%s=%q: %v", env, raw, err)
		}
	}
	if _, err := agentd.ConfigFromEnv(envOf(map[string]string{agentd.EnvDeliverMaxFails: "0"})); err == nil {
		t.Error("no heartbeat failure allowed at all was taken")
	}
}
