package evalreadings

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// guardBlockedSince is from when the ledger's guard.block.recorded rows carry
// the time of the refusal itself (body blocked_at). A row's own time is when
// it was posted, and the first rows were posted all at once from a local log
// (2026-11-21 22:38Z, 550 rows in 2 seconds), so counting by it would put
// every refusal ever made into that one day. Zero until the producer writes
// blocked_at: every window is then unmeasured.
var guardBlockedSince time.Time

// guardBlockReading: guard_blocks, the times in the window the agent guard
// (preflight.js) really refused an agent. Origin: the coordinator, 2026-11-21:
// archive "who the rules stopped" daily from the system record Foundation
// built. Rows whose session is "-" are left out: preflight's own tests wrote
// those into the local log (about 530 of the first 550), not a refusal of a
// session. The auto-mode classifier's refusals have no hook and are not here.
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
	// A refusal can be posted after its window closed: read every row
	// posted from the window's start on and place each by blocked_at.
	rows, err := s.ofType("guard.block.recorded", w.From, w.To.Add(7*24*time.Hour))
	if err != nil {
		return Reading{}, fmt.Errorf("guard.block.recorded: %w", err)
	}
	byRule := map[string]int{}
	n := 0
	for _, r := range rows {
		if sess := str(r.Body, "session"); sess == "" || sess == "-" {
			continue
		}
		at, ok := instant(r.Body, "blocked_at")
		if !ok || !in(at, w) {
			continue
		}
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
