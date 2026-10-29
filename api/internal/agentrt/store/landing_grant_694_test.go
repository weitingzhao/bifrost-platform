package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Regressions of code review 694 that need the database (W-31 core C-5).

var pipelineRuns = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}

// greenGrantRun is c5Wait's pinned run as Tekton has it: R's revision,
// the platform pipeline, success, and the key's results.
func greenGrantRun(t *testing.T, c *c5) *unstructured.Unstructured {
	t.Helper()
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": c.ci.Run, "namespace": "cicd", "labels": map[string]any{"bifrost.io/repo": "bifrost-platform"}},
		"spec": map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-platform"},
			"params": []any{map[string]any{"name": "revision", "value": commitB}}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
			"results": []any{map[string]any{"name": "platform-commit", "value": commitB}, map[string]any{"name": "ui-commit", "value": c.key.Siblings["bifrost-ui"]},
				map[string]any{"name": "infra-commit", "value": c.key.Siblings["bifrost-trade-infra"]}}},
	}}
	o.SetUID(types.UID(c.ci.UID))
	o.SetCreationTimestamp(metav1.NewTime(c.ci.CreatedAt))
	return o
}

// strictCI wires the real landingci reader over a fake cluster holding run.
func strictCI(c *c5, run *unstructured.Unstructured) {
	d := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRuns: "PipelineRunList"}, run)
	client := &landingci.Client{Dynamic: func() (dynamic.Interface, error) { return d, nil }}
	c.src.GrantCI = client.GrantCI
	c.gsrc.Facts.GrantCI = client.GrantCI
}

