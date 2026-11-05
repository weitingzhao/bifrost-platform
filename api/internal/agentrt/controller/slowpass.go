package controller

import "time"

// Where a loop's minutes go (R5:5.7-5): a loop pass, or one landing
// operation within the landing drill, that takes at least this long is logged
// with its time. 11-05 a landing waited 3.5 minutes between its CI ending and
// ci_seen while the drill's ticker is 15 seconds: a slow pass, not the
// interval. Logging only; nothing waits on it or changes because of it.
var (
	slowPassLog = 10 * time.Second
	slowStepLog = 5 * time.Second
)

// slow logs "<what> took <d>" when at least min passed since start, and says
// whether it did.
func (c *Controller) slow(min time.Duration, start time.Time, format string, args ...any) bool {
	d := time.Since(start)
	if d < min {
		return false
	}
	c.logf(format+" took %s", append(args, d.Round(time.Millisecond))...)
	return true
}
