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

func TestFreshnessBadgeLevel(t *testing.T) {
	d := func(v float64) *float64 { return &v }
	cases := []struct {
		days    *float64
		verdict string
		want    string
	}{
		{d(0), "fresh", "green"},
		{d(1), "fresh", "green"},
		{d(5), "aging", "yellow"},
		{d(10), "stale", "red"},
		{nil, "unknown", "unknown"},
		{d(51), "reference", "reference"}, // wall age must not drive badge for prod
		{nil, "reference", "reference"},
	}
	for _, tc := range cases {
		if got := FreshnessBadgeLevel(tc.days, tc.verdict); got != tc.want {
			t.Errorf("badge(%v,%s)=%s want %s", tc.days, tc.verdict, got, tc.want)
		}
	}
}

func TestFreshnessLagSemantics(t *testing.T) {
	// lag=0 → fresh (even if wall age would be huge)
	if got := freshnessVerdictFromLag(0); got != "fresh" {
		t.Fatalf("lag=0 → fresh, got %s", got)
	}
	if got := FreshnessBadgeLevel(ptrFloat(0), "fresh"); got != "green" {
		t.Fatalf("lag=0 badge green, got %s", got)
	}
	// lag=5 → aging / yellow (aligned with badge)
	if got := freshnessVerdictFromLag(5); got != "aging" {
		t.Fatalf("lag=5 → aging, got %s", got)
	}
	if got := FreshnessBadgeLevel(ptrFloat(5), "aging"); got != "yellow" {
		t.Fatalf("lag=5 badge yellow, got %s", got)
	}
	// prod reference is not scored by wall age
	if got := FreshnessBadgeLevel(ptrFloat(51), "reference"); got != "reference" {
		t.Fatalf("prod reference badge, got %s", got)
	}
}

func ptrFloat(v float64) *float64 { return &v }

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