// Serial: FreezeEventClock. B-01 through the real reader (landingci.Client
// .GrantCI, as the server wires it): a run with no sibling results, with
// another sibling, or with a remote pipeline is not green; the material says
// so, the request and the executor refuse, nothing is written. The control's
// grant, whose run then turns out so, is not pushed nor reserved. (On
// dff96c3c the grant read v1's LandingCI, which took all three as green.)
func TestC5B01StrictCIThroughTheReader(t *testing.T) {
	variants := map[string]func(*testing.T, *unstructured.Unstructured){
		"missing siblings": func(t *testing.T, o *unstructured.Unstructured) {
			results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			ok(t, unstructured.SetNestedSlice(o.Object, results[:1], "status", "results"))
		},
		"wrong sibling": func(t *testing.T, o *unstructured.Unstructured) {
			results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			results[1].(map[string]any)["value"] = commitB
			ok(t, unstructured.SetNestedSlice(o.Object, results, "status", "results"))
		},
		"remote pipeline": func(t *testing.T, o *unstructured.Unstructured) {
			ok(t, unstructured.SetNestedField(o.Object, "git", "spec", "pipelineRef", "resolver"))
		},
	}
	for name, mutate := range variants {
		t.Run(name, func(t *testing.T) {
			c := c5Wait(t, c5Core, false, true)
			run := greenGrantRun(t, c)
			mutate(t, run)
			strictCI(c, run)
			m, err := store.ReadLandingGrantMaterial(ctx, c.w.rt, c.gsrc, c.op, true)
			ok(t, err)
			if !containsString(m.Problems, "ci_not_green") {
				t.Fatalf("material: %v", m.Problems)
			}
			p := store.GrantParams{Env: "prod", Repo: "bifrost-platform", Task: c.root, Op: c.op, R: commitB, MaterialDigest: m.MaterialDigest, STGRelease: true}
			_, err = store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionCoreLanding, p)
			refusedWith(t, err, "ci_not_green")
			_, err = store.WriteLandingGrant(ctx, c.w.rt, c.gsrc, c.approval(store.ActionCoreLanding, time.Minute), p)
			refusedWith(t, err, "ci_not_green")
			if n := c.w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'landing_grant'`); n != 0 {
				t.Fatal("wrote a grant on loose CI")
			}
		})
	}
	t.Run("control, then the run turns", func(t *testing.T) {
		c := c5Wait(t, c5Core, false, true)
		strictCI(c, greenGrantRun(t, c))
		c.grant(t, store.ActionCoreLanding)
		bad := greenGrantRun(t, c)
		variants["missing siblings"](t, bad)
		strictCI(c, bad)
		if _, d := c.gate(t); d.Allowed || c.reservations(t) != 0 || c.phase(t) != "awaiting_grant" {
			t.Fatalf("pushed on loose CI: %+v", d)
		}
		strictCI(c, greenGrantRun(t, c))
		if _, d := c.gate(t); !d.Allowed {
			t.Fatalf("the green run: %+v", d)
		}
	})
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Serial: FreezeEventClock. B-01 at the kick: an operation that will need a
// grant (core, grants enabled) runs CI pinned to the siblings' heads read
// at its kick, recorded with the ci_kick; an ordinary one keeps v1's CI and
// records no key. Without a pinning creator the waiting operation has no
// key, and its material says ci_not_pinned.
func TestC5B01PinnedKickForGrantOperations(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		pinned     bool
	}{{"core", c5Core, true}, {"ordinary", "console/src/components/C5.tsx", false}} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().UTC().Truncate(time.Second)
			defer store.FreezeEventClock(at)()
			w := liveWorld(t, "none", at)
			store.FreezeEventClock(at.Add(-40 * time.Minute))
			root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "694 B-01 kick", BaseCommit: commitB})
			w.passRound(t, root, "h1", "h2", "h3")
			enableGrants(t, w, signer{t, rptest.NewKey(t)}, 1, true)
			src := c4Source(tc.path)
			store.FreezeEventClock(at.Add(-30 * time.Minute))
			op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, at)
			ok(t, err)
			if op == "" || !d.Allowed {
				t.Fatal("claim", d)
			}
			c4Prepare(t, w, root, op, at.Add(-29*time.Minute), true)
			store.FreezeEventClock(at.Add(-15 * time.Minute)) // ten minutes past preparation: CI is due
			var legacy, pinned []string
			var keys []store.GrantCIKey
			sources := controller.DrillSources{Facts: src,
				Main:          func(context.Context, string) (string, error) { return commitA, nil },
				WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
				CI: func(context.Context, string, string, time.Time) (landing.CIRun, error) {
					return landing.CIRun{Status: "Missing"}, nil
				},
				EnsureCI: func(_ context.Context, _ string, name, r string) error { legacy = append(legacy, name); return nil },
				EnsurePinnedCI: func(_ context.Context, name string, k store.GrantCIKey) error {
					pinned, keys = append(pinned, name), append(keys, k)
					return nil
				},
				SiblingHeads: func(context.Context) (map[string]string, error) {
					return map[string]string{"bifrost-ui": commitC, "bifrost-trade-infra": commitA}, nil
				}}
			ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}, OpenLive: func(context.Context, string, string) (controller.LiveWorkspace, error) {
				return &livePusher{outcome: landinggit.PushOK}, nil
			}}
			a, err := ctl.LandingDrillCI(ctx, sources, op, at.Add(-15*time.Minute))
			ok(t, err)
			if a.Kind != "ci_kick" {
				t.Fatal(a)
			}
			key, err := store.LandingCIKey(ctx, w.rt, op)
			ok(t, err)
			if tc.pinned {
				if len(pinned) != 1 || len(legacy) != 0 || key == nil || key.Revision != commitB || key.Base != commitA || key.Siblings["bifrost-ui"] != commitC ||
					keys[0].Siblings["bifrost-trade-infra"] != commitA {
					t.Fatalf("pinned %v legacy %v key %+v", pinned, legacy, key)
				}
				// A crash after the kick: the resume pins the same key again.
				_, err = ctl.LandingDrillCI(ctx, sources, op, at.Add(-15*time.Minute))
				ok(t, err)
				if len(pinned) < 2 || len(legacy) != 0 {
					t.Fatalf("resume: pinned %v legacy %v", pinned, legacy)
				}
			} else if len(legacy) != 1 || len(pinned) != 0 || key != nil {
				t.Fatalf("pinned %v legacy %v key %+v", pinned, legacy, key)
			}
		})
	}
}

// Serial: FreezeEventClock. B-02: an ordinary (unpinned) core
// implementation's review asks the trust chain's questions once grants are
// enabled, since its change may then be granted (§12.3 last item); before,
// reviews are as they were, and a non-core task's review never changes.
func TestC5B02TrustQuestionsForOrdinaryCoreReviewsOnceEnabled(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	attempts := map[string]string{}
	for _, c := range []bool{true, false} {
		id := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "694 B-02", BaseCommit: commitB, Core: c})
		run := w.run(t, "h1", claude, "done")
		if run.TaskID != id {
			t.Fatal("claimed", run.TaskID)
		}
		attempts[id] = run.AttemptID
	}
	var core, plain string
	for id := range attempts {
		var c bool
		ok(t, w.rt.QueryRow(ctx, `SELECT core FROM agentrt.task WHERE task_id=$1`, id).Scan(&c))
		if c {
			core = id
		} else {
			plain = id
		}
	}
	text := func(task string) string {
		var out string
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, s, err := store.PinnedReviewForTest(ctx, tx, task, attempts[task])
			out = s
			return err
		}))
		return out
	}
	if got := text(core); got != "" {
		t.Fatalf("before the entry: %q", got)
	}
	enableGrants(t, w, signer{t, rptest.NewKey(t)}, 1, true)
	if got := text(core); got != store.TrustReviewText {
		t.Fatalf("after the entry: %q", got)
	}
	if got := text(plain); got != "" {
		t.Fatalf("a non-core review: %q", got)
	}
}

// Serial: FreezeEventClock. R-06 (§5.1 step 4): a pinned root waits on
// rule_paths with no request yet; the Owner signs a rule that allows its
// path. The controller's next pass rechecks it once and pushes it on the
// rule's authority (landing_rule_clear), every unwaivable check done; before
// the new rule the recheck writes nothing. (On dff96c3c it stayed waiting
// until main moved or 72 hours passed.)
func TestC5R06RuleClearsAWaitingOperation(t *testing.T) {
	const page = "console/src/pages/C5Page.tsx"
	c := c5Wait(t, page, true, true)
	p := &livePusher{outcome: landinggit.PushOK}
	src := controller.DrillSources{Facts: c.src,
		Main:          func(context.Context, string) (string, error) { return c.main, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI:            func(context.Context, string, string, time.Time) (landing.CIRun, error) { return c.ci, nil },
		EnsureCI:      func(context.Context, string, string, string) error { return nil }}
	ctl := controller.Controller{DB: c.w.rt, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}, Drill: &src,
		OpenLive: func(context.Context, string, string) (controller.LiveWorkspace, error) { return p, nil }}
	ok(t, ctl.LandingDrillPass(ctx))
	if len(p.pushes) != 0 || c.phase(t) != "awaiting_grant" {
		t.Fatal("pushed while the rule still fails it", p.pushes)
	}
	looser := strings.Replace(c5RuleBody, `allow: ["console/src/components/**/*.tsx"]`, `allow: ["console/src/components/**/*.tsx", "console/src/pages/*.tsx"]`, 1)
	ok(t, firstErr(c.signer.sign(c.w, ruleText("landing:dev", 2, "loosen", since, until, looser))))
	ok(t, ctl.LandingDrillPass(ctx))
	if len(p.pushes) != 1 || c.phase(t) != "main_result" {
		t.Fatal("not pushed on the rule that clears it", p.pushes, c.phase(t))
	}
	var clear, use bool
	ok(t, c.w.rt.QueryRow(ctx, `SELECT body_json ? 'landing_rule_clear', body_json ? 'landing_grant_use' FROM agentrt.event
	  WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent'`, landingRepoStream).Scan(&clear, &use))
	if !clear || use || c.reservations(t) != 1 {
		t.Fatal("main_intent", clear, use, c.reservations(t))
	}
}

// Serial: FreezeEventClock. B-01: a waiting operation whose CI was never
// pinned (kicked before grants, or by a process that could not pin) has no
// key: its material says ci_not_pinned and no grant can rest on it.
func TestC5B01UnpinnedCINeverGranted(t *testing.T) {
	w, root, op, src, _ := c4Wait(t)
	src.GrantPatch = func(context.Context, string, string, string) (store.GrantPatch, error) {
		return store.GrantPatch{SHA256: strings.Repeat("7", 64), Match: true}, nil
	}
	src.GrantCI = func(context.Context, store.GrantCIKey, time.Time) (store.GrantCIEvidence, error) {
		t.Fatal("read CI with no key")
		return store.GrantCIEvidence{}, nil
	}
	gsrc := store.GrantSources{Facts: src, Main: func(context.Context) (string, error) { return commitA, nil }}
	c5Trust(t, w, root)
	m, err := store.ReadLandingGrantMaterial(ctx, w.rt, gsrc, op, true)
	ok(t, err)
	if !containsString(m.Problems, "ci_not_pinned") {
		t.Fatalf("material: %v", m.Problems)
	}
	_, err = store.WriteLandingGrant(ctx, w.rt, gsrc, store.GrantApproval{ID: "appr_unpinned", Number: 9, Action: store.ActionCoreLanding,
		ParamsHash: strings.Repeat("ab", 32), ApprovedAt: time.Now().Add(-time.Minute)},
		store.GrantParams{Env: "prod", Repo: "bifrost-platform", Task: root, Op: op, R: commitB, MaterialDigest: m.MaterialDigest, STGRelease: true})
	refusedWith(t, err, "ci_not_pinned")
}
