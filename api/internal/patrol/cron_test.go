package patrol

import (
	"testing"
	"time"
)

func TestParseCronAndNextAfter(t *testing.T) {
	from := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	next, err := NextAfter("0 3 * * *", from)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s want %s", next, want)
	}

	// Monday 06:00 — 2026-08-09 is Sunday.
	nextMon, err := NextAfter("0 6 * * 1", from)
	if err != nil {
		t.Fatal(err)
	}
	wantMon := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	if !nextMon.Equal(wantMon) {
		t.Fatalf("next monday = %s want %s", nextMon, wantMon)
	}
}

func TestParseCronInvalid(t *testing.T) {
	if _, err := ParseCron("* * *"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseCron("99 3 * * *"); err == nil {
		t.Fatal("expected minute range error")
	}
}
