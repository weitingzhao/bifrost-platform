package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func pvDocument(t *testing.T, version int) *planview.Document {
	t.Helper()
	raw, err := os.ReadFile("../planview/testdata/r4-v1.json")
	ok(t, err)
	d, err := planview.Parse(raw)
	ok(t, err)
	d.Root = root
	d.Version = version
	d.Items = d.Items[:1]
	n := &d.Items[0]
	n.Node = "2.1"
	n.Parent = root
	n.Dependencies = []planview.Edge{}
	n.Anchor = nil
	n.Milestones = []string{"accepted"}
	n.Acceptance = n.Acceptance[:1]
	n.Acceptance[0].Milestone = "accepted"
	n.Acceptance[0].ID = "result"
	n.Acceptance[0].Rule = "main_effective"
	for i := range n.Evidence {
		n.Evidence[i].Milestone = "accepted"
	}
	return d
}
func pvBinding(d *planview.Document, step, atom, source string) planview.Binding {
	return planview.Binding{V: 1, Kind: planview.BindingKind, ID: step + "-" + atom, Version: d.Version, Tenant: "dev", Root: root, Node: "2.1", Step: step, Acceptance: d.Items[0].Acceptance[0].ID, AcceptanceSHA256: planview.AcceptanceDigest(d.Items[0].Acceptance[0]), Atom: atom, Source: source}
}
func pvArtifact(t *testing.T, w *world, d *planview.Document, bs ...planview.Binding) string {
	t.Helper()
	d.Items[0].Bindings = []json.RawMessage{}
	for _, b := range bs {
		raw, err := json.Marshal(b)
		ok(t, err)
		d.Items[0].Bindings = append(d.Items[0].Bindings, raw)
	}
	raw, err := json.Marshal(d)
	ok(t, err)
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var e error
		id, e = store.PutArtifact(ctx, tx, "controller", "other", "application/json", raw)
		return e
	}))
	return id
}
func pvPoll(t *testing.T, w *world, src store.PlanSources) {
	t.Helper()
	_, err := store.CollectPlanObservations(ctx, w.rt, src)
	ok(t, err)
}
func pvEvaluate(t *testing.T, w *world, step, want string) {
	t.Helper()
	_, err := store.EvaluateStep(ctx, w.rt, step, nil)
	ok(t, err)
	if got := w.stepState(t, step); got != want {
		t.Fatalf("step %s = %s; want %s", step, got, want)
	}
}
func TestPV2MainFactsWithdrawAndReplay(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	var attempt string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id FROM agentrt.verification WHERE task_id=$1 AND accepts_attempt`, task).Scan(&attempt))
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "main_effective", "git")
	b.Task = task
	b.Round = 1
	b.Attempt = attempt
	b.Mission = "M-1"
	b.Result = commitA
	b.Repo = "bifrost-platform"
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	history := landing.Never
	src := store.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: history, Commit: commitB}, nil
	}}
	ctl := controller.Controller{DB: w.rt, Sources: controller.Sources{Plan: src}}
	if count, err := ctl.PlanObservations(ctx); err != nil || count < 1 {
		t.Fatalf("controller plan observations: %d %v", count, err)
	}
	pvEvaluate(t, w, s1, "planned") // accepted is not landed
	history = landing.Effective
	pvPoll(t, w, src)
	if w.stepState(t, s1) == "done" {
		t.Fatal("collector wrote step completion")
	}
	pvEvaluate(t, w, s1, "done")
	var firstSeq int64
	ok(t, w.rt.QueryRow(ctx, `SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$1`, "plan-observation:"+root).Scan(&firstSeq))
	pvPoll(t, w, src)
	var secondSeq int64
	ok(t, w.rt.QueryRow(ctx, `SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$1`, "plan-observation:"+root).Scan(&secondSeq))
	if secondSeq != firstSeq {
		t.Fatal("an unchanged answer was written again (PV2 review: one event per binding per pass)")
	}
	history = landing.Reverted
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "active")
	c := controller.Controller{DB: w.rt}
	_, err := c.Rules(ctx)
	ok(t, err)
	pvEvaluate(t, w, s1, "active")
	history = landing.Effective
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "done")
	// A failed read writes no answer and does not withdraw done: the last
	// answer stands until it is Stale (PV2 review S4); the pass records that
	// this binding was not read.
	if _, err := store.CollectPlanObservations(ctx, w.rt, store.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{}, errors.New("offline")
	}}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("a failed read was not reported: %v", err)
	}
	pvEvaluate(t, w, s1, "done")
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+root).Scan(&body))
	if body["value"] != "true" {
		t.Fatalf("a failed read replaced the answer: %v", body)
	}
	var pass map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation_pass' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "plan-observation-pass:"+root).Scan(&pass))
	// The failed read changed nothing the pass records: no new pass either.
	if fresh, _ := pass["fresh"].(map[string]any); len(fresh) != 1 {
		t.Fatalf("the pass lost the binding's last success: %v", pass)
	}
	history = landing.Effective
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "done")
	// A plan version change cannot inherit the old source observation.
	d.Version = 2
	b.Version = 2
	adoptView(t, w, pvArtifact(t, w, d, b), 1)
	pvEvaluate(t, w, s1, "active")
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "done")
}
func TestPV2OwnerAcceptanceVersionAndAuthority(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "owner_signoff")
	evidence := []string{w.artifact(t, "report", "drill evidence")}
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	pvPoll(t, w, store.PlanSources{})
	pvEvaluate(t, w, s1, "planned")
	_, err := store.SignOffStep(ctx, w.rt, "thread:pv", s1, b.SignoffRef(), evidence...)
	is(t, err, store.ErrNotOwner)
	_, err = store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	pvEvaluate(t, w, s1, "planned")
	pvPoll(t, w, store.PlanSources{})
	pvEvaluate(t, w, s1, "done")
	oldRef := b.SignoffRef()
	d.Version = 2
	b.Version = 2
	d.Items[0].Acceptance[0].Criterion = "A different acceptance"
	b.AcceptanceSHA256 = planview.AcceptanceDigest(d.Items[0].Acceptance[0])
	adoptView(t, w, pvArtifact(t, w, d, b), 1)
	pvPoll(t, w, store.PlanSources{})
	pvEvaluate(t, w, s1, "active")
	_, err = store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, oldRef, evidence...)
	is(t, err, store.ErrInvalid)
	_, err = store.SignOffStep(ctx, w.rt, store.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	pvPoll(t, w, store.PlanSources{})
	pvEvaluate(t, w, s1, "done")
}
func TestPV2BindingRejectsCrossStepAndMissingVerification(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	var attempt string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id FROM agentrt.verification WHERE task_id=$1 AND accepts_attempt`, task).Scan(&attempt))
	d := pvDocument(t, 1)
	b := pvBinding(d, s2, "review_valid", "verification")
	b.Task = task
	b.Round = 1
	b.Attempt = attempt
	b.Mission = "M-1"
	b.Repo = "bifrost-platform"
	b.Result = commitA
	b.Verification = "v_missing"
	id := pvArtifact(t, w, d, b)
	zero := 0
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "cross-step", root, "test", []store.PlanChange{{Op: "adopt_plan_view", Artifact: id, ExpectedVersion: &zero}}, (&asker{}).ask)
	if err == nil {
		t.Fatal("accepted cross-step runtime binding")
	}
	b.Step = s1
	id = pvArtifact(t, w, d, b)
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "missing-verification", root, "test", []store.PlanChange{{Op: "adopt_plan_view", Artifact: id, ExpectedVersion: &zero}}, (&asker{}).ask)
	if err == nil {
		t.Fatal("accepted dangling verification")
	}
}
func TestPV2ReviewAndActualDeployment(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	var attempt, verification string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id,verification_id FROM agentrt.verification WHERE task_id=$1 AND method='review'`, task).Scan(&attempt, &verification))
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "review_valid", "verification")
	b.Task = task
	b.Round = 1
	b.Attempt = attempt
	b.Mission = "M-1"
	b.Repo = "bifrost-platform"
	b.Result = commitA
	b.Verification = verification
	deploy := b
	deploy.ID = "deployment"
	deploy.Atom = "deployment_verified"
	deploy.Source = "deployment"
	deploy.Verification = ""
	deploy.Application = "bifrost-platform"
	deploy.Environment = "stg"
	deploy.Check = "T01"
	deploy.Spec = strings.Repeat("a", 64)
	adoptView(t, w, pvArtifact(t, w, d, b, deploy), 0)
	ref := map[string]any{"delivery_run": "run-1", "images": map[string]any{"platform-api": "sha256:" + strings.Repeat("b", 64)}}
	src := store.PlanSources{History: func(_ context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
		if snapshot != commitB {
			t.Errorf("snapshot not actual SHA: %s", snapshot)
		}
		return landing.MainHistory{State: landing.Effective, Commit: commitA}, nil
	}, Deployed: func(_ context.Context, app, env string) (string, map[string]any, bool) {
		if app != "bifrost-platform" || env != "stg" {
			t.Error("wrong deployment scope")
		}
		return commitB, ref, true
	}}
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "planned")
	w.gateVerification(t, task, attempt, "gate", "pass", "T01", "stg", commitB, deploy.Spec, ref)
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "done")
	ref["delivery_run"] = "run-2"
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "active")
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if out["freshness"] == nil {
		t.Fatal(fmt.Sprint(out))
	}
}

func TestPV2ExternalVerificationPinsSubmission(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	var attempt, verification string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id,verification_id FROM agentrt.verification WHERE task_id=$1 AND method='deterministic'`, task).Scan(&attempt, &verification))
	who := wr("thread:external")
	reg, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "external", Root: root, Node: s1, RunKey: "work630", Artifact: externalSpec(t, w), Mission: "M-1", Task: task})
	ok(t, err)
	id := reg["external_work_id"].(string)
	report := w.artifact(t, "report", "external result")
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: report, Commits: []string{commitA}})
	ok(t, err)
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "external_verified", "verification")
	b.Task = task
	b.Round = 1
	b.Attempt = attempt
	b.Mission = "M-1"
	b.Repo = "bifrost-platform"
	b.Result = commitA
	b.Verification = verification
	b.ExternalWork = id
	b.ExternalSeq = 2
	b.Report = report
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	pvEvaluate(t, w, s1, "planned")
	pvPoll(t, w, store.PlanSources{})
	pvEvaluate(t, w, s1, "done")
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	ext := out["external_work"].([]map[string]any)
	if len(ext) != 1 || ext[0]["state"] != "Submitted" || ext[0]["verification"] != "Verified" {
		t.Fatalf("external verification not derived: %v", ext)
	}
	// A registrant cannot emit the trusted observation or completion state.
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "verify", Seq: 3, Type: "verified"})
	is(t, err, store.ErrInvalid)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "relink", Seq: 3, Type: "linked", Root: root, Node: s1, Mission: "M-1", Task: task})
	ok(t, err)
	pvEvaluate(t, w, s1, "active")
}

