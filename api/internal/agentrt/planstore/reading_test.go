package planstore_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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

// Origin: 2026-11-25: the plan-standing grant takes a first rule that reads a
// kept number only when the signed rule grants it (done_rules_readings) and
// the reading shows it does not hold yet; unknown is not "not yet".
//
// Serial: it sets the process's reading sources (SetReadings).
func TestStandingGrantForReadings(t *testing.T) {
	w := newWorld(t)
	t.Cleanup(func() { planstore.SetReadings(nil) })
	rule := json.RawMessage(`{"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<=","value":14}}`)
	change := planstore.PlanChange{Op: "set_done_rule", Node: s3, Rule: rule}
	check := func(grant planstore.PlanRule, doc string) bool {
		t.Helper()
		planstore.SetReadings(func(context.Context, string) ([]byte, bool) { return []byte(doc), doc != "" })
		var in bool
		ok(t, eventlog.InTx(ctxOf(t), w.rt, func(tx pgx.Tx) error {
			var err error
			in, err = planstore.StandingRuleForTest(ctxOf(t), tx, change, &grant)
			return err
		}))
		return in
	}
	granted := planstore.PlanRule{ID: "plan-standing", Version: 2, DoneRules: true, DoneRulesReadings: true}
	if !check(granted, `{"pool":{"oldest_days":30}}`) {
		t.Fatal("granted, not holding yet: not in the grant")
	}
	if check(planstore.PlanRule{ID: "plan-standing", Version: 1, DoneRules: true}, `{"pool":{"oldest_days":30}}`) {
		t.Fatal("no readings grant: in the grant")
	}
	if check(granted, `{"pool":{"oldest_days":3}}`) {
		t.Fatal("holding already: in the grant")
	}
	if check(granted, "") {
		t.Fatal("source unreadable: in the grant")
	}
}
