//bifrost:owner plan

package store

import (
	"fmt"
	"testing"
	"time"
)

// LiveForTest is a live attempt as the projection reads one: its state,
// whether its host is known, when the host last polled, and its elapsed
// seconds.
func LiveForTest(id, state string, hostKnown bool, polled *time.Time, elapsed *int64, started time.Time) *ProgressAttempt {
	a := &ProgressAttempt{AttemptID: id, HostID: "h1", State: state, hostKnown: hostKnown, hostPolled: polled, ElapsedS: elapsed, started: started}
	if !started.IsZero() {
		a.StartedAt = started.UTC().Format(time.RFC3339Nano)
	}
	return a
}

// SortLiveForTest is the projection's order of live attempts.
func SortLiveForTest(list []*ProgressAttempt) []string {
	m := map[string]*ProgressAttempt{}
	for _, a := range list {
		m[a.AttemptID] = a
	}
	var ids []string
	for _, a := range sortedLive(m) {
		ids = append(ids, a.AttemptID)
	}
	return ids
}

// Live attempts in start order by time, not by the text of the time (a
// fraction of a second sorts after the whole second).
func TestLiveOrder(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	// The ids sort the other way round from the times; as text,
	// "…00.1Z" sorts before "…00Z".
	got := SortLiveForTest([]*ProgressAttempt{
		LiveForTest("a_a_late", "running", true, nil, nil, base.Add(100*time.Millisecond)),
		LiveForTest("a_b_first", "running", true, nil, nil, base),
		LiveForTest("a_0_none", "leased", true, nil, nil, time.Time{}),
	})
	if fmt.Sprint(got) != "[a_b_first a_a_late a_0_none]" {
		t.Fatalf("order %v", got)
	}
}
