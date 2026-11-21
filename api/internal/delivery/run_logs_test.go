package delivery

import (
	"testing"
	"time"
)

func TestStripK8sLogTimestamps(t *testing.T) {
	raw := []byte(
		"2026-08-10T19:00:58.123456789Z INFO build start\n" +
			"2026-08-10T19:01:01.000000000Z INFO build done\n",
	)
	cleaned, last := stripK8sLogTimestamps(raw)
	if cleaned != "INFO build start\nINFO build done\n" {
		t.Fatalf("cleaned=%q", cleaned)
	}
	if last == nil {
		t.Fatal("expected last_log_at")
	}
	want := time.Date(2026, 8, 10, 19, 1, 1, 0, time.UTC)
	if !last.Equal(want) {
		t.Fatalf("last=%v want=%v", last, want)
	}
}

func TestStripK8sLogTimestampsNoPrefix(t *testing.T) {
	raw := []byte("plain line\n")
	cleaned, last := stripK8sLogTimestamps(raw)
	if cleaned != "plain line\n" {
		t.Fatalf("cleaned=%q", cleaned)
	}
	if last != nil {
		t.Fatalf("expected nil last, got %v", last)
	}
}
