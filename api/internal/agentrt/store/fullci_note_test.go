package store

import (
	"reflect"
	"testing"
)

// 2026-11-14: the daily full CI was the only run that would catch a module
// wrongly left out of a landing's CI, and a red one reached nobody: a log
// line, a note, and a goal for a dispatch lane that was being removed; it
// also named only Go tests. A red run's note carries the alert, the failed
// tasks and the suspects; the note of a day's starts carries no alert.
func TestFullCIRedRecordsAlertTasksAndSuspects(t *testing.T) {
	t.Parallel()
	b := (FullCI{Run: "daily-red", Commit: "main", FailedTasks: []string{"a", "b"}, Unread: 2, Unreconciled: "why",
		Suspects: []FullCISuspect{{Commit: "c1", Subject: "s", Run: "ci-1", Modules: " x y "}}}).body()
	want := map[string]any{"alert": true, "run": "daily-red", "commit": "main", "failed_tasks": []any{"a", "b"}, "unread": 2, "unreconciled": "why",
		"suspects": []any{map[string]any{"commit": "c1", "subject": "s", "run": "ci-1", "modules": " x y "}}}
	if !reflect.DeepEqual(b, want) {
		t.Fatalf("red note body: %v", b)
	}
	for _, gone := range []string{"mission", "open_mission", "open_run", "deviation"} {
		if _, ok := b[gone]; ok {
			t.Fatalf("the body still has %q", gone)
		}
	}
	if _, ok := (FullCI{Started: []string{"daily"}}).body()["alert"]; ok {
		t.Fatal("starting an audit raised an alert")
	}
}
