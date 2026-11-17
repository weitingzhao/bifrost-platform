package planstore

import (
	"testing"
	"time"
)

// 2026-11-17 audit row #13 (LANE-BIZ-ALIGN): freshness.database.status was the
// constant "Fresh"; it now comes from the snapshot's read time and is Unknown
// when the read returned none.
func TestDatabaseFreshnessFromReadTime(t *testing.T) {
	if got := databaseFreshness(time.Time{}); got != "Unknown" {
		t.Fatalf("no read time: got %s, want Unknown", got)
	}
	if got := databaseFreshness(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)); got != "Fresh" {
		t.Fatalf("with a read time: got %s, want Fresh", got)
	}
}
