package opscontext

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "ops-context.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("config/ops-context.yaml not found from test cwd")
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Meta.CatalogVersion == "" {
		t.Error("catalog_version should not be empty")
	}
	if f.NorthStar == nil || f.NorthStar.ID != "ops-ui-single-pane" {
		t.Errorf("north_star = %+v", f.NorthStar)
	}
	if f.Deployment.ActiveTrack != "ops_ui_actuation" {
		t.Errorf("active_track = %q", f.Deployment.ActiveTrack)
	}
	if len(f.Decisions) < 6 {
		t.Errorf("expected >= 6 decisions, got %d", len(f.Decisions))
	}
	foundCutover := false
	foundK3sMacAgents := false
	for _, m := range f.Milestones {
		if m.ID == "k3s-phase1" && m.Status != "CLOSED" {
			t.Errorf("k3s-phase1 status = %q", m.Status)
		}
		if m.ID == "k3s-mac-agents" {
			foundK3sMacAgents = true
			if m.Status != "CLOSED" {
				t.Errorf("k3s-mac-agents status = %q", m.Status)
			}
		}
		if m.ID == "2c-b-prod-cutover" {
			foundCutover = true
		}
	}
	if !foundK3sMacAgents {
		t.Error("k3s-mac-agents milestone missing")
	}
	if !foundCutover {
		t.Error("2c-b-prod-cutover milestone missing")
	}
}

func TestValidateRequired(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestUpdateLastGate(t *testing.T) {
	src := filepath.Join("..", "..", "..", "config", "ops-context.yaml")
	if _, err := os.Stat(src); err != nil {
		t.Skip("config/ops-context.yaml not found")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "ops-context.yaml")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateLastGate(tmp, "2026-06-25T12:00:00Z", "pass"); err != nil {
		t.Fatal(err)
	}

	f, err := Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if f.Promotion.LastGate.At == nil || *f.Promotion.LastGate.At != "2026-06-25T12:00:00Z" {
		t.Errorf("expected at=2026-06-25T12:00:00Z, got %v", f.Promotion.LastGate.At)
	}
	if f.Promotion.LastGate.Result == nil || *f.Promotion.LastGate.Result != "pass" {
		t.Errorf("expected result=pass, got %v", f.Promotion.LastGate.Result)
	}
}
