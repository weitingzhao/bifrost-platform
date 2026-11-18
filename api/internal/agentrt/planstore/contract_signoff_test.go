package planstore_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// The Owner's sign-off, and what moves a step: through the contract (doc.go).

func (w *planWorld) sign(step, ref string, evidence ...string) (map[string]any, error) {
	return planstore.SignOffStep(contractCtx, w.rt, ownerActor, step, ref, evidence...)
}

func (w *planWorld) signoffs() int {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_signoff'`)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestSignOffStepIsTheOwners and the single-press half of TestSignOffStepOnceAtATime: only the Owner signs, whatever the route lets through, and a second press answers the first; the wrong outcome is an agent finishing a step that waits for the Owner.
func TestContractOnlyTheOwnerSignsOff(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.land("rule", doneRule(stepA, `{"owner_signoff":"x"}`))
	for _, who := range []string{"thread:night", "seat:chief_of_staff", "controller", ""} {
		_, err := planstore.SignOffStep(contractCtx, w.rt, who, stepA, "")
		wantIs(t, fmt.Sprintf("%q signing", who), err, planstore.ErrNotOwner)
	}
	if n := w.signoffs(); n != 0 {
		t.Fatalf("%d sign-offs written by others", n)
	}
	first, err := w.sign(stepA, "")
	must(t, err)
	if first["plan_node_id"] != stepA || first["ref"] != "x" || first["by"] != ownerActor || first["already_signed"] != false || first["event_id"] == nil || first["at"] == "" {
		t.Fatalf("the answer: %v", first)
	}
	if n := w.events("plan:R2", "owner_signoff"); n != 1 {
		t.Fatalf("%d sign-offs on the plan's stream", n)
	}
	again, err := w.sign(stepA, "x")
	must(t, err)
	if again["already_signed"] != true || again["event_id"] != first["event_id"] || w.signoffs() != 1 {
		t.Fatalf("the second press: %v (first %v), %d events", again, first, w.signoffs())
	}
}

// Origin: 2026-11-18, TD-582: the refusals SignOffStep's comment promises had no test of their own in the plan's tests: each kind of error tells the route and the Console something different (404, invalid, wrong state), and a sign-off that moves nothing must not be recorded as if it had.
func TestContractSignOffRefusals(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.land("two", doneRule(stepA, `{"all":[{"owner_signoff":"x"},{"owner_signoff":"y"}]}`))
	w.land("either", doneRule(stepB, `{"any":[{"owner_signoff":"x"},{"owner_signoff":"y"}]}`))
	w.land("dropped-rule", doneRule(stepC, `{"owner_signoff":"z"}`))
	evidence := w.artifact("report", "proof")
	for name, c := range map[string]struct {
		step, ref string
		evidence  []string
		want      error
	}{
		"no such node":                 {"R2:9", "", nil, pgx.ErrNoRows},
		"an item":                      {item1, "", nil, eventlog.ErrInvalid},
		"a rule with no sign-off":      {stepLive, "", nil, eventlog.ErrInvalid},
		"a manual rule":                {stepDone, "", nil, eventlog.ErrInvalid},
		"two refs and none named":      {stepA, "", nil, eventlog.ErrInvalid},
		"a ref the rule does not name": {stepA, "other", nil, eventlog.ErrInvalid},
		"evidence on a plain sign-off": {stepA, "x", []string{evidence}, eventlog.ErrInvalid},
	} {
		before := w.footprint()
		_, err := w.sign(c.step, c.ref, c.evidence...)
		wantIs(t, name, err, c.want)
		if w.footprint() != before {
			t.Errorf("%s: the refusal left something", name)
		}
	}
	// A step closed by what its rule needed: a further ref would move nothing.
	_, err := w.sign(stepB, "x")
	must(t, err)
	if got := w.evaluate(stepB); got != "done" {
		t.Fatalf("the step moved to %q", got)
	}
	_, err = w.sign(stepB, "y")
	wantIs(t, "a done step", err, eventlog.ErrState)
	// A dropped step.
	w.land("drop", planstore.PlanChange{Op: "drop_step", Node: stepC})
	_, err = w.sign(stepC, "z")
	wantIs(t, "a dropped step", err, eventlog.ErrState)
	if n := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_signoff' AND plan_node_id = ANY($1)`, []string{stepC}); n != 0 {
		t.Fatalf("%d sign-offs on the dropped step", n)
	}
	if n := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_signoff' AND plan_node_id = $1 AND body_json->>'ref' = 'y'`, stepB); n != 0 {
		t.Fatal("the refused ref was written")
	}
}

