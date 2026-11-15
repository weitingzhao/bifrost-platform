package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
)

// PipelineRunObservations lists every retained cicd run, without a label filter.
//
// The runs are observations, not evidence: each comes without the copy of
// the Pipeline definition Tekton keeps in its status (status.pipelineSpec)
// and without metadata.managedFields. What a loop reads of a run is its
// reference, params, conditions, times and results. Measured 2026-11-15 on
// 1182 retained runs: the copy is 77% of the bytes (14.1 of 18.3 MB), and the
// workers, which list every run each 30 s, peaked at 640Mi in PROD and were
// OOMKilled at a 512Mi limit in STG. A reader that needs the executed
// Pipeline (the STG release evidence) gets the run itself, not this list.
func (s *Service) PipelineRunObservations(ctx context.Context) ([]unstructured.Unstructured, error) {
	return s.pipelineRuns(ctx, "")
}

// PipelineRunsOf lists the retained runs of one pipeline, by the label
// Tekton puts on every run of a named pipeline. A loop that follows one
// pipeline reads this and not every run: on 2026-11-14 the cluster kept 1073
// runs (32 MB as JSON), 113 of them (1.2 MB) of the pipeline the post-release
// check follows, and reading all of them every 30 s took the STG workers past
// their 512Mi limit three times in four minutes.
func (s *Service) PipelineRunsOf(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error) {
	if errs := validation.IsValidLabelValue(pipeline); pipeline == "" || len(errs) > 0 {
		return nil, fmt.Errorf("pipeline %q is not a label value", pipeline)
	}
	return s.pipelineRuns(ctx, "tekton.dev/pipeline="+pipeline)
}

func (s *Service) pipelineRuns(ctx context.Context, selector string) ([]unstructured.Unstructured, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	var out []unstructured.Unstructured
	options := metav1.ListOptions{Limit: 100, LabelSelector: selector}
	seen := map[string]bool{}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").List(ctx, options)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			slimObservation(&page.Items[i])
		}
		out = append(out, page.Items...)
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("pipeline run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}

// slimObservation drops from a listed run what no observer reads and what
// makes up most of it (PipelineRunObservations has the measurement).
func slimObservation(run *unstructured.Unstructured) {
	unstructured.RemoveNestedField(run.Object, "status", "pipelineSpec")
	unstructured.RemoveNestedField(run.Object, "metadata", "managedFields")
}

// PipelineRunObservations exposes the service's retained runs to runtime wiring.
func (h *Handler) PipelineRunObservations(ctx context.Context) ([]unstructured.Unstructured, error) {
	return h.svc.PipelineRunObservations(ctx)
}

// PipelineRunsOf exposes the service's read of one pipeline's runs to wiring.
func (h *Handler) PipelineRunsOf(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error) {
	return h.svc.PipelineRunsOf(ctx, pipeline)
}

// FailedTask is a task of a run that failed: its pipeline task name, the
// tests its failed-tests result names, and whether it has that result at all
// (a task that names its failed tests and named none did not get to a test's
// verdict: it did not compile, or the test program died).
type FailedTask struct {
	Task       string
	Tests      []string
	NamesTests bool
}

// RunFailedTasks lists the failed tasks of a pipeline run, by name. A run
// whose TaskRuns the cluster no longer keeps has none.
func (s *Service) RunFailedTasks(ctx context.Context, run string) ([]FailedTask, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	list, err := dyn.Resource(taskRunGVR).Namespace("cicd").List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + run})
	if err != nil {
		return nil, err
	}
	var out []FailedTask
	for _, item := range list.Items {
		if tektonTaskRunState(item) != "failed" {
			continue
		}
		f := FailedTask{Task: pipelineTaskFromTaskRun(item, run)}
		results, _, _ := unstructured.NestedSlice(item.Object, "status", "results")
		for _, value := range results {
			r, _ := value.(map[string]any)
			if r["name"] != "failed-tests" {
				continue
			}
			f.NamesTests = true
			v, _ := r["value"].(string)
			for _, name := range strings.Split(v, ",") {
				if name = strings.TrimSpace(name); name != "" && name != "…" {
					f.Tests = append(f.Tests, name)
				}
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Task < out[j].Task })
	return out, nil
}

// RunFailedTasks exposes the service's read of a run's failed tasks to wiring.
func (h *Handler) RunFailedTasks(ctx context.Context, run string) ([]FailedTask, error) {
	return h.svc.RunFailedTasks(ctx, run)
}
