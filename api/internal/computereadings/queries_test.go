package computereadings

import (
	"strings"
	"testing"
)

// Origin: 2026-11-20 (law 43 readings), moved with the queries 2026-11-22.
// Every compute reading is named once, says what it answers, and looks back
// the window, except the "now" ones. (The check against the overview presets
// went: a group runs alone, so its ids only need to be unique within it.)
func TestComputeQueriesAreNamedOnceAndSayWhatTheyAnswer(t *testing.T) {
	seen := map[string]bool{}
	for _, q := range Queries() {
		if seen[q.ID] {
			t.Errorf("%s is defined twice", q.ID)
		}
		seen[q.ID] = true
		if !strings.HasPrefix(q.ID, "compute_") || q.Title == "" || q.Unit == "" || q.Target == "" || q.Note == "" {
			t.Errorf("%s: id prefix, title, unit, target and note are all required: %+v", q.ID, q)
		}
		if expr := q.Build("ignored"); !strings.Contains(expr, computeWindow) && q.ID != "compute_pods_evicted" && !strings.HasSuffix(q.ID, "_now") {
			t.Errorf("%s does not look back %s: %s", q.ID, computeWindow, expr)
		}
	}
}
