package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
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
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var e error
		id, e = artifact.PutArtifact(ctx, tx, "controller", "other", "application/json", raw)
		return e
	}))
	return id
}
func pvPoll(t *testing.T, w *world, src planstore.PlanSources) {
	t.Helper()
	_, err := planstore.CollectPlanObservations(ctx, w.rt, src)
	ok(t, err)
}
func pvEvaluate(t *testing.T, w *world, step, want string) {
	t.Helper()
	_, err := planstore.EvaluateStep(ctx, w.rt, step, nil)
	ok(t, err)
	if got := w.stepState(t, step); got != want {
		t.Fatalf("step %s = %s; want %s", step, got, want)
	}
}
func TestPV2MainFactsWithdrawAndReplay(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	d := pvDocument(t, 1)
	b := pvBinding(d, s1, "main_effective", "git")
	b.Result = commitA
	b.Repo = "bifrost-platform"
	adoptView(t, w, pvArtifact(t, w, d, b), 0)
	history := landing.Never
	src := planstore.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: history, Commit: commitB}, nil
	}}
	ctl := controller.Controller{DB: w.rt, Sources: controller.Sources{Plan: src}}
	if count, err := ctl.PlanObservations(ctx); err != nil || count < 1 {
		t.Fatalf("controller plan observations: %d %v", count, err)
	}
	pvEvaluate(t, w, s1, "planned") // not landed
	history = landing.Effective
	pvPoll(t, w, src)
	if w.stepState(t, s1) == "done" {
		t.Fatal("collector wrote step completion")
	}
	pvEvaluate(t, w, s1, "done")
	var firstSeq int64
	ok(t, w.rt.QueryRow(ctx, `SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$1`, "plan-observation:"+root).Scan(&firstSeq))
	// Origin: 2026-11-18, TD-582: Recent changes is two reads now, and a changed observation is the plan's own part of it.
	listed, listErr := planstore.PlanView(ctx, w.rt, root)
	ok(t, listErr)
	if !strings.Contains(string(listed["recent_changes"].(json.RawMessage)), `"plan-observation:`+root+`"`) {
		t.Fatalf("a changed observation is not in Recent changes: %s", listed["recent_changes"])
	}
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
	if _, err := planstore.CollectPlanObservations(ctx, w.rt, planstore.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
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
	pvPoll(t, w, planstore.PlanSources{})
	pvEvaluate(t, w, s1, "planned")
	_, err := planstore.SignOffStep(ctx, w.rt, "thread:pv", s1, b.SignoffRef(), evidence...)
	is(t, err, planstore.ErrNotOwner)
	_, err = planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	pvEvaluate(t, w, s1, "planned")
	pvPoll(t, w, planstore.PlanSources{})
	pvEvaluate(t, w, s1, "done")
	oldRef := b.SignoffRef()
	d.Version = 2
	b.Version = 2
	d.Items[0].Acceptance[0].Criterion = "A different acceptance"
	b.AcceptanceSHA256 = planview.AcceptanceDigest(d.Items[0].Acceptance[0])
	adoptView(t, w, pvArtifact(t, w, d, b), 1)
	pvPoll(t, w, planstore.PlanSources{})
	pvEvaluate(t, w, s1, "active")
	_, err = planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, oldRef, evidence...)
	is(t, err, eventlog.ErrInvalid)
	_, err = planstore.SignOffStep(ctx, w.rt, planstore.OwnerActor, s1, b.SignoffRef(), evidence...)
	ok(t, err)
	pvPoll(t, w, planstore.PlanSources{})
	pvEvaluate(t, w, s1, "done")
}

// The coordinator, 2026-11-14 (store cut 4's first half): the binding sources
// that rest on the dispatch lane's records are retired. A view document with
// a verification binding, a deployment binding, or a git binding that names
// a task is not adopted; one that names no task still is.
func TestPV2RetiredSourcesAreNotAdopted(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	zero := 0
	adopt := func(key string, d *planview.Document, b planview.Binding) error {
		_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:pv"), key, root, "test",
			[]planstore.PlanChange{{Op: "adopt_plan_view", Artifact: pvArtifact(t, w, d, b), ExpectedVersion: &zero}}, (&asker{}).ask)
		return err
	}
	runtime := func(b planview.Binding) planview.Binding {
		b.Task, b.Round, b.Attempt, b.Mission = "t_01M5192P0JPMJAP63ZKRPXJ2VV", 1, "a_01M5192P0JPMJAP63ZKRPXJ2VV", "M-1"
		b.Repo, b.Result = "bifrost-platform", commitA
		return b
	}
	d := pvDocument(t, 1)
	review := runtime(pvBinding(d, s1, "review_valid", "verification"))
	review.Verification = "v_any"
	deploy := runtime(pvBinding(d, s1, "deployment_verified", "deployment"))
	deploy.Application, deploy.Environment, deploy.Check, deploy.Spec = "bifrost-platform", "stg", "health", strings.Repeat("ab", 32)
	for name, b := range map[string]planview.Binding{
		"verification":    review,
		"deployment":      deploy,
		"git with a task": runtime(pvBinding(d, s1, "main_effective", "git")),
	} {
		if !b.Retired() {
			t.Fatalf("%s: not told as retired", name)
		}
		if err := adopt("retired-"+name, pvDocument(t, 1), b); !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(err.Error(), "retired") {
			t.Errorf("%s: adopted or refused for another reason: %v", name, err)
		}
	}
	plain := pvBinding(d, s2, "main_effective", "git")
	plain.Repo, plain.Result = "bifrost-platform", commitA
	if plain.Retired() {
		t.Fatal("a git binding without a task is told as retired")
	}
	// The control: the same document with a binding that is not retired is adopted.
	if err := adopt("plain", pvDocument(t, 1), plain); err != nil {
		t.Fatalf("a git binding without a task was not adopted: %v", err)
	}
}
func TestPV2DependencyReblocksActualSteps(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, e := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "R2:2.2", Parent: root, Kind: "item", Label: "2.2", Title: "dependent", Root: root, Ordinal: 2}); e != nil {
			return e
		}
		_, e := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "R2:2.2-1", Parent: "R2:2.2", Kind: "step", StepKind: "agent", Label: "2.2-1", Title: "dependent step", Root: root})
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
	src := planstore.PlanSources{History: func(_ context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
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
	out, err := planstore.PlanView(ctx, w.rt, root)
	ok(t, err)
	if len(out["waiting_on"].([]map[string]any)) != 1 {
		t.Fatalf("dependency not blocked: %v", out["waiting_on"])
	}
	pvEvaluate(t, w, dep.Step, "active")
	pvEvaluate(t, w, s1, "active")
}
