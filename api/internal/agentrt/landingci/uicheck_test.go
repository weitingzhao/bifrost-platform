package landingci

import (
	"context"
	"errors"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Origin: R0E (CI line 2026-11-25). A new ui commit is checked with platform's
// main before the stable pin moves: a full run (no base) with uiRevision set
// to that commit, one run per pair, and its status read back by name.
func TestUICheckIsAFullRunWithTheUICommitAndItsStatusIsRead(t *testing.T) {
	t.Parallel()
	c, _ := drillService(t)
	ctx := context.Background()
	name, err := c.EnsureUICheck(ctx, factSHA1, factSHA2)
	if err != nil || name != "ui-check-"+factSHA2[:12]+"-"+factSHA1[:8] {
		t.Fatal(name, err)
	}
	if again, err := c.EnsureUICheck(ctx, factSHA1, factSHA2); err != nil || again != name {
		t.Fatal("the same pair is the same run", again, err)
	}
	params, labels := fullParams(t, c, name)
	want := map[string]string{"revision": factSHA1, "uiRevision": factSHA2, "repo-url": platformRepoURL}
	if !reflect.DeepEqual(params, want) {
		t.Fatal(params)
	}
	if labels["bifrost.io/trigger"] != UICheckTrigger || labels["bifrost.io/ui-revision"] != factSHA2 || labels["bifrost.io/revision"] != factSHA1 {
		t.Fatal(labels)
	}
	if status, err := c.RunStatus(ctx, name); err != nil || status != "Unknown" {
		t.Fatal("a run with no condition yet", status, err)
	}
	if _, err := c.RunStatus(ctx, "ui-check-gone"); !errors.Is(err, ErrRunGone) {
		t.Fatal("a missing run", err)
	}
	if _, err := c.EnsureUICheck(ctx, "main", factSHA2); err == nil {
		t.Fatal("a branch name was accepted")
	}
}

// Origin: R0E de-duplication (2026-11-25): every ui push ran the whole
// platform suite twice, once from the Gitea trigger and once as the check
// (ci-platform-fzzks, ui-check-03dc06fd2372-f493175e). The push run of the
// commit is the check; a failed one is not, and an own run is created.
func TestTheUIPushRunIsTheCheckUnlessItFailed(t *testing.T) {
	t.Parallel()
	push := func(name, ui, status string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
			"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": map[string]any{"triggers.tekton.dev/trigger": UIPushTrigger}},
			"spec": map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-platform"},
				"params": []any{map[string]any{"name": "revision", "value": "main"}, map[string]any{"name": "uiRevision", "value": ui}}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}}}}
		return o
	}
	c, _ := drillService(t, push("ci-platform-ok", factSHA2, "True"), push("ci-platform-other", factSHA1, "True"))
	ctx := context.Background()
	if name, err := c.EnsureUICheck(ctx, factSHA1, factSHA2); err != nil || name != "ci-platform-ok" {
		t.Fatal("the push run of the commit is the check", name, err)
	}
	// Positive control: a failed push run is passed over and an own run made.
	c2, _ := drillService(t, push("ci-platform-red", factSHA2, "False"))
	if name, err := c2.EnsureUICheck(ctx, factSHA1, factSHA2); err != nil || name != UICheckRunName(factSHA1, factSHA2) {
		t.Fatal("a failed push run is not the check", name, err)
	}
}

// Origin: 2026-11-25. The Trade frontend's CI clones the stable pin too, and
// the pin moved on the console's check alone: a ui commit that broke only the
// frontend would have moved it. The frontend's push run of the commit is its
// check; a failed one is not, and an own run (3Gi, as the trigger asks) is made.
func TestTheFrontendCheckIsItsPushRunUnlessItFailed(t *testing.T) {
	t.Parallel()
	push := func(name, ui, status string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
			"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": map[string]any{"triggers.tekton.dev/trigger": UIFrontendPushTrigger}},
			"spec": map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-frontend"},
				"params": []any{map[string]any{"name": "revision", "value": "main"}, map[string]any{"name": "uiRevision", "value": ui}}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}}}}
	}
	ctx := context.Background()
	c, _ := drillService(t, push("ci-frontend-ok", factSHA2, "True"))
	if name, err := c.EnsureUIFrontendCheck(ctx, factSHA2); err != nil || name != "ci-frontend-ok" {
		t.Fatal("the frontend's push run of the commit is its check", name, err)
	}
	c2, _ := drillService(t, push("ci-frontend-red", factSHA2, "False"))
	name, err := c2.EnsureUIFrontendCheck(ctx, factSHA2)
	if err != nil || name != UIFrontendCheckRunName(factSHA2) {
		t.Fatal("a failed push run is not the check", name, err)
	}
	params, labels := fullParams(t, c2, name)
	if !reflect.DeepEqual(params, map[string]string{"revision": "main", "uiRevision": factSHA2}) || labels["bifrost.io/trigger"] != UICheckTrigger {
		t.Fatal(params, labels)
	}
	res, _ := c2.resource()
	obj, _ := res.Get(ctx, name, metav1.GetOptions{})
	ws, _, _ := unstructured.NestedSlice(obj.Object, "spec", "workspaces")
	storage, _, _ := unstructured.NestedString(ws[0].(map[string]any), "volumeClaimTemplate", "spec", "resources", "requests", "storage")
	if storage != "3Gi" {
		t.Fatal("the frontend's own run asks 3Gi", storage)
	}
	if _, err := c2.EnsureUIFrontendCheck(ctx, "main"); err == nil {
		t.Fatal("a branch name was accepted")
	}
}
