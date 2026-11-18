package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// step adds a step of M-1 under the item, with a done rule when rule is set.
func (w *world) step(t *testing.T, id string, ordinal int, rule string, binds ...[2]string) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: id, Parent: item, Kind: "step", Label: id, Title: id,
			MissionID: "M-1", StepKind: "agent", Ordinal: ordinal, Root: root})
		return err
	}))
	if rule != "" {
		w.rule(t, id, rule, binds...)
	}
}

// rule sets a node's done rule and binds approvals or questions to it, as an
// authorized plan change does.
func (w *world) rule(t *testing.T, node, rule string, binds ...[2]string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, planChange(w, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, node, rule); err != nil {
			return err
		}
		for _, b := range binds {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.plan_node_binding (plan_node_id, bound_kind, bound_ref, event_id)
			  VALUES ($1, $2, $3, current_setting('agentrt.event_id')::bigint)`, node, b[0], b[1]); err != nil {
				return err
			}
		}
		return nil
	}))
}

func (w *world) stepState(t *testing.T, id string) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT state FROM agentrt.plan_node WHERE plan_node_id = $1`, id).Scan(&s))
	return s
}

// Readers of outside records that always return the given value.
func approvalIs(status string) func(context.Context) (seenfacts.ApprovalFact, bool) {
	return func(context.Context) (seenfacts.ApprovalFact, bool) {
		return seenfacts.ApprovalFact{Status: status, Action: "test", Tier: "test"}, true
	}
}

func questionIs(q seenfacts.Question) func(context.Context) (seenfacts.Question, bool) {
	return func(context.Context) (seenfacts.Question, bool) { return q, true }
}

func ciIs(status, run string) func(context.Context) (string, string, bool) {
	return func(context.Context) (string, string, bool) { return status, run, true }
}

func deployedIs(commit string, ref map[string]any) seenfacts.Deployed {
	return func(context.Context, string) (string, map[string]any, bool) { return commit, ref, true }
}

// answered is a full answer: option, who, when.
func answered(id, option string) seenfacts.Question {
	return seenfacts.Question{ID: id, Status: seenfacts.QuestionAnswered, Option: option, Text: "yes", AnsweredBy: "owner",
		AnsweredAt: "2026-10-14T12:00:00Z"}
}

// deployments is a fake delivery record: what each environment runs, or
// that it cannot be read.
type deployments struct {
	mu sync.Mutex
	m  map[string]deployment
}

type deployment struct {
	commit string
	ref    map[string]any
}

func (d *deployments) read(_ context.Context, env string) (string, map[string]any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	x, ok := d.m[env]
	return x.commit, x.ref, ok
}

func (w *world) evaluate(t *testing.T, step string, d *deployments) string {
	t.Helper()
	ctx := ctxOf(t)
	moved, err := planstore.EvaluateStep(ctx, w.rt, step, d.read)
	ok(t, err)
	return moved
}

