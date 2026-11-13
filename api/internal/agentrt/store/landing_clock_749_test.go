package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Review 749's exact counterexample, using the real six-hour budget check.
// The PG test below also exercises the fixture and its persisted records.
func TestReview749RootWindowCounterexample(t *testing.T) {
	t.Parallel()
	databaseNow := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	gate := databaseNow.Add(10 * time.Minute)
	for _, tc := range []struct {
		name     string
		prepared time.Time
		elapsed  time.Duration
		want     string
	}{
		{"historical claim with database gate", time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), 36*time.Hour + 11*time.Minute, "bound"},
		{"database claim with database gate", databaseNow, 11 * time.Minute, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := landing.State{Operations: map[string]landing.Operation{
				"op": {Task: "root", Phase: "branch_pushed", ClaimedAt: tc.prepared.Add(-time.Minute), PreparedAt: tc.prepared},
			}}
			if got := landing.RootTiming(s, "root", gate).Execution; got != tc.elapsed {
				t.Fatalf("execution=%s, want %s", got, tc.elapsed)
			}
			if got := landing.RootBound(s, "root", gate, false); got != tc.want {
				t.Fatalf("root bound=%q, want %q", got, tc.want)
			}
		})
	}
}
