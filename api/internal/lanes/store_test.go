package lanes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCatalogFromRepoConfig(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// api/internal/lanes → repo root config/
	configDir := filepath.Clean(filepath.Join(wd, "..", "..", "..", "config"))
	store := NewStore(configDir)
	list, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Lanes) < 26 {
		t.Fatalf("expected >=26 lanes, got %d", len(list.Lanes))
	}
	found := false
	for _, l := range list.Lanes {
		if l.ID == "console-api" {
			found = true
			if l.ComponentLine != "rocket" || l.AgentMode != "Ops" {
				t.Fatalf("console-api fields wrong: %+v", l)
			}
		}
	}
	if !found {
		t.Fatal("console-api missing")
	}
}

func TestCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	seed := `version: "1"
lanes:
  - id: seed-lane
    track: build
    component_line: rocket
    track_type: build
    label: Seed
    short_label: Seed
    description: Seed lane for test.
    agent_mode: Ops
    work_intent: feature
`
	path := filepath.Join(dir, "lanes.yaml")
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)

	_, err := store.Create(Lane{
		ID: "dup", Track: "build", ComponentLine: "rocket", TrackType: "build",
		Label: "L", ShortLabel: "L", Description: "d", AgentMode: "Ops", WorkIntent: "feature",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, ok, err := store.Get("dup")
	if err != nil || !ok || got.ID != "dup" {
		t.Fatalf("get: ok=%v err=%v got=%+v", ok, err, got)
	}

	_, err = store.Create(Lane{
		ID: "dup", Track: "build", ComponentLine: "rocket", TrackType: "build",
		Label: "L", ShortLabel: "L", Description: "d", AgentMode: "Ops", WorkIntent: "feature",
	})
	if err == nil || !IsValidation(err) {
		t.Fatalf("expected conflict validation, got %v", err)
	}

	_, err = store.Create(Lane{ID: "Bad_ID", Track: "build", ComponentLine: "rocket", TrackType: "build",
		Label: "L", ShortLabel: "L", Description: "d", AgentMode: "Ops", WorkIntent: "feature"})
	if err == nil || !IsValidation(err) {
		t.Fatalf("expected id validation, got %v", err)
	}
}