// Origin: 2026-11-18, TD-582: the contract version of the sign-off half of store's TestStepRules and of TestPlanChange's evaluation: a sign-off alone moves nothing, the controller's evaluation does, on the step it was given for, and a done step without a standing condition does not move again; the wrong outcomes are a step done by another step's sign-off, or a step finishing without the pass the Console shows.
func TestContractEvaluateStep(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.land("a", doneRule(stepA, `{"owner_signoff":"same"}`))
	w.land("b", doneRule(stepB, `{"owner_signoff":"same"}`))
	if got := w.evaluate(stepA); got != "" {
		t.Fatalf("an unmet rule moved the step to %q", got)
	}
	_, err := w.sign(stepB, "same")
	must(t, err)
	if got := w.node(stepB).state; got != "planned" {
		t.Fatalf("the sign-off itself moved the step to %s", got)
	}
	if got := w.evaluate(stepA); got != "" {
		t.Fatalf("a sign-off on another step moved this one to %q", got)
	}
	if got := w.evaluate(stepB); got != "done" {
		t.Fatalf("the signed step moved to %q", got)
	}
	var cause, actor string
	must(t, w.rt.QueryRow(contractCtx, `SELECT body_json->>'cause', actor_id FROM agentrt.event WHERE event_type = 'plan_node_state_changed'
	  AND plan_node_id = $1 AND event_stream_id = 'plan:R2' ORDER BY event_id DESC LIMIT 1`, stepB).Scan(&cause, &actor))
	if cause != "done_rule" || actor != "controller" {
		t.Fatalf("the move: cause %q by %q", cause, actor)
	}
	if got := w.evaluate(stepB); got != "" {
		t.Fatalf("a done step moved again: %q", got)
	}
	if got := w.evaluate(stepLive); got != "" {
		t.Fatalf("a step with no rule moved: %q", got)
	}
	_, err = planstore.EvaluateStep(contractCtx, w.rt, "R2:9", nil)
	wantIs(t, "no such step", err, pgx.ErrNoRows)
	_, err = planstore.EvaluateStep(contractCtx, w.rt, item1, nil)
	wantIs(t, "an item", err, pgx.ErrNoRows)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestRuledNodes: the controller's rules pass works from this list, so a dropped step on it would be evaluated for ever and an architecture row missing from it never ticks.
func TestContractRuledNodes(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	gates := `{"gates_passed":[{"test":"T19","spec_sha256":"` + strings.Repeat("ab", 32) + `"}],"env":"prod"}`
	w.importAndApply("import-arch", archRequest(t, gates))
	w.land("kept", doneRule(stepA, `{"owner_signoff":"a"}`))
	w.land("gone", doneRule(stepB, `{"owner_signoff":"b"}`))
	w.land("drop", planstore.PlanChange{Op: "drop_step", Node: stepB})
	nodes, err := planstore.RuledNodes(contractCtx, w.rt)
	must(t, err)
	got := map[string]string{}
	var ids []string
	for _, n := range nodes {
		got[n.ID] = n.Kind + "/" + n.State
		ids = append(ids, n.ID)
		if n.Rule.Empty() {
			t.Errorf("%s came with no rule", n.ID)
		}
		if n.ID == stepA && !n.Rule.Names("owner_signoff") {
			t.Errorf("%s: the rule read back does not name its atom", n.ID)
		}
		if n.ID == archRowA && !n.Rule.Names("gates_passed") {
			t.Errorf("%s: the rule read back does not name its atom", n.ID)
		}
	}
	want := map[string]string{archRowA: "arch_row/", stepA: "step/planned", stepDone: "step/done"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ruled nodes %v; want %v", got, want)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("not in id order: %v", ids)
		}
	}
}

// Origin: 2026-11-18, TD-582: the architecture row's rule is gate tests only (review 427 A2, store's TestArchRowRuleIsGatesOnly): a fact that stays true once recorded would tick the row whatever runs; the wrong outcome is a table saying a system is in place because someone once signed.
func TestContractArchRowRuleIsGatesOnly(t *testing.T) {
	t.Parallel()
	w := emptyWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	before, cards := w.footprint(), w.desk.count()
	_, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "row", archRoot, "r", []planstore.PlanChange{doneRule(archRowA, `{"owner_signoff":"x"}`)}, w.desk.ask)
	wantIs(t, "a sign-off on a row", err, eventlog.ErrInvalid)
	if !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(err.Error(), "gate tests only") {
		t.Fatalf("err = %v", err)
	}
	if w.footprint() != before || w.desk.count() != cards {
		t.Fatal("the refusal left something")
	}
	gates := `{"gates_passed":[{"test":"T19","spec_sha256":"` + strings.Repeat("ab", 32) + `"}],"env":"stg"}`
	out, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "row-ok", archRoot, "r", []planstore.PlanChange{doneRule(archRowA, gates)}, w.desk.ask)
	must(t, err)
	w.applyCard(out)
	if got := w.decisionOf(out); got != "applied" {
		t.Fatalf("the gate rule: %s (%s)", got, w.decisionError(out))
	}
}
