package store

import (
	"strings"
	"testing"
	"time"
)

// Every new read snapshot reconstructs the attempted blocker through the
// production constructor. Append must retain the original interval identity
// and clock; this is separate from the terminal health observation deadline.
func TestB5HBlockClockSurvivesNewSnapshot(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	record, err := StgBlockedRecord(strings.Repeat("a", 40), "baseline_unknown", nil, at, "v1", map[string]any{"error": "baseline unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	envelope := func(r StgRecord) Event {
		return Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "STG release blocked", "stg_release": map[string]any(r)}}
	}
	empty, err := StgFold(nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, err := checkStgReleaseEvent(empty, envelope(record), 1, at)
	if err != nil {
		t.Fatal(err)
	}
	entries := []StgEntry{{Seq: 1, At: at, Event: envelope(checked)}}
	for scan := 1; scan <= 100; scan++ {
		s, err := StgFold(entries)
		if err != nil {
			t.Fatal(err)
		}
		later := at.Add(time.Duration(scan) * time.Minute)
		rebuilt, err := StgBlockedRecord(strings.Repeat("a", 40), "baseline_unknown", nil, later, "v1", map[string]any{"error": "baseline unavailable"})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = checkStgReleaseEvent(s, envelope(rebuilt), 2, later)
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("new snapshot reset existing blocking interval: scan=%d err=%v", scan, err)
		}
		// Re-read, since the append checker is allowed to mutate its snapshot.
		s, err = StgFold(entries)
		if err != nil {
			t.Fatal(err)
		}
		b := s.Blocks[record.String("block")]
		if len(s.Records) != 1 || len(s.Blocks) != 1 || len(s.BlockHistory) != 1 || b.Opened.Seq != 1 || !b.Opened.At.Equal(at) || !b.Opened.Record.Time("first_at").Equal(at) || !b.IsOpen() {
			t.Fatal("block identity, count or first clock changed", b)
		}
	}
}
