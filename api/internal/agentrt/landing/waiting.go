package landing

import (
	"encoding/json"
	"slices"
	"time"
)

// Explicit versions keep historic 1.1 streams readable when Version advances.
var minimumVersion = map[string]int{
	"discarded": 2, "rehearsed": 2, "retry": 2, "mode": 2,
	"awaiting_grant": 3,
}

func recordVersion(r Record) int {
	if n, ok := r["v"].(json.Number); ok && n.String() == "1" {
		return 1
	}
	switch r.String("v") {
	case "1.1":
		return 2
	case "1.2":
		return 3
	}
	return 0
}

func validSoft(v any) bool {
	a, ok := v.([]any)
	if !ok || len(a) == 0 {
		return false
	}
	last := ""
	for _, x := range a {
		s, ok := x.(string)
		if !ok || s <= last || !slices.Contains([]string{"core", "floor_soft", "presentation", "rule_paths", "size"}, s) {
			return false
		}
		last = s
	}
	return true
}

const WaitWindow = 72 * time.Hour

// Interval is a half-open period of valid grant availability, not merely an
// approval. C-5's authoritative grant fold supplies these intervals and clips
// End to expiry, revocation or consumption. C-4 writes/reads no grants, so its
// production projection is empty. An empty End is invalid (never unlimited).
type Interval struct{ Start, End time.Time }

type Timing struct {
	Epoch              time.Time
	Execution, Waiting time.Duration
	Operations         int
}

// RootTiming is the only E/W/n calculation. The waiting phase is immutable per
// operation; leaving it ends its interval. Valid grants cut holes in that
// interval, so expiry/revocation resumes W at its actual time, even after a
// restart. Retry clips all intervals to the new epoch; n never resets.
func RootTiming(s State, root string, now time.Time) Timing {
	first, n := RootBudget(s, root)
	b := Timing{Epoch: first, Operations: n}
	if first.IsZero() || now.Before(first) {
		return b
	}
	for _, o := range s.Operations {
		if o.Task != root || o.AwaitingAt.IsZero() {
			continue
		}
		start, end := o.AwaitingAt, now
		if start.Before(first) {
			start = first
		}
		if !o.WaitEndedAt.IsZero() && o.WaitEndedAt.Before(end) {
			end = o.WaitEndedAt
		}
		if !start.Before(end) {
			continue
		}
		b.Waiting += end.Sub(start) - availableFor(o.GrantPeriods, start, end)
	}
	b.Execution = max(time.Duration(0), now.Sub(first)-b.Waiting)
	return b
}

// Union clipped intervals: duplicate/overlapping observations never subtract
// availability twice. Unknown, zero-length and future intervals count nothing.
func availableFor(periods []Interval, start, end time.Time) time.Duration {
	var clipped []Interval
	for _, p := range periods {
		if p.Start.IsZero() || p.End.IsZero() {
			continue
		}
		if p.Start.Before(start) {
			p.Start = start
		}
		if p.End.After(end) {
			p.End = end
		}
		if p.Start.Before(p.End) {
			clipped = append(clipped, p)
		}
	}
	slices.SortFunc(clipped, func(a, b Interval) int { return a.Start.Compare(b.Start) })
	var total time.Duration
	last := start
	for _, p := range clipped {
		if p.Start.Before(last) {
			p.Start = last
		}
		if p.Start.Before(p.End) {
			total += p.End.Sub(p.Start)
			last = p.End
		}
	}
	return total
}

// RootBound checks opening the next operation (next=true) or finishing the
// current one. Operation eight may finish; opening operation nine cannot.
// AtMain callers reconcile instead of using this clock to terminate a push.
func RootBound(s State, root string, now time.Time, next bool) string {
	b := RootTiming(s, root, now)
	if b.Waiting >= WaitWindow {
		return "approval_expired"
	}
	n := b.Operations
	if !next {
		n--
	}
	if Bound(b.Epoch, n, now, b.Waiting) {
		return "bound"
	}
	return ""
}
