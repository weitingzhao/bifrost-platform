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

// Serial: reviewOpenPG changes FreezeEventClock. On a3651f81 this fails at
// "original landing prerequisite" once database now reaches 2026-10-28
// 17:49 UTC. Keep the SQL timestamps real: freezing event time does not
// freeze mission/task created_at, including for the later reversal root.
func TestReview749OpenLandingTimelinePG(t *testing.T) {
	x, _ := reviewOpenPG(t)
	s := landingState(t, x.w, landingRepoStream)
	o := s.Operations[x.op]
	if o.Phase != "landed" || !o.EverLanded || o.ClaimedAt.IsZero() || !o.PreparedAt.After(o.ClaimedAt) || !x.at.After(o.PreparedAt) {
		t.Fatalf("landing timeline: operation=%+v gate=%s", o, x.at)
	}
	if why := landing.RootBound(s, x.root, x.at, false); why != "" {
		t.Fatalf("fresh database-clock root exhausted at gate: %s (claim=%s prepared=%s gate=%s)", why, o.ClaimedAt, o.PreparedAt, x.at)
	}
	var missionCreated, taskCreated, accepted, databaseNow time.Time
	ok(t, x.w.rt.QueryRow(ctx, `SELECT m.created_at, t.created_at,
      (SELECT max(at) FROM agentrt.event WHERE task_id=t.task_id AND event_type='task_state_changed' AND body_json->>'to'='accepted'), now()
      FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id=$1`, x.root).
		Scan(&missionCreated, &taskCreated, &accepted, &databaseNow))
	fence := s.Fence()
	if fence.IsZero() || fence.After(databaseNow) || missionCreated.Before(fence) || taskCreated.Before(fence) || accepted.Before(fence) || !x.at.After(taskCreated) {
		t.Fatalf("fixture ordering: fence=%s database_now=%s mission=%s task=%s accepted=%s gate=%s", fence, databaseNow, missionCreated, taskCreated, accepted, x.at)
	}
	q := landingState(t, x.w, "landing-quota:dev:"+x.at.UTC().Format("2006-01-02"))
	if q.Reservations[x.root] != x.op || x.read(t).Batches[b5Batch].Phase != "OPEN" {
		t.Fatal("landing did not reserve quota and admit the STG batch", q.Reservations)
	}
}
