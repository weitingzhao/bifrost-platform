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

func TestDeleteLane(t *testing.T) {
	dir := t.TempDir()
	seed := `version: "1"
lanes:
  - id: keep-me
    track: build
    component_line: rocket
    track_type: build
    label: Keep
    short_label: Keep
    description: Stays.
    agent_mode: Ops
    work_intent: feature
  - id: drop-me
    track: build
    component_line: rocket
    track_type: build
    label: Drop
    short_label: Drop
    description: Remove me.
    agent_mode: Ops
    work_intent: feature
`
	path := filepath.Join(dir, "lanes.yaml")
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)

	if err := store.Delete("drop-me"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := store.Get("drop-me"); err != nil || ok {
		t.Fatalf("drop-me should be gone: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.Get("keep-me"); err != nil || !ok {
		t.Fatalf("keep-me should remain: ok=%v err=%v", ok, err)
	}

	err := store.Delete("drop-me")
	if err == nil || !IsValidation(err) {
		t.Fatalf("expected not found validation, got %v", err)
	}
}

func TestUpdateLane(t *testing.T) {
	dir := t.TempDir()
	seed := `version: "1"
lanes:
  - id: move-me
    track: build
    component_line: rocket
    track_type: build
    label: Move Me
    short_label: Move Me
    description: Original description.
    agent_mode: Ops
    work_intent: feature
`
	path := filepath.Join(dir, "lanes.yaml")
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)

	updated, err := store.Update("move-me", UpdateRequest{
		ComponentLine: "satellite",
		TrackType:     "migrate",
		Track:         "migrate",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ComponentLine != "satellite" || updated.TrackType != "migrate" || updated.Track != "migrate" {
		t.Fatalf("unexpected update result: %+v", updated)
	}
	if updated.Label != "Move Me" || updated.ID != "move-me" {
		t.Fatalf("id/label must stay immutable: %+v", updated)
	}

	got, ok, err := store.Get("move-me")
	if err != nil || !ok {
		t.Fatalf("get after update: ok=%v err=%v", ok, err)
	}
	if got.ComponentLine != "satellite" {
		t.Fatalf("persisted line wrong: %+v", got)
	}

	_, err = store.Update("missing-lane", UpdateRequest{ComponentLine: "rocket"})
	if err == nil || !IsValidation(err) {
		t.Fatalf("expected not-found validation, got %v", err)
	}
}
