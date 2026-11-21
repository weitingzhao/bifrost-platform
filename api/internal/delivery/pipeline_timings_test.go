package delivery

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// Origin: relay F, 2026-11-15 (the timings view shows only the references it
// uses, never an arbitrary parameter) and #63, 2026-11-11 (a page's unfiltered
// cluster reads starved the platform). The reader those two were tested
// through went to cirunview on 2026-11-17 and takes its parameter list and
// its list of a pipeline's runs from the caller; this is the caller. Through
// the route: a parameter outside delivery's list is not in the answer, one in
// it is, and every list the route causes carries a label selector.
func TestTimingsRouteShowsOnlyListedParamsAndReadsByLabel(t *testing.T) {
	object := func(kind, name string, labels map[string]any) *unstructured.Unstructured {
		labels["tekton.dev/pipeline"] = "ci"
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": kind,
			"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": labels, "creationTimestamp": "2026-11-15T20:00:00Z"},
			"spec": map[string]any{"pipelineRef": map[string]any{"name": "ci"}, "params": []any{
				map[string]any{"name": "revision", "value": strings.Repeat("a", 40)},
				map[string]any{"name": "password", "value": "must-not-be-returned"}}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}}}}}
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList"},
		object("PipelineRun", "ci-one", map[string]any{"bifrost.io/repo": "r"}),
		object("TaskRun", "ci-one-changes", map[string]any{"tekton.dev/pipelineRun": "ci-one", "tekton.dev/pipelineTask": "changes"}))
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	router := chi.NewRouter()
	router.Get("/pipelines/{name}/timings", NewHandlerForTest(s, nil).HandlePipelineTimings)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/pipelines/ci/timings?repo=r", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"name":"ci-one"`) || !strings.Contains(body, `"task":"changes"`) {
		t.Fatalf("%d %s", rec.Code, body)
	}
	if !strings.Contains(body, `"params":{"revision":"`+strings.Repeat("a", 40)+`"}`) {
		t.Fatalf("the listed parameter is not the whole of params: %s", body)
	}
	// The control for the check above: the parameter is in the run that was read.
	if strings.Contains(body, "must-not-be-returned") || strings.Contains(body, "password") {
		t.Fatalf("a parameter outside the list is exposed: %s", body)
	}
	lists := 0
	for _, action := range dyn.Actions() {
		if action.GetVerb() != "list" {
			t.Fatalf("the route did %s", action.GetVerb())
		}
		lists++
		if action.(ktesting.ListAction).GetListRestrictions().Labels.String() == "" {
			t.Fatalf("unfiltered read of %s", action.GetResource().Resource)
		}
	}
	if lists != 2 {
		t.Fatalf("%d list reads, want the pipeline's runs and their tasks", lists)
	}
}
