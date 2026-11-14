package store

import (
	"time"
)

// MetricSpanInterval is a half-open interval from recorded activity or cards.
// A zero End means the activity/card is still open at the observation time.
type MetricSpanInterval struct{ Start, End time.Time }

// MetricSpan is evidence for one wave, plan subtree (including a stage root),
// or group. Callers must include each member task once, including descendants.
// End is zero until all member commits are running on PROD; wave closure alone
// does not end delivery. Start is wave_opened or the first task start.
type MetricSpan struct {
	ScopeKind, ScopeRef, Label string
	Start, End                 time.Time
	Doing, OwnerWait           []MetricSpanInterval
	Accepted                   []time.Time // one first acceptance per distinct member task
	RemainingTasks             int
}
