package store

import (
	"context"
	"time"
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
	testHook = f
	return func() { testHook = nil }
}
