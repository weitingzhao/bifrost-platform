package telemetry

import (
	"strings"
	"time"
)

// SplitK8sLogTimestamp peels a kubectl --timestamps prefix (RFC3339 / RFC3339Nano + space).
func SplitK8sLogTimestamp(line string) (ts time.Time, rest string, ok bool) {
	idx := strings.Index(line, "Z ")
	if idx < 0 {
		return time.Time{}, line, false
	}
	prefix := line[:idx+1]
	t, err := time.Parse(time.RFC3339Nano, prefix)
	if err != nil {
		t, err = time.Parse(time.RFC3339, prefix)
		if err != nil {
			return time.Time{}, line, false
		}
	}
	return t, line[idx+2:], true
}

// StripK8sLogTimestamps removes leading timestamps from pod log bytes and returns
// the latest timestamp found (UTC). Body text stays otherwise unchanged.
func StripK8sLogTimestamps(data []byte) (cleaned string, lastAt *time.Time) {
	if len(data) == 0 {
		return "", nil
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.Grow(len(text))
	var latest time.Time
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if line == "" {
			continue
		}
		ts, rest, ok := SplitK8sLogTimestamp(line)
		if ok {
			if ts.After(latest) {
				latest = ts
			}
			b.WriteString(rest)
		} else {
			b.WriteString(line)
		}
	}
	if !latest.IsZero() {
		u := latest.UTC()
		lastAt = &u
	}
	return b.String(), lastAt
}

func MaxTimePtr(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if b.After(*a) {
		return b
	}
	return a
}
