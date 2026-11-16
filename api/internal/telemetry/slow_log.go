package telemetry

import "time"

// LogSlow only formats an already observed duration; it does not schedule or act.
func LogSlow(d, min time.Duration, logf func(string, ...any), format string, args ...any) bool {
	if d < min {
		return false
	}
	logf(format+" took %s", append(args, d.Round(time.Millisecond))...)
	return true
}
