package patrol

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestThrottleAllowsFirstRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	th, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if !th.CanRestart("platform-api") {
		t.Fatal("first restart should be allowed")
	}
}

func TestThrottleBlocksWithin24h(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	th, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	th.RecordRestart("redis")

	// 23 hours later → still blocked
	now = now.Add(23 * time.Hour)
	if th.CanRestart("redis") {
		t.Fatal("should block restart within 24h")
	}

	// 24 hours later → allowed
	now = now.Add(1 * time.Hour)
	if !th.CanRestart("redis") {
		t.Fatal("should allow restart after 24h")
	}
}

func TestThrottlePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	th1, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	th1.RecordRestart("nginx-edge")

	// Reload from same dir
	th2, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if th2.CanRestart("nginx-edge") {
		t.Fatal("persisted throttle should block immediate re-restart")
	}
}

func TestThrottlePrunesOldEntries(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	th, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	th.RecordRestart("old-target")

	// Advance 49h (beyond 48h prune window), reload
	now = now.Add(49 * time.Hour)
	th2, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	// After prune, the old entry should be gone
	if !th2.CanRestart("old-target") {
		t.Fatal("pruned entry should allow restart")
	}
}

func TestThrottleFilePath(t *testing.T) {
	dir := t.TempDir()
	th, err := NewRestartThrottle(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	th.RecordRestart("test")
	expectedFile := filepath.Join(dir, "autopilot-throttle.json")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatal("throttle file should exist after RecordRestart")
	}
}

func TestThrottleDifferentTargetsIndependent(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	th, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	th.RecordRestart("redis")
	if !th.CanRestart("nginx-edge") {
		t.Fatal("different targets should be independent")
	}
	if th.CanRestart("redis") {
		t.Fatal("same target should be blocked")
	}
}
