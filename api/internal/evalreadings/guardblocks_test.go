package evalreadings

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// 2026-11-21, the coordinator: archive the guard's real refusals daily. The
// first 550 rows were posted in 2 seconds from a local log, about 530 of them
// with session "-" (preflight's own tests). So: "-" is left out, a row is
// placed by its blocked_at, not by when it was posted, and before the producer
// carries blocked_at every window is unmeasured. Foundation, same day: lines
// are posted every 5 minutes and the whole log is replayed once with
// blocked_at, so a refusal posted twice counts once.
func TestGuardBlocks(t *testing.T) {
	day := ClosedWindow(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC), 1)
	posted := day.To.Add(time.Hour) // posted after the window closed
	b := func(rule, session string, at time.Time) eventlog.Row {
		return row("guard.block.recorded", posted, map[string]any{"rule": rule, "session": session, "blocked_at": ts(at)})
	}
	rows := []eventlog.Row{
		b("D10", "s1", day.From.Add(time.Hour)),
		b("nas", "s2", day.From.Add(2*time.Hour)),
		b("nas", "s2", day.From.Add(3*time.Hour)),
		b("nas", "s2", day.From.Add(3*time.Hour)), // the replay posts it again
		b("nas", "-", day.From.Add(3*time.Hour)),
		b("D10", "s1", day.From.Add(-time.Hour)),
		row("guard.block.recorded", day.From.Add(time.Hour), map[string]any{"rule": "D10", "session": "s3"}),
	}
	src := ledgerSource{ofType: func(typ string, _, _ time.Time) ([]eventlog.Row, error) {
		if typ != "guard.block.recorded" {
			t.Fatalf("read %s", typ)
		}
		return rows, nil
	}}
	defer func(v time.Time) { guardBlockedSince = v }(guardBlockedSince)

	guardBlockedSince = time.Time{}
	if r, _ := src.guardBlockReading(day); r.Status != StatusUnmeasured || r.Value != nil {
		t.Fatalf("no refusal times yet must be unmeasured: %+v", r)
	}
	guardBlockedSince = day.From.Add(-24 * time.Hour)
	r, _ := src.guardBlockReading(day)
	if r.Status != StatusOK || *r.Value != 3 || r.Labels["by_rule"] != "D10 1, nas 2" {
		t.Fatalf("three real refusals in the day: %+v", r)
	}
	empty := ledgerSource{ofType: func(string, time.Time, time.Time) ([]eventlog.Row, error) { return nil, nil }}
	if r, _ := empty.guardBlockReading(day); r.Status != StatusOK || *r.Value != 0 {
		t.Fatalf("no refusal is a measured 0: %+v", r)
	}
	guardBlockedSince = day.From.Add(time.Hour)
	if r, _ := src.guardBlockReading(day); r.Status != StatusUnmeasured {
		t.Fatalf("a window starting before the start: %+v", r)
	}
}
