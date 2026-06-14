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
	if f.Meta.CatalogVersion != "2026-06-15" {
		t.Errorf("catalog_version = %q", f.Meta.CatalogVersion)
	}
	if f.NorthStar == nil || f.NorthStar.ID != "ops-ui-single-pane" {
		t.Errorf("north_star = %+v", f.NorthStar)
	}
	if f.Deployment.ActiveTrack != "k3s_phase1" {
		t.Errorf("active_track = %q", f.Deployment.ActiveTrack)
	}
	if len(f.Decisions) < 6 {
		t.Errorf("expected >= 6 decisions, got %d", len(f.Decisions))
	}
	foundD1 := false
	for _, m := range f.Milestones {
		if m.ID == "k3s-phase1" && m.Status != "IN_PROGRESS" {
			t.Errorf("k3s-phase1 status = %q", m.Status)
		}
		if m.ID == "2c-b-prod-cutover" {
			foundD1 = m.Blocker == "decision:D1"
		}
	}
	if !foundD1 {
		t.Error("2c-b-prod-cutover should block on decision:D1")
	}
}

func TestValidateRequired(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
