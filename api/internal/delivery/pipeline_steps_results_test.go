package delivery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// The package's tests read runs as the server wires them: with
// verification's readings bound (server/delivery_ports.go). This init runs
// before any test. The tests named ...Unbound unbind them for a moment.
func init() { bindVerificationForTest() }

func bindVerificationForTest() {
	BindCIRunReadings(CIRunReadings{
		TaskRunState: cirunview.TaskRunState,
		PipelineTask: cirunview.PipelineTask,
		RunObservation: func(obj unstructured.Unstructured, pipelineName string) PipelineRunView {
			return PipelineRunView(cirunview.PipelineRunObservation(obj, pipelineName))
		},
		RunStartedAt: func(view PipelineRunView) int64 {
			return cirunview.PipelineRunStartedAt(cirunview.PipelineObservation(view))
		},
		TaskRunFacts: func(obj unstructured.Unstructured, run string) TaskRunFacts {
			t := cirunview.TaskTiming(obj, run)
			return TaskRunFacts{Name: t.Name, Task: t.Task, Status: t.Status, Finished: t.Finished, Results: fmt.Sprint(t.Results), Steps: len(t.Steps)}
		},
		ReadRunSteps: func(ctx context.Context, client func() (dynamic.Interface, error), clusterID, namespace, runName string, tasks TaskRead) PipelineRunStepsResponse {
			return cirunview.ReadRunStepsVia(ctx, client, clusterID, namespace, runName, cirunview.TaskRead(tasks))
		},
		UnitTaskRuns: cirunview.UnitTaskRuns,
		NewTimings:   func() TimingsReader { return &testTimings{} },
	})
	BindUnitTaskReadings(UnitTaskReadings{PlanOfSelector: unitexec.PlanOfSelector, CompareKey: unitexec.CompareKey})
}

type testTimings struct{ cache cirunview.TimingsCache }

func (t *testTimings) Serve(w http.ResponseWriter, r *http.Request, pipeline string, src TimingsSource) {
	t.cache.Serve(w, r, pipeline, cirunview.TimingsSource(src))
}

// Origin: TestPipelineRunStepsInlineAndResults, whose reading of the run and
// encoding of its results went to cirunview with that code on 2026-11-17
// (TestRunStepsInlineAndResults). What stayed is delivery's part of it: the
// steps route reads this service's cluster and its runs namespace when the
// caller names none, and the pipeline it answers (a reference, or the label
// of an inline run) is the name the waiter then lists that pipeline's runs by.
func TestPipelineRunStepsUseServiceClusterAndFeedRunsLookup(t *testing.T) {
	for _, tc := range []struct{ name, ref, label, pipeline string }{
		{name: "reference", ref: "bifrost-ci-platform", label: "other", pipeline: "bifrost-ci-platform"},
		{name: "inline-labelled", label: "inline-labelled", pipeline: "inline-labelled"},
		{name: "inline-unlabelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := map[string]any{"pipelineSpec": map[string]any{"tasks": []any{map[string]any{"name": "check"}}}}
			if tc.ref != "" {
				spec = map[string]any{"pipelineRef": map[string]any{"name": tc.ref}}
			}
			run := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
				"metadata": map[string]any{"name": tc.name, "namespace": "cicd"}, "spec": spec,
			}}
			if tc.label != "" {
				run.SetLabels(map[string]string{"tekton.dev/pipeline": tc.label})
			}
			task := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
				"metadata": map[string]any{"name": tc.name + "-check", "namespace": "cicd", "labels": map[string]any{
					"tekton.dev/pipelineRun": tc.name, "tekton.dev/pipelineTask": "check",
				}},
				"status": map[string]any{"conditions": []any{map[string]any{"status": "True", "reason": "Succeeded"}}},
			}}
			dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
				pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
			}, run, task)
			s := NewService(&config.ClusterEntry{ID: "test"})
			s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
			out, _ := s.PipelineRunSteps(context.Background(), "", tc.name).(cirunview.RunSteps)
			if out.Reachability != probe.ReachOK || out.ClusterID != "test" || out.Namespace != "cicd" || out.Pipeline != tc.pipeline || len(out.Tasks) != 1 {
				t.Fatalf("steps = %+v", out)
			}
			// Exercise the same steps -> pipeline runs lookup used by the waiter.
			if out.Pipeline != "" {
				runs := s.PipelineRuns(context.Background(), out.Pipeline)
				if runs.Reachability != probe.ReachOK || len(runs.Runs) != 1 || runs.Runs[0].Name != tc.name {
					t.Fatalf("runs = %+v", runs)
				}
			}
		})
	}
}

// Origin: 2026-11-21 decoupling line: delivery reaches verification through a port instead of importing cirunview
// and unitexec. Unbound, nothing panics: a run shows status Unknown, a task's
// state is unknown (never failed), the steps and timings routes answer 503
// with what is missing, and the unit TaskRuns are an error, not an empty
// list. Bound again (the other tests of this package), they read as before.
func TestVerificationReadingsUnbound(t *testing.T) {
	BindCIRunReadings(CIRunReadings{})
	BindUnitTaskReadings(UnitTaskReadings{})
	t.Cleanup(bindVerificationForTest)
	run := unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "r1", "namespace": "cicd"},
		"status": map[string]any{"conditions": []any{map[string]any{"status": "False", "reason": "Failed"}}}}}
	if v := pipelineRunFromUnstructured(run, "p"); v != (PipelineRunView{Name: "r1", Namespace: "cicd", Pipeline: "p", Status: "Unknown"}) || pipelineRunStartedAt(v) != 0 {
		t.Fatalf("run: %+v", v)
	}
	if st, task := tektonTaskRunState(run), pipelineTaskFromTaskRun(run, "r1"); st != "unknown" || task != "" {
		t.Fatalf("task: %q %q", st, task)
	}
	h := NewHandlerForTest(nil, nil)
	for name, serve := range map[string]func(http.ResponseWriter, *http.Request){
		errRunStepsUnbound: h.HandleRunSteps, errTimingsUnbound: h.HandlePipelineTimings,
	} {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "r1")
		rctx.URLParams.Add("name", "p")
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		serve(rec, req)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), name) {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) {
		return dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{taskRunGVR: "TaskRunList"}), nil
	})
	if items, err := s.UnitTaskRuns(context.Background(), unitexec.LabelPlan, ""); err == nil || err.Error() != errUnitTaskRunUnbound || items != nil {
		t.Fatalf("unit TaskRuns: %v %v", items, err)
	}
}
