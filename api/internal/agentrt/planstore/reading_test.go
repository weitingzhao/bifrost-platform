package planstore_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-25, the map line asked the plan (map/done-rules-for-plan-2026-11-25.md):
// R6:6.9's steps are numbers the platform keeps (catalog/status) and no atom read one.
// The step must move on the kept number alone, and never while the document
// cannot be read (unknown is never done).
//
// Serial: it sets the process's reading sources (SetReadings).
func TestReadingMovesAStepOnAKeptNumber(t *testing.T) {
	w := newWorld(t)
	t.Cleanup(func() { planstore.SetReadings(nil) })
	w.rule(t, s3, `{"reading":{"source":"catalog.status","path":"inventory.*.gap","op":"==","value":0}}`)
	none := &deployments{}

	planstore.SetReadings(nil)
	if moved := w.evaluate(t, s3, none); moved != "" || w.stepState(t, s3) == "done" {
		t.Fatalf("no source: moved %q", moved)
	}
	doc := `{"inventory":[{"gap":0},{"gap":2}]}`
	planstore.SetReadings(func(_ context.Context, source string) ([]byte, bool) {
		if source != "catalog.status" {
			return nil, false
		}
		return []byte(doc), true
	})
	if moved := w.evaluate(t, s3, none); moved != "" {
		t.Fatalf("a gap of 2: moved %q", moved)
	}
	// Positive control of the case above: the same rule moves once the gap is 0.
	doc = `{"inventory":[{"gap":0},{"gap":0}]}`
	if moved := w.evaluate(t, s3, none); moved != "done" || w.stepState(t, s3) != "done" {
		t.Fatalf("gaps all 0: moved %q, state %s", moved, w.stepState(t, s3))
	}
}
