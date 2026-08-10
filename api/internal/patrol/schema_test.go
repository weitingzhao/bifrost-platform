package patrol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSeedSkills(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "config", "patrol-skills")
	skills, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir seeds: %v", err)
	}
	if len(skills) != 4 {
		t.Fatalf("seed count = %d, want 4", len(skills))
	}
	byID := map[string]PatrolSkill{}
	for _, s := range skills {
		byID[s.ID] = s
	}
	fleet := byID["fleet-drift-scan"]
	if fleet.TrustLevel != TrustL1 || fleet.Schedule != "5 * * * *" {
		t.Fatalf("fleet-drift-scan = %+v", fleet)
	}
	cert := byID["cert-expiry-check"]
	if cert.TrustLevel != TrustL0 || cert.Schedule != "0 6 * * 1" {
		t.Fatalf("cert-expiry-check = %+v", cert)
	}
	stale := byID["stale-pod-cleanup"]
	if stale.TrustLevel != TrustL1 || stale.Schedule != "0 4 * * *" {
		t.Fatalf("stale-pod-cleanup = %+v", stale)
	}
	foundDelete := false
	for _, tool := range stale.MCPTools {
		if tool == "delete_pod" {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Fatal("stale-pod-cleanup should include delete_pod")
	}
	autopilot := byID["ops-autopilot"]
	if autopilot.TrustLevel != TrustL1 || autopilot.Schedule != "*/15 * * * *" {
		t.Fatalf("ops-autopilot = %+v", autopilot)
	}
	if autopilot.CronActuation != CronActuationConfirm {
		t.Fatalf("ops-autopilot cron_actuation = %s, want confirm", autopilot.CronActuation)
	}
	if autopilot.Scope != "ops-checklist" {
		t.Fatalf("ops-autopilot scope = %s, want ops-checklist", autopilot.Scope)
	}
}

func TestLoadDirMissingIsEmpty(t *testing.T) {
	skills, err := LoadDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Fatalf("got %d", len(skills))
	}
}

func TestLoadRejectsL0WriteTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(`
id: bad-l0-write
name: Bad
description: should fail load
schedule: "0 3 * * *"
trust_level: L0
scope: test
timeout: 30
mcp_tools:
  - delete_pod
prompt_template: nope
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected L0 write-tool load error")
	}
}

func TestLoadRejectsUnknownTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(`
id: unknown-tool-skill
name: Unknown
description: fake tool
schedule: "0 * * * *"
trust_level: L0
scope: test
timeout: 30
mcp_tools:
  - not_a_real_mcp_tool
prompt_template: nope
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected unknown tool error")
	}
}
