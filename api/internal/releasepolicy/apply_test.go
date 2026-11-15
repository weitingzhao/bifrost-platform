package releasepolicy_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// fakePlans is the plan source the policy's apply section reads.
type fakePlans map[string]releasepolicy.ApplyPlan

func (p fakePlans) ApplyPlan(_ context.Context, id string) (releasepolicy.ApplyPlan, error) {
	plan, ok := p[id]
	if !ok {
		return releasepolicy.ApplyPlan{}, errors.New("not found")
	}
	return plan, nil
}

const applyPath = "k8s/cicd/tekton/pipeline-ci.yaml"

// liveSource adapts the fake cluster to the engine's Live.
type liveSource struct{ l *rptest.Live }

func (s liveSource) Managers(_ context.Context, namespace, kind, name string) ([]releasepolicy.FieldManager, bool, error) {
	list, found, err := s.l.Read(namespace, kind, name)
	out := make([]releasepolicy.FieldManager, 0, len(list))
	for _, m := range list {
		out = append(out, releasepolicy.FieldManager{Manager: m.Manager, Operation: m.Operation})
	}
	return out, found, err
}

// applied is the managedFields of an object the applier applied; a kubectl
// edit beside it changes nothing.
var applied = []rptest.LiveManager{{Manager: "kubectl-edit", Operation: "Update"}, {Manager: "the-applier", Operation: "Apply"}}

// applySection is the apply section the Owner's design names, with invented paths.
func applySection() map[string]any {
	return map[string]any{
		"namespaces": []string{"cicd"},
		"kinds":      []string{"Pipeline", "Task"},
		"exclude_paths": []string{
			"k8s/cicd/release-policy/**", "k8s/cicd/tekton/task-release-window.yaml",
			"k8s/cicd/tekton/apply-manifest/**", "k8s/cicd/tekton/pipeline-deliver-prod.yaml",
		},
		"exclude_objects":             []string{"Pipeline/deliver-prod", "Task/release-window"},
		"field_manager":               "the-applier",
		"require_previous_generation": true,
	}
}

type applyFixture struct {
	*fixture
	plans fakePlans
	live  *rptest.Live
}

// newApplyFixture: the fixture's policy with an apply section, and one plan
// of one Pipeline in cicd that the applier applied before.
func newApplyFixture(t *testing.T, edit func(doc map[string]any)) *applyFixture {
	t.Helper()
	f := &applyFixture{fixture: newFixture(t), plans: fakePlans{}, live: &rptest.Live{
		Objects: map[string][]rptest.LiveManager{"cicd/Pipeline/ci": applied}, Err: map[string]error{},
	}}
	f.signPolicy(t, f.key, rptest.PolicyTextWith("rp-apply", now.Add(-time.Hour), 90, func(doc map[string]any) {
		doc["apply"] = applySection()
		if edit != nil {
			edit(doc)
		}
	}, pipeline))
	f.plans["plan-1"] = releasepolicy.ApplyPlan{
		Ready: true, Policy: "pass", Repo: "repo-ops", Path: applyPath, Commit: rptest.SHA("ops"),
		Objects: []releasepolicy.ApplyObject{{Namespace: "cicd", Kind: "Pipeline", Name: "ci", Ownership: "ssa"}},
	}
	f.eng = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: rptest.Writer{CMs: f.cms}, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		Plans: f.plans, Live: liveSource{f.live}, Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock },
	})
	return f
}

func (f *applyFixture) apply() releasepolicy.Decision {
	return f.eng.Decide(context.Background(), "apply_manifest", actions.TierC, map[string]any{"plan_id": "plan-1"})
}

