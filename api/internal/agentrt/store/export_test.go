package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClaimFor is ClaimNext with a lease of the test's choosing, so a test can
// start from a lease that has lapsed, or is past its grace, without waiting.
func ClaimFor(ctx context.Context, db DB, r ClaimRequest, ttl time.Duration) (*Claim, error) {
	return claimNext(ctx, db, r, claimOpts{ttl: ttl, effects: dispatched})
}

// ClaimExternalFor also hands out external work, which round 2 never
// dispatches: the fixture for the takeover rules of external tasks.
func ClaimExternalFor(ctx context.Context, db DB, r ClaimRequest, ttl time.Duration) (*Claim, error) {
	return claimNext(ctx, db, r, claimOpts{ttl: ttl, effects: append([]string{"external"}, dispatched...)})
}

// ReplayRoute decides a route from the inputs a route_decided body recorded:
// the route, the exclusions and the input's fingerprint.
func ReplayRoute(inputs map[string]any) ([]string, []any, string, error) {
	in, err := replayInput(inputs)
	if err != nil {
		return nil, nil, "", err
	}
	fp, err := in.fingerprint()
	route, excluded := in.decide()
	return route, excluded, fp, err
}

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	testHook.Store(&f)
	return func() { testHook.Store(nil) }
}

// MoveTaskForTest moves a task as the kernel does: the fixture that leases a
// given task directly (LeaseAttempt) moves it to leased with it.
func MoveTaskForTest(ctx context.Context, tx pgx.Tx, actor, taskID, to string) error {
	t, err := readTask(ctx, tx, taskID, true)
	if err != nil {
		return err
	}
	return moveTask(ctx, tx, actor, t, to, "test")
}

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
