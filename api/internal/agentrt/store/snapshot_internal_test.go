package store

import (
	"strings"
	"testing"
	"time"
)

// RenderSnapshot writes given records as planSnapshot writes the database's:
// empty parts say None., a long one is cut and the fixed part kept whole.
func TestRenderSnapshot(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 19, 21, 0, 0, 0, time.UTC)
	empty := RenderSnapshot(Snapshot{Taken: at, Mission: "M-1", Budget: `{"max_attempts": 3}`})
	for _, want := range []string{"Taken 2026-10-19T21:00:00Z.", "### Missions open besides this one\n\nNone.", "### Their unfinished tasks\n\nNone.",
		"(newest first)\n\nNone.", "No standing envelope is in force", `This mission (M-1): budget {"max_attempts": 3}.`, "- bifrost-platform: "} {
		if !strings.Contains(empty, want) {
			t.Errorf("the empty snapshot lacks %q:\n%s", want, empty)
		}
	}
	s := Snapshot{Taken: at, Mission: "M-2", Budget: "{}", Envelope: "standing-envelope:dev v1: up to none."}
	for i := 0; i < 2000; i++ {
		s.Missions = append(s.Missions, SnapshotMission{ID: "M-9", State: "active", Title: strings.Repeat("标题", 40), Goal: strings.Repeat("目标", 100)})
	}
	s.Decisions = []SnapshotDecision{{At: at, Mission: "M-3", Decision: "d", Why: "w", Dropped: "x", Reverse: "y"}}
	long := RenderSnapshot(s)
	if len(long) > snapMaxBytes || !strings.Contains(long, "(The records above were longer; they are cut here.)") ||
		!strings.Contains(long, "standing-envelope:dev v1: up to none.\nThis mission (M-2): budget {}.") || !strings.HasSuffix(long, "\n") {
		t.Fatalf("a long snapshot of %d bytes:\n%.300s\n…\n%s", len(long), long, long[max(0, len(long)-600):])
	}
}
