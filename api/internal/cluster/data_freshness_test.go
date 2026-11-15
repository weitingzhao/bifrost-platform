package cluster

import (
	"testing"
	"time"
)

func TestFreshnessVerdictFromLag(t *testing.T) {
	cases := []struct {
		days float64
		want string
	}{
		{0, "fresh"},
		{2.9, "fresh"},
		{3.0, "aging"},
		{5.0, "aging"},
		{6.9, "aging"},
		{7.0, "stale"},
		{7.5, "stale"},
	}
	for _, tc := range cases {
		if got := freshnessVerdictFromLag(tc.days); got != tc.want {
			t.Errorf("freshnessVerdictFromLag(%.1f)=%s want %s", tc.days, got, tc.want)
		}
	}
}

func TestFreshnessLagSemantics(t *testing.T) {
	// lag=0 → fresh (even if wall age would be huge)
	if got := freshnessVerdictFromLag(0); got != "fresh" {
		t.Fatalf("lag=0 → fresh, got %s", got)
	}
	// lag=5 → aging / yellow (aligned with badge)
	if got := freshnessVerdictFromLag(5); got != "aging" {
		t.Fatalf("lag=5 → aging, got %s", got)
	}
}

func TestParsePostgresTimestamp(t *testing.T) {
	ts, err := parsePostgresTimestamp("2026-07-20 12:00:00+00")
	if err != nil {
		t.Fatal(err)
	}
	if ts.Year() != 2026 || ts.Month() != time.July || ts.Day() != 20 {
		t.Fatalf("unexpected ts %v", ts)
	}
	_, err = parsePostgresTimestamp("not-a-date")
	if err == nil {
		t.Fatal("expected error")
	}
}