// External facts are recorded when they change, once per change even when
// several controllers see it at once.
//
// Serial: it holds the package's test hook (SetHook).
func TestExternalFactsRecordChangesOnly(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct {
		see  func() (bool, error)
		want bool
	}{
		{func() (bool, error) { return seenfacts.SeeApproval(ctx, w.rt, 16, approvalIs("pending")) }, true},
		{func() (bool, error) { return seenfacts.SeeApproval(ctx, w.rt, 16, approvalIs("pending")) }, false},
		{func() (bool, error) {
			return seenfacts.SeeApproval(ctx, w.rt, 16, approvalIs(seenfacts.ApprovalExecuted))
		}, true},
		{func() (bool, error) { return seenfacts.SeeApproval(ctx, w.rt, 160, approvalIs("pending")) }, true},
		{func() (bool, error) {
			return seenfacts.SeeQuestion(ctx, w.rt, "Q3", questionIs(seenfacts.Question{ID: "Q3", Status: "pending"}))
		}, true},
		{func() (bool, error) {
			return seenfacts.SeeQuestion(ctx, w.rt, "Q3", questionIs(answered("Q3", "A")))
		}, true},
		{func() (bool, error) {
			return seenfacts.SeeQuestion(ctx, w.rt, "Q3", questionIs(answered("Q3", "A")))
		}, false},
		{func() (bool, error) {
			return seenfacts.SeeCI(ctx, w.rt, "bifrost-platform", commitA, ciIs("Running", "ci-1"))
		}, true},
		{func() (bool, error) {
			return seenfacts.SeeCI(ctx, w.rt, "bifrost-platform", commitA, ciIs(seenfacts.CISucceeded, "ci-1"))
		}, true},
		{func() (bool, error) {
			return seenfacts.SeeCI(ctx, w.rt, "bifrost-platform", commitA, ciIs(seenfacts.CISucceeded, "ci-1"))
		}, false},
		{func() (bool, error) {
			return seenfacts.SeeDeploy(ctx, w.rt, "stg", deployedIs(commitA, map[string]any{"run": "d-1"}))
		}, true},
		{func() (bool, error) {
			return seenfacts.SeeDeploy(ctx, w.rt, "stg", deployedIs(commitA, map[string]any{"run": "d-1"}))
		}, false},
		{func() (bool, error) {
			return seenfacts.SeeDeploy(ctx, w.rt, "stg", deployedIs(commitA, map[string]any{"run": "d-2"}))
		}, true},
	} {
		got, err := c.see()
		ok(t, err)
		if got != c.want {
			t.Fatalf("recorded = %v; want %v", got, c.want)
		}
	}

	// Five controllers see one change at once; all of them read the last
	// record before any writes, unless the key's lock makes them wait.
	pool := w.db.Pool(t, w.db.Runtime)
	const n = 5
	arrived := make(chan struct{}, n)
	unhook := store.SetHook(func(point, _ string) {
		if point != "seen" {
			return
		}
		arrived <- struct{}{}
		deadline := time.After(300 * time.Millisecond)
		for len(arrived) < n {
			select {
			case <-deadline:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = seenfacts.SeeApproval(ctx, pool, 18, approvalIs(seenfacts.ApprovalExecuted))
		}(i)
	}
	wg.Wait()
	unhook()
	for _, err := range errs {
		ok(t, err)
	}
	var events int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'approval_seen' AND body_json @> '{"approval": 18}'`).Scan(&events))
	if events != 1 {
		t.Fatalf("%d approval_seen events for one change", events)
	}
	w.chainHolds(t, seenfacts.ExternalStream)
}

// A step's done rule moves it once its facts hold, and only facts bound to
// the step count (v5 §5.1.2).
func TestStepRules(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	d := &deployments{}
	w.rule(t, s1, `{"all":[{"approval_executed":16},{"question_answered":{"question":"Q3","option":"A"}},`+
		`{"ci_succeeded":{"repo":"bifrost-platform","commit":"`+commitA+`"}}]}`, [2]string{"approval", "16"}, [2]string{"question", "Q3"})
	if w.evaluate(t, s1, d) != "" {
		t.Fatal("a rule with no facts moved the step")
	}
	_, err := seenfacts.SeeApproval(ctx, w.rt, 16, approvalIs(seenfacts.ApprovalExecuted))
	ok(t, err)
	_, err = seenfacts.SeeQuestion(ctx, w.rt, "Q3", questionIs(answered("Q3", "B")))
	ok(t, err)
	_, err = seenfacts.SeeCI(ctx, w.rt, "bifrost-platform", commitA, ciIs(seenfacts.CISucceeded, ""))
	ok(t, err)
	if w.evaluate(t, s1, d) != "" {
		t.Fatal("option B answered option A's atom")
	}
	_, err = seenfacts.SeeQuestion(ctx, w.rt, "Q3", questionIs(answered("Q3", "A")))
	ok(t, err)
	if got := w.evaluate(t, s1, d); got != "done" || w.stepState(t, s1) != "done" {
		t.Fatalf("moved to %q", got)
	}
	var cause string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'cause' FROM agentrt.event WHERE event_type = 'plan_node_state_changed' AND plan_node_id = $1
	  ORDER BY event_id DESC LIMIT 1`, s1).Scan(&cause))
	if cause != "done_rule" || w.evaluate(t, s1, d) != "" {
		t.Fatalf("cause %q, or a done step moved again", cause)
	}

	// A rule naming an approval or a question not bound to its step cannot
	// be written (the 2.1 guard); Facts checks the binding again.
	for _, rule := range []string{`{"approval_executed":17}`, `{"question_answered":{"question":"Q9","option":"A"}}`} {
		wantErr(t, planChange(w, func(tx pgx.Tx) error {
			_, execErr := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, s3, rule)
			return execErr
		}), "not bound to this step")
	}

	// A sign-off counts on its own step only.
	w.rule(t, s2, `{"owner_signoff":"TD-1"}`)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return planstore.SignOff(ctx, tx, "owner", s3, "TD-1") }))
	if w.evaluate(t, s2, d) != "" {
		t.Fatal("a sign-off on another step moved this one")
	}
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { return planstore.SignOff(ctx, tx, "owner", s2, "TD-1") }))
	if w.evaluate(t, s2, d) != "done" {
		t.Fatal("the sign-off did not move its step")
	}

	// A manual atom needs a record with evidence on its step, and the move
	// says it was manual.
	w.step(t, "R2:2.1-4", 3, `{"manual":"checked by hand"}`)
	err = eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return planstore.RecordManual(ctx, tx, "owner", "R2:2.1-4", "checked by hand", "looked at it", nil)
	})
	is(t, err, eventlog.ErrState)
	proof := w.artifact(t, "other", "screenshot")
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return planstore.RecordManual(ctx, tx, "owner", "R2:2.1-4", "checked by hand", "looked at it", []string{proof})
	}))
	if w.evaluate(t, "R2:2.1-4", d) != "done" {
		t.Fatal("the manual record did not move its step")
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'cause' FROM agentrt.event WHERE event_type = 'plan_node_state_changed' AND plan_node_id = $1
	  ORDER BY event_id DESC LIMIT 1`, "R2:2.1-4").Scan(&cause))
	if cause != "done_rule_manual" {
		t.Fatalf("cause = %q", cause)
	}

	// task_accepted answers false (store cut 4): the rule parses and a step
	// that names a task never becomes done by it.
	w.step(t, "R2:2.1-5", 4, "")
	w.rule(t, "R2:2.1-5", `{"task_accepted":"t_01M5192P0JPMJAP63ZKRPXJ2VV"}`)
	if w.evaluate(t, "R2:2.1-5", d) != "" {
		t.Fatal("a task_accepted rule moved the step")
	}
}

// RuledNodes lists live steps and architecture rows with a rule.
func TestRuledNodes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.rule(t, s1, `{"owner_signoff":"a"}`)
	w.rule(t, s2, `{"owner_signoff":"b"}`)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.SetStepState(ctx, tx, "controller", root, s2, "dropped", "test")
		return err
	}))
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, n := range []planstore.PlanNode{
			{ID: "A", Kind: "arch_root", Label: "A", Title: "architecture", Root: "A"},
			{ID: "A:1", Parent: "A", Kind: "arch_row", Label: "1", Title: "state database", Root: "A"},
		} {
			if _, err := planstore.CreatePlanNode(ctx, tx, "controller", n); err != nil {
				return err
			}
		}
		return nil
	}))
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream("A"), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "test"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, "A:1",
			`{"gates_passed":[{"test":"T19","spec_sha256":"`+strings.Repeat("ab", 32)+`"}],"env":"prod"}`)
		return err
	}))
	nodes, err := planstore.RuledNodes(ctx, w.rt)
	ok(t, err)
	var got []string
	for _, n := range nodes {
		got = append(got, n.ID+"/"+n.Kind)
	}
	if strings.Join(got, ",") != "A:1/arch_row,"+s1+"/step" {
		t.Fatalf("ruled nodes = %v", got)
	}
}
