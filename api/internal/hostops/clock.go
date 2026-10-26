package hostops

import (
	"errors"
	"time"
)

// ClockObservation is supplied by the local trusted adapter, never a request
// or signed timestamp. Every authorization/step verifier checks its own clock.
type ClockObservation struct {
	SyncedAt   time.Time
	Offset     time.Duration
	RolledBack bool
}

func (c ClockObservation) Check(now time.Time) error {
	if now.IsZero() || c.RolledBack || c.SyncedAt.IsZero() || c.SyncedAt.After(now) || now.Sub(c.SyncedAt) > 5*time.Minute || c.Offset > 5*time.Second || c.Offset < -5*time.Second {
		return errors.New("clock unknown, stale, skewed or rolled back")
	}
	return nil
}

func (f Facts) clock() ClockObservation {
	return ClockObservation{f.ClockSyncedAt, f.ClockOffset, f.ClockRolledBack}
}
