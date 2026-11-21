package evalreadings

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// guardBlockedSince is from when the ledger holds every refusal with its own
// time (body blocked_at). A row's ledger time is when it was posted: the
// first rows were posted all at once from a local log (2026-11-21 22:38Z, 550
// rows in 2 seconds), and the producer posts each machine's new log lines
// every 5 minutes, so counting by it would misplace refusals. Zero until the
// producer writes blocked_at; set it to the earliest time the producer's
// replay of the local logs covers. Before it, every window is unmeasured.
var guardBlockedSince time.Time

// guardBlockReading: guard_blocks, the times in the window the agent guard
// (preflight.js) really refused an agent. Origin: the coordinator, 2026-11-21:
// archive "who the rules stopped" daily from the system record Foundation
// built. Rows whose session is "-" are left out: preflight's own tests wrote
// those into the local log (about 530 of the first 550), not a refusal of a
// session. The auto-mode classifier's refusals have no hook and are not here.
// farFuture is the open end of a read by posting time.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

func (s ledgerSource) guardBlockReading(w Window) (Reading, error) {
	const name = "guard_blocks"
	d := def("event ledger guard.block.recorded (one per refusal by preflight.js, POST /api/v1/bus/guard-blocks)",
		"rows whose session is not \"-\" and whose blocked_at is in the window; by rule in the labels")
	if guardBlockedSince.IsZero() {
		return finish(Reading{Name: name, Unit: "count", Window: w, Definition: d,
			Note: "the rows carry only the time they were posted, not the time of the refusal; the first ones were posted in one batch from a local log"}, false), nil
	}
	if w.From.Before(guardBlockedSince) {
		return finish(Reading{Name: name, Unit: "count", Window: w, Definition: d,
			Note: "the ledger keeps refusal times only since " + guardBlockedSince.Format(time.RFC3339) + "; this window starts earlier"}, false), nil
	}
	// A refusal is posted minutes to hours after it happened, and a replay of
	// the local logs posts old ones again: read every row posted from the
	// window's start on, place each by blocked_at, and count a refusal once
	// (same host, session, rule and time).
	rows, err := s.ofType("guard.block.recorded", w.From, farFuture)
	if err != nil {
		return Reading{}, fmt.Errorf("guard.block.recorded: %w", err)
	}
	byRule := map[string]int{}
	seen := map[string]bool{}
	n := 0
	for _, r := range rows {
		if sess := str(r.Body, "session"); sess == "" || sess == "-" {
			continue
		}
		at, ok := instant(r.Body, "blocked_at")
		if !ok || !in(at, w) {
			continue
		}
		key := strings.Join([]string{str(r.Body, "host"), str(r.Body, "session"), str(r.Body, "rule"), at.UTC().Format(time.RFC3339Nano)}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		byRule[str(r.Body, "rule")]++
		n++
	}
	var labels map[string]string
	if len(byRule) > 0 {
		labels = map[string]string{}
		rules := make([]string, 0, len(byRule))
		for k := range byRule {
			rules = append(rules, k)
		}
		sort.Strings(rules)
		parts := make([]string, 0, len(rules))
		for _, k := range rules {
			parts = append(parts, fmt.Sprintf("%s %d", k, byRule[k]))
		}
		labels["by_rule"] = strings.Join(parts, ", ")
	}
	r := finish(Reading{Name: name, Labels: labels, Value: count(n), Unit: "count", Samples: n, Window: w, Definition: d}, false)
	measuredZero(&r)
	return r, nil
}