func TestPV2DependencyReblocksActualSteps(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, e := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:2.2", Parent: root, Kind: "item", Label: "2.2", Title: "dependent", Root: root, Ordinal: 2}); e != nil {
			return e
		}
		_, e := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:2.2-1", Parent: "R2:2.2", Kind: "step", StepKind: "agent", Label: "2.2-1", Title: "dependent step", Root: root})
		return e
	}))
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "main_effective", "git")
	b.Repo = "bifrost-platform"
	b.Result = commitA
	second := d.Items[0]
	second.Node = "2.2"
	second.Title = "dependent"
	second.Ordinal = 2
	order := 2
	second.Order = &order
	second.Dependencies = []planview.Edge{{Root: root, Node: "2.1", Milestone: "accepted", RequiredFor: "accepted", Relation: "blocks", Reason: "needs main", Sources: []string{"work"}}}
	dep := b
	dep.ID = "dependent"
	dep.Node = "2.2"
	dep.Step = "R2:2.2-1"
	dep.Result = commitB
	raw, _ := json.Marshal(dep)
	second.Bindings = []json.RawMessage{raw}
	d.Items = append(d.Items, second)
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	history := landing.Effective
	src := store.PlanSources{History: func(_ context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
		if result == commitA {
			return landing.MainHistory{State: history, Commit: commitA}, nil
		}
		return landing.MainHistory{State: landing.Effective, Commit: commitB}, nil
	}}
	pvPoll(t, w, src)
	pvEvaluate(t, w, s1, "done")
	pvEvaluate(t, w, dep.Step, "done")
	history = landing.Reverted
	pvPoll(t, w, src)
	// Read model and downstream step react before the prerequisite step's own
	// write. No dependence on iteration order or cached node states.
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if len(out["waiting_on"].([]map[string]any)) != 1 {
		t.Fatalf("dependency not blocked: %v", out["waiting_on"])
	}
	pvEvaluate(t, w, dep.Step, "active")
	pvEvaluate(t, w, s1, "active")
}