func (f *applyFixture) editPlan(edit func(p *releasepolicy.ApplyPlan)) {
	p := f.plans["plan-1"]
	p.Objects = append([]releasepolicy.ApplyObject(nil), p.Objects...)
	edit(&p)
	f.plans["plan-1"] = p
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. The positive control of
// every case below: an apply of an object the applier applied before, of an
// allowed kind in an allowed namespace, from a path that is not excluded.
func TestApplyOfAPreviousGenerationIsCovered(t *testing.T) {
	f := newApplyFixture(t, nil)
	d := f.apply()
	wantAuto(t, d)
	if d.PolicyID != "rp-apply" || d.Plan != "plan-1" || d.Path != "repo-ops:"+applyPath || d.SHAText() != "repo-ops="+rptest.SHA("ops") {
		t.Fatalf("decision = %+v", d)
	}
	if got, want := d.ClauseText(), "signature,unexpired,not_frozen,plan_passed,apply_namespaces,apply_kinds,apply_objects,apply_paths,previous_generation"; got != want {
		t.Fatalf("clauses = %s, want %s", got, want)
	}
	// Two objects, both applied before: covered, each read once.
	f.editPlan(func(p *releasepolicy.ApplyPlan) {
		p.Objects = append(p.Objects, releasepolicy.ApplyObject{Namespace: "cicd", Kind: "Task", Name: "t", Ownership: "ssa"})
	})
	f.live.Objects["cicd/Task/t"] = applied
	f.live.Asked = nil
	wantAuto(t, f.apply())
	if got := strings.Join(f.live.Asked, " "); got != "cicd/Pipeline/ci cicd/Task/t" {
		t.Fatalf("read from the cluster: %s", got)
	}
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. Each case is the covered
// apply with one thing wrong.
func TestApplyWithOneThingWrongWaits(t *testing.T) {
	cases := map[string]struct {
		policy func(doc map[string]any)
		plan   func(p *releasepolicy.ApplyPlan)
		world  func(f *applyFixture)
		want   string
	}{
		"wrong namespace": {plan: func(p *releasepolicy.ApplyPlan) { p.Objects[0].Namespace = "data" }, want: "outside the policy's apply namespaces and kinds: data/Pipeline/ci"},
		"wrong kind":      {plan: func(p *releasepolicy.ApplyPlan) { p.Objects[0].Kind = "ConfigMap" }, want: "outside the policy's apply namespaces and kinds"},
		"one object of two outside": {plan: func(p *releasepolicy.ApplyPlan) {
			p.Objects = append(p.Objects, releasepolicy.ApplyObject{Namespace: "cicd", Kind: "Role", Name: "r", Ownership: "ssa"})
		}, want: "cicd/Role/r"},
		// The hole a path list leaves: another file can render an object of
		// the same name. An excluded object is excluded whatever renders it.
		"an excluded object rendered from a path that is not excluded": {plan: func(p *releasepolicy.ApplyPlan) {
			p.Objects[0].Name = "deliver-prod"
		}, world: func(f *applyFixture) { f.live.Objects["cicd/Pipeline/deliver-prod"] = applied }, want: "excluded by the policy whatever renders it: cicd/Pipeline/deliver-prod"},
		"an excluded object among others": {plan: func(p *releasepolicy.ApplyPlan) {
			p.Objects = append(p.Objects, releasepolicy.ApplyObject{Namespace: "cicd", Kind: "Task", Name: "release-window", Ownership: "ssa"})
		}, world: func(f *applyFixture) { f.live.Objects["cicd/Task/release-window"] = applied }, want: "cicd/Task/release-window"},
		"excluded file":               {plan: func(p *releasepolicy.ApplyPlan) { p.Path = "k8s/cicd/tekton/pipeline-deliver-prod.yaml" }, want: "is excluded by the policy"},
		"under an excluded directory": {plan: func(p *releasepolicy.ApplyPlan) { p.Path = "k8s/cicd/tekton/apply-manifest/pipeline.yaml" }, want: "is excluded by the policy"},
		// A directory is rendered whole: one that holds an excluded file is excluded.
		"a directory that holds an excluded file": {plan: func(p *releasepolicy.ApplyPlan) { p.Path = "k8s/cicd/tekton" }, want: "is excluded by the policy"},
		"the excluded directory itself":           {plan: func(p *releasepolicy.ApplyPlan) { p.Path = "k8s/cicd/release-policy" }, want: "is excluded by the policy"},
		"no path":                                 {plan: func(p *releasepolicy.ApplyPlan) { p.Path = "" }, want: "is excluded by the policy"},
		// A new object plans as "ssa" as well; the cluster's own copy tells
		// the two apart.
		"missing object: a first creation": {world: func(f *applyFixture) { delete(f.live.Objects, "cicd/Pipeline/ci") },
			want: "no previous generation to go back to: cicd/Pipeline/ci (does not exist in the cluster"},
		"existing, but only kubectl manages it": {world: func(f *applyFixture) {
			f.live.Objects["cicd/Pipeline/ci"] = []rptest.LiveManager{{Manager: "kubectl-client-side-apply", Operation: "Update"}, {Manager: "kubectl", Operation: "Apply"}}
		}, want: "cicd/Pipeline/ci (exists, but the-applier has not applied it)"},
		"the applier's manager, but an Update, not an Apply": {world: func(f *applyFixture) {
			f.live.Objects["cicd/Pipeline/ci"] = []rptest.LiveManager{{Manager: "the-applier", Operation: "Update"}}
		}, want: "the-applier has not applied it"},
		"existing with no managed fields": {world: func(f *applyFixture) { f.live.Objects["cicd/Pipeline/ci"] = nil }, want: "has not applied it"},
		"read error":                      {world: func(f *applyFixture) { f.live.Err["cicd/Pipeline/ci"] = errors.New("forbidden") }, want: "cicd/Pipeline/ci (cannot be read from the cluster: forbidden)"},
		"one object of several is new": {plan: func(p *releasepolicy.ApplyPlan) {
			p.Objects = append(p.Objects, releasepolicy.ApplyObject{Namespace: "cicd", Kind: "Task", Name: "new", Ownership: "ssa"})
		}, want: "no previous generation to go back to: cicd/Task/new (does not exist"},
		"more objects than are checked": {plan: func(p *releasepolicy.ApplyPlan) {
			for len(p.Objects) < 51 {
				p.Objects = append(p.Objects, p.Objects[0])
			}
		}, want: "too many to check"},
		"the section names no field manager": {policy: func(doc map[string]any) {
			delete(doc["apply"].(map[string]any), "field_manager")
		}, want: "names no field_manager, so it covers nothing"},
		"ownership take-over":       {plan: func(p *releasepolicy.ApplyPlan) { p.Objects[0].Ownership = "migrate" }, want: `(ownership "migrate")`},
		"ownership not stated":      {plan: func(p *releasepolicy.ApplyPlan) { p.Objects[0].Ownership = "" }, want: "no previous generation to go back to"},
		"plan not ready":            {plan: func(p *releasepolicy.ApplyPlan) { p.Ready = false }, want: "not a finished plan"},
		"plan's check did not pass": {plan: func(p *releasepolicy.ApplyPlan) { p.Policy = "fail" }, want: "not a finished plan"},
		"plan with no objects":      {plan: func(p *releasepolicy.ApplyPlan) { p.Objects = nil }, want: "lists no objects"},
		"plan unreadable":           {world: func(f *applyFixture) { delete(f.plans, "plan-1") }, want: "cannot read plan plan-1"},
		"frozen": {world: func(f *applyFixture) {
			f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:30:00Z", "who": "a", "reason": "b"})
		}, want: "releases are frozen"},
		"expired": {world: func(f *applyFixture) { f.clock = now.Add(91 * 24 * time.Hour) }, want: "expired at"},
		"the section does not require a previous generation": {policy: func(doc map[string]any) {
			doc["apply"].(map[string]any)["require_previous_generation"] = false
		}, want: "covers nothing"},
		"the section names no namespaces": {policy: func(doc map[string]any) {
			delete(doc["apply"].(map[string]any), "namespaces")
		}, want: "outside the policy's apply namespaces and kinds"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t, tc.policy)
			if tc.plan != nil {
				f.editPlan(tc.plan)
			}
			if tc.world != nil {
				tc.world(f)
			}
			d := f.apply()
			wantWait(t, d, tc.want)
			if d.PolicyID != "rp-apply" {
				t.Fatalf("a policy that judges applies names itself, covered or not: %+v", d)
			}
		})
	}
	t.Run("tier D", func(t *testing.T) {
		f := newApplyFixture(t, nil)
		wantWait(t, f.eng.Decide(context.Background(), "apply_manifest", actions.TierD, map[string]any{"plan_id": "plan-1"}), "tier D is never auto-approved")
	})
	t.Run("no plan reader", func(t *testing.T) {
		f := newApplyFixture(t, nil)
		f.eng = f.engine(f.key.Fingerprint)
		wantWait(t, f.apply(), "no reader for plans or for the cluster's objects")
	})
	// Positive control of the cap: fifty objects, all applied before, are covered.
	t.Run("as many objects as are checked", func(t *testing.T) {
		f := newApplyFixture(t, nil)
		f.editPlan(func(p *releasepolicy.ApplyPlan) {
			for len(p.Objects) < 50 {
				p.Objects = append(p.Objects, p.Objects[0])
			}
		})
		wantAuto(t, f.apply())
	})
	// An object outside the section is refused without a read of the cluster.
	t.Run("an object outside the section is not read", func(t *testing.T) {
		f := newApplyFixture(t, nil)
		f.editPlan(func(p *releasepolicy.ApplyPlan) { p.Objects[0].Kind = "Secret" })
		wantWait(t, f.apply(), "outside the policy's apply namespaces and kinds")
		if len(f.live.Asked) != 0 {
			t.Fatalf("read from the cluster: %v", f.live.Asked)
		}
	})
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. The policy signed before
// the section existed answers an apply exactly as it answers any action that
// is not a release, and names no policy: the server keeps the card.
func TestAPolicyWithoutAnApplySectionCoversNoApply(t *testing.T) {
	f := newApplyFixture(t, func(doc map[string]any) { delete(doc, "apply") })
	got := f.apply()
	want := releasepolicy.Decision{SHAs: map[string]string{}, Reasons: []string{"apply_manifest is not a release; the policy covers start_pipeline_run only"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decision = %+v\nwant      %+v", got, want)
	}
	// The same words every other action gets, today as before.
	other := f.eng.Decide(context.Background(), "cordon_node", actions.TierC, map[string]any{"name": "n"})
	if len(other.Reasons) != 1 || other.Reasons[0] != "cordon_node is not a release; the policy covers start_pipeline_run only" {
		t.Fatalf("another action: %+v", other)
	}
}
