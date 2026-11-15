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

// fakePlans, fakeHistory and fakeReviews are the three sources the policy's
// newer sections read.
type fakePlans map[string]releasepolicy.ApplyPlan

func (p fakePlans) ApplyPlan(_ context.Context, id string) (releasepolicy.ApplyPlan, error) {
	plan, ok := p[id]
	if !ok {
		return releasepolicy.ApplyPlan{}, errors.New("not found")
	}
	return plan, nil
}

type fakeHistory struct {
	commits []releasepolicy.Commit
	err     error
}

func (h *fakeHistory) Commits(context.Context, string, string, string) ([]releasepolicy.Commit, error) {
	return h.commits, h.err
}

// fakeReviews is keyed "repo@commit".
type fakeReviews struct {
	rows map[string][]releasepolicy.Review
	err  error
}

func (r *fakeReviews) Reviews(_ context.Context, repo, commit string) ([]releasepolicy.Review, error) {
	return r.rows[repo+"@"+commit], r.err
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

const (
	anchorFile = "internal/releasepolicy/anchor.go"
	author     = "local_author"
	reviewer   = "local_reviewer"
)

type reviewFixture struct {
	*fixture
	history *fakeHistory
	reviews *fakeReviews
}

func reviewedPolicy(doc map[string]any) {
	doc["conditions"].(map[string]any)["trust_anchor_reviewed"] = true
	doc["reviewable_repos"] = []string{"repo-app"}
}

// newReviewFixture: a release of repo-app whose one commit, written by
// author, changes a trust anchor, and a passed review of exactly the new
// commit by reviewer that lists that file.
func newReviewFixture(t *testing.T, edit func(doc map[string]any)) *reviewFixture {
	t.Helper()
	f := &reviewFixture{fixture: newFixture(t)}
	if edit == nil {
		edit = reviewedPolicy
	}
	f.signPolicy(t, f.key, rptest.PolicyTextWith("rp-review", now.Add(-time.Hour), 90, edit, pipeline))
	f.facts.Files["repo-app"] = []string{"docs/readme.md", anchorFile}
	f.history = &fakeHistory{commits: []releasepolicy.Commit{
		{SHA: rptest.SHA("new"), Message: "feat: anchor\n\nbody\n\nClaude-Session: " + author + "\nWork: X-1\n", Files: []string{anchorFile}},
		{SHA: rptest.SHA("mid"), Message: "docs: readme\n", Files: []string{"docs/readme.md"}},
	}}
	f.reviews = &fakeReviews{rows: map[string][]releasepolicy.Review{
		"repo-app@" + rptest.SHA("new"): {{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true}},
	}}
	f.eng = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: rptest.Writer{CMs: f.cms}, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		History: f.history, Reviews: f.reviews, Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock },
	})
	return f
}