func TestPV2LandingNoteAdapter(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	var attempt string
	ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id FROM agentrt.verification WHERE task_id=$1 AND accepts_attempt`, task).Scan(&attempt))
	op := "land:" + task + ":1"
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "Claim observed", "landing": map[string]any{"v": "1.1", "kind": "claimed", "op": op, "task": task, "attempt": attempt, "A": commitA, "rule_version": 1}}})
		return e
	}))
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	tasks := out["tasks"].([]map[string]any)
	found := false
	for _, x := range tasks {
		if x["task_id"] == task {
			l, has := x["landing"].(map[string]any)
			found = has && l["phase"] == "claimed" && l["operation"] == op
		}
	}
	if !found {
		t.Fatal("note.landing was not folded into task detail")
	}
	if !strings.Contains(string(out["recent_changes"].(json.RawMessage)), op) {
		t.Fatal("landing operation absent from recent changes")
	}
	if w.stepState(t, s1) == "done" {
		t.Fatal("landing lifecycle was treated as completion proof")
	}
}

// PV2 review 2026-10-26 S2: a card-auto-answer rule that answers plan changes
// does not adopt a view binding completion atoms; the same view without
// bindings it does (the control).
func TestPV2RuleDoesNotAdoptBindings(t *testing.T) {
	t.Parallel()
	for _, binds := range []bool{true, false} {
		w := newKernelWorld(t)
		d := pvDocument(t, 1)
		var artifact string
		if binds {
			b := pvBinding(d, s1, "external_verified", "owner_signoff")
			artifact = pvArtifact(t, w, d, b)
		} else {
			artifact = pvArtifact(t, w, d)
		}
		signCardAuto(t, w, "30", "[plan_change]")
		var s cardStore
		zero := 0
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:pv"), "adopt", root, "adopt the view", []store.PlanChange{{Op: "adopt_plan_view", Artifact: artifact, ExpectedVersion: &zero}}, s.ask)
		ok(t, err)
		s.aged(s.order[0], time.Hour)
		n, err := store.AnswerByRule(ctx, w.rt, s.rule())
		ok(t, err)
		if want := map[bool]int{true: 0, false: 1}[binds]; n != want {
			t.Fatalf("binds %v: the rule answered %d cards; want %d", binds, n, want)
		}
	}
}
