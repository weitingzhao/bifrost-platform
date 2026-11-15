package delivery

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

// Origin: relay F, 2026-11-15: the CI page lacked the changes result and
// per-step times; benchmark 1999f61b's 35 s before tests must not be confused
// with its 32 s changes task, and a zero-second step is a real observation.
func timingFixture(kind, name, pipeline, repo string) *unstructured.Unstructured {
	labels := map[string]any{"tekton.dev/pipeline": pipeline, "bifrost.io/repo": repo}
	spec := map[string]any{"pipelineRef": map[string]any{"name": pipeline}, "params": []any{map[string]any{"name": "revision", "value": strings.Repeat("a", 40)}, map[string]any{"name": "password", "value": "must-not-be-returned"}}}
	status := map[string]any{"startTime": "2026-11-15T20:00:00Z", "completionTime": "2026-11-15T20:01:00Z", "conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}}}
	if kind == "TaskRun" {
		labels["tekton.dev/pipelineRun"] = "ci-one"
		labels["tekton.dev/pipelineTask"] = "changes"
		status["results"] = []any{map[string]any{"name": "run-modules", "value": " api repo "}, map[string]any{"name": "why", "value": ""}}
		status["steps"] = []any{map[string]any{"name": "diff", "terminated": map[string]any{"startedAt": "2026-11-15T20:00:03Z", "finishedAt": "2026-11-15T20:00:03Z", "exitCode": int64(0)}}, map[string]any{"name": "next", "running": map[string]any{"startedAt": "2026-11-15T20:00:04Z"}}}
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": "cicd", "labels": labels, "creationTimestamp": "2026-11-15T20:00:00Z"}, "spec": spec, "status": status}}
}
func timingsService(objects ...runtime.Object) (*Service, *dynfake.FakeDynamicClient) {
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList"}, objects...)
	svc := NewService(&config.ClusterEntry{ID: "test"})
	svc.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	return svc, dyn
}
func TestTimingReadsResultsAndZeroSecondStep(t *testing.T) {
	s, d := timingsService(timingFixture("PipelineRun", "ci-one", "ci", "r"), timingFixture("PipelineRun", "other", "ci", "another"), timingFixture("TaskRun", "ci-one-changes", "ci", "r"))
	out, err := s.readPipelineTimings(context.Background(), "ci", "r", "", 60)
	if err != nil || out.Retained != 1 || len(out.Runs) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	run := out.Runs[0]
	if _, ok := run.Params["password"]; ok {
		t.Fatal("arbitrary parameter exposed")
	}
	if len(run.Tasks) != 1 || len(run.Tasks[0].Steps) != 2 {
		t.Fatalf("tasks %+v", run.Tasks)
	}
	task := run.Tasks[0]
	if task.Results[0].Value != " api repo " || task.Results[1].Value != "" {
		t.Fatal(task.Results)
	}
	if step := task.Steps[0]; step.Started != step.Finished || step.ExitCode == nil || *step.ExitCode != 0 || step.Status != "succeeded" {
		t.Fatal(step)
	}
	if step := task.Steps[1]; step.Finished != "" || step.Status != "running" {
		t.Fatal(step)
	}
	for _, action := range d.Actions() {
		if action.GetVerb() == "list" {
			a := action.(ktesting.ListAction)
			if a.GetListRestrictions().Labels.String() == "" {
				t.Fatal("unfiltered cluster read")
			}
		} else if action.GetVerb() != "get" {
			t.Fatal("write action", action)
		}
	}
}

// Origin: relay F retention boundary, 2026-11-15: an expired task is absent,
// while an API read failure is unknown; neither is a successful zero duration.
func TestTimingRetentionAndFailedTaskRead(t *testing.T) {
	s, d := timingsService(timingFixture("PipelineRun", "ci-one", "ci", "r"))
	out, err := s.readPipelineTimings(context.Background(), "ci", "r", "", 60)
	if err != nil || out.Runs[0].Tasks == nil || len(out.Runs[0].Tasks) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = s.readPipelineTimings(context.Background(), "ci", "r", "expired", 60); err == nil {
		t.Fatal("missing run hidden")
	}
	d.PrependReactor("list", "taskruns", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("cluster unavailable")
	})
	if _, err = s.readPipelineTimings(context.Background(), "ci", "r", "", 60); err == nil {
		t.Fatal("failed task read hidden")
	}
}

// Origin: #63 (2026-11-11), page fan-out starved the platform. A second reader
// uses the short cache; oversized histories and selectors are rejected.
func TestTimingHandlerCacheAndBounds(t *testing.T) {
	s, d := timingsService(timingFixture("PipelineRun", "ci-one", "ci", "r"))
	h := NewHandlerForTest(s, nil)
	router := chi.NewRouter()
	router.Get("/pipelines/{name}/timings", h.HandlePipelineTimings)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/pipelines/ci/timings?repo=r", nil))
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	if len(d.Actions()) != 2 {
		t.Fatalf("cache caused %d cluster requests", len(d.Actions()))
	}
	for _, query := range []string{"limit=101", "limit=0", "repo=a,b", "run=bad%20name"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/pipelines/ci/timings?"+query, nil))
		if rec.Code != 400 {
			t.Fatal(query, rec.Code)
		}
	}
	// Direct lookup does not leak a run from another pipeline.
	out, err := s.readPipelineTimings(context.Background(), "another", "r", "ci-one", 60)
	if err != nil || len(out.Runs) != 0 {
		t.Fatal(out, err)
	}
}

// Origin: 2026-11-15 baseline uses retained cluster objects. Kubernetes
// paginates these lists; a partial first page must never masquerade as all tasks.
func TestTimingTaskPagination(t *testing.T) {
	s, d := timingsService(timingFixture("PipelineRun", "ci-one", "ci", "r"))
	calls := 0
	d.PrependReactor("list", "taskruns", func(ktesting.Action) (bool, runtime.Object, error) {
		calls++
		page := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRunList"}}
		if calls == 1 {
			page.SetContinue("next")
		} else {
			page.Items = []unstructured.Unstructured{*timingFixture("TaskRun", "ci-one-changes", "ci", "r")}
		}
		return true, page, nil
	})
	out, err := s.readPipelineTimings(context.Background(), "ci", "r", "", 60)
	if err != nil || calls != 2 || len(out.Runs[0].Tasks) != 1 {
		t.Fatal(out, err, calls)
	}
}