func (f *reviewFixture) setReviews(rows ...releasepolicy.Review) {
	f.reviews.rows["repo-app@"+rptest.SHA("new")] = rows
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. The positive control of
// every case below: a trust-anchor change read whole, at the released commit,
// by a thread that did not write it.
func TestATrustAnchorChangeWithAnIndependentReviewIsCovered(t *testing.T) {
	f := newReviewFixture(t, nil)
	d := f.decide(nil)
	wantAuto(t, d)
	if got, want := d.ClauseText(), "signature,unexpired,not_frozen,allow:"+pipeline+
		",window_held_by_requester,revision,ci_succeeded,no_ddl,no_d10_paths,trust_anchor_reviewed"; got != want {
		t.Fatalf("clauses = %s, want %s", got, want)
	}
	if want := []string{"repo-app@" + rptest.SHA("new")[:12] + " by " + reviewer}; !reflect.DeepEqual(d.ReviewedAnchors, want) {
		t.Fatalf("reviewed = %q, want %q", d.ReviewedAnchors, want)
	}
	// A reviewer who failed it and then passed it has passed it.
	f.setReviews(releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}}, releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true})
	wantAuto(t, f.decide(nil))
	// A review that read more than the trust anchors covers them.
	f.setReviews(releasepolicy.Review{Reviewer: reviewer, Files: []string{"docs/readme.md", anchorFile, "x.go"}, Passed: true})
	wantAuto(t, f.decide(nil))
	// An author's own review beside an independent one changes nothing.
	f.setReviews(releasepolicy.Review{Reviewer: author, Files: []string{anchorFile}, Passed: true}, releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true})
	wantAuto(t, f.decide(nil))
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. Each case is the covered
// release with one thing wrong.
func TestATrustAnchorReviewWithOneThingWrongWaits(t *testing.T) {
	second := "internal/releasepolicy/engine.go"
	cases := map[string]struct {
		policy func(doc map[string]any)
		world  func(f *reviewFixture)
		want   string
	}{
		"no review": {world: func(f *reviewFixture) { f.setReviews() }, want: "no review of " + rptest.SHA("new")[:12] + " covers: " + anchorFile},
		"review of another commit": {world: func(f *reviewFixture) {
			f.reviews.rows = map[string][]releasepolicy.Review{"repo-app@" + rptest.SHA("mid"): {{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true}}}
		}, want: "no review of " + rptest.SHA("new")[:12] + " covers"},
		"review of another repo": {world: func(f *reviewFixture) {
			f.reviews.rows = map[string][]releasepolicy.Review{"repo-lib@" + rptest.SHA("new"): {{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true}}}
		}, want: "no review of"},
		"review by an author": {world: func(f *reviewFixture) {
			f.setReviews(releasepolicy.Review{Reviewer: author, Files: []string{anchorFile}, Passed: true})
		}, want: "the reviewer " + author + " is an author"},
		"review by the second author of two": {world: func(f *reviewFixture) {
			f.history.commits = append(f.history.commits, releasepolicy.Commit{SHA: rptest.SHA("co"), Message: "fix\n\nClaude-Session: local_other\n", Files: []string{anchorFile}})
			f.setReviews(releasepolicy.Review{Reviewer: "local_other", Files: []string{anchorFile}, Passed: true})
		}, want: "the reviewer local_other is an author"},
		// Every value of the trailer is an author, not the first alone.
		"review by a second session of one commit": {world: func(f *reviewFixture) {
			f.history.commits[0].Message += "Claude-Session: " + reviewer + "\n"
		}, want: "the reviewer " + reviewer + " is an author"},
		"review missing a file": {world: func(f *reviewFixture) {
			f.facts.Files["repo-app"] = []string{anchorFile, second}
			f.history.commits[0].Files = []string{anchorFile, second}
		}, want: "no review of " + rptest.SHA("new")[:12] + " covers: " + second},
		"failed verdict": {world: func(f *reviewFixture) {
			f.setReviews(releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}})
		}, want: "by " + reviewer + " failed"},
		"passed, then failed by the same reviewer": {world: func(f *reviewFixture) {
			f.setReviews(releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true}, releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}})
		}, want: "failed"},
		"passed by one, failed by another": {world: func(f *reviewFixture) {
			f.setReviews(releasepolicy.Review{Reviewer: reviewer, Files: []string{anchorFile}, Passed: true}, releasepolicy.Review{Reviewer: "local_third", Files: []string{anchorFile}})
		}, want: "by local_third failed"},
		"commit without trailer": {world: func(f *reviewFixture) {
			f.history.commits[0].Message = "feat: anchor\n\nCo-Authored-By: someone\n"
		}, want: "has no Claude-Session trailer"},
		"a changed anchor no listed commit names": {world: func(f *reviewFixture) {
			f.history.commits[0].Files = []string{"other.go"}
		}, want: "its author is unknown"},
		// The commits' lists may be cut short at this size: every commit of
		// the range then counts as an author, the one without a trailer too.
		"a diff large enough to be cut short, one commit without a trailer": {world: func(f *reviewFixture) {
			f.facts.Files["repo-app"] = append(manyFiles(100), anchorFile)
			f.facts.TreeFiles = map[string][]string{"repo-app": append(manyFiles(100), anchorFile)}
		}, want: "commit " + rptest.SHA("mid")[:12] + " touches a trust anchor and has no Claude-Session trailer"},
		"commits unreadable":        {world: func(f *reviewFixture) { f.history.err = errors.New("status 500") }, want: "cannot read the commits"},
		"review records unreadable": {world: func(f *reviewFixture) { f.reviews.err = errors.New("no ledger") }, want: "cannot read the review records"},
		"repo not reviewable": {policy: func(doc map[string]any) {
			reviewedPolicy(doc)
			doc["reviewable_repos"] = []string{"repo-lib"}
		}, want: "hits no_trust_anchor_change: " + anchorFile},
		"condition off": {policy: func(doc map[string]any) {
			reviewedPolicy(doc)
			doc["conditions"].(map[string]any)["trust_anchor_reviewed"] = false
		}, want: "hits no_trust_anchor_change: " + anchorFile},
		// A test file under a trust-anchor path is a trust-anchor change
		// unless the policy says tests are left out.
		"a changed test file the review did not read": {world: func(f *reviewFixture) {
			f.facts.Files["repo-app"] = []string{anchorFile, "internal/releasepolicy/anchor_test.go"}
			f.history.commits[0].Files = []string{anchorFile, "internal/releasepolicy/anchor_test.go"}
		}, want: "covers: internal/releasepolicy/anchor_test.go"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newReviewFixture(t, tc.policy)
			if tc.world != nil {
				tc.world(f)
			}
			d := f.decide(nil)
			wantWait(t, d, tc.want)
			if len(d.ReviewedAnchors) != 0 || strings.Contains(d.ClauseText(), "trust_anchor_reviewed") {
				t.Fatalf("a refused review is named as passed: %+v", d)
			}
		})
	}
	t.Run("no readers", func(t *testing.T) {
		f := newReviewFixture(t, nil)
		f.eng = f.engine(f.key.Fingerprint)
		wantWait(t, f.decide(nil), "no reader for commit messages or review records")
	})
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. The policy signed before
// the condition existed refuses a trust-anchor change in the words it always
// used, whatever review is on record.
func TestAPolicyWithoutTheReviewConditionRefusesATrustAnchorChange(t *testing.T) {
	f := newReviewFixture(t, func(map[string]any) {})
	got := f.decide(nil)
	// The same request on an engine with no review sources at all.
	plain := newFixture(t)
	plain.signPolicy(t, plain.key, rptest.PolicyText("rp-review", now.Add(-time.Hour), 90, pipeline))
	plain.facts.Files["repo-app"] = []string{"docs/readme.md", anchorFile}
	want := plain.decide(nil)
	if got.Auto || !reflect.DeepEqual(got, want) {
		t.Fatalf("decision = %+v\nwant      %+v", got, want)
	}
	wantWait(t, got, "repo-app "+rptest.SHA("old")[:12]+".."+rptest.SHA("new")[:12]+" hits no_trust_anchor_change: "+anchorFile)
}

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. Test files are left out
// of what a review must cover only when the policy says so.
func TestTrustAnchorTestFilesAreIgnoredOnlyWhenThePolicySaysSo(t *testing.T) {
	testFiles := []string{"internal/releasepolicy/anchor_test.go", "internal/releasepolicy/testdata/key.pub"}
	ignoring := func(doc map[string]any) {
		reviewedPolicy(doc)
		doc["conditions"].(map[string]any)["trust_anchor_ignore_tests"] = true
	}
	world := func(f *reviewFixture, files ...string) {
		f.facts.Files["repo-app"] = files
		// A commit with no trailer wrote the test files.
		f.history.commits = append(f.history.commits, releasepolicy.Commit{SHA: rptest.SHA("tst"), Message: "test\n", Files: testFiles})
	}
	t.Run("ignored: only test files changed, no review needed", func(t *testing.T) {
		f := newReviewFixture(t, ignoring)
		world(f, testFiles...)
		f.setReviews()
		d := f.decide(nil)
		wantAuto(t, d)
		if !strings.HasSuffix(d.ClauseText(), ",no_trust_anchor_change") {
			t.Fatalf("clauses = %s", d.ClauseText())
		}
	})
	t.Run("ignored: the review covers the other file", func(t *testing.T) {
		f := newReviewFixture(t, ignoring)
		world(f, append([]string{anchorFile}, testFiles...)...)
		wantAuto(t, f.decide(nil))
	})
	t.Run("not ignored by default", func(t *testing.T) {
		f := newReviewFixture(t, nil)
		world(f, testFiles...)
		wantWait(t, f.decide(nil), "has no Claude-Session trailer")
	})
	t.Run("ignoring does nothing where no review is allowed", func(t *testing.T) {
		f := newReviewFixture(t, func(doc map[string]any) {
			doc["conditions"].(map[string]any)["trust_anchor_ignore_tests"] = true
		})
		world(f, testFiles...)
		wantWait(t, f.decide(nil), "hits no_trust_anchor_change")
	})
}

func TestSessionTrailers(t *testing.T) {
	got := releasepolicy.SessionTrailers("subject\r\n\r\nClaude-Session: local_a \r\nclaude-session:local_b\nNot-Claude-Session: x\n Claude-Session: indented\n")
	if want := []string{"local_a", "local_b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trailers = %q, want %q", got, want)
	}
}
