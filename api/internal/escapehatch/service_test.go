package escapehatch

import (
	"testing"
	"time"
)

func TestQuarterlyFromRecordOverdueWhenMissing(t *testing.T) {
	q := quarterlyFromRecord(nil)
	if !q.Overdue {
		t.Fatal("expected overdue when no drill recorded")
	}
	if q.IntervalDays != quarterlyIntervalDays {
		t.Fatalf("interval: got %d", q.IntervalDays)
	}
}

func TestQuarterlyFromRecordNotOverdueWhenRecent(t *testing.T) {
	at := time.Now().UTC().Add(-10 * 24 * time.Hour)
	rec := &DrillRecord{At: at, By: "owner"}
	q := quarterlyFromRecord(rec)
	if q.Overdue {
		t.Fatal("expected not overdue for 10-day-old drill")
	}
	if q.DaysSince == nil || *q.DaysSince < 9 {
		t.Fatalf("days_since: %v", q.DaysSince)
	}
}
