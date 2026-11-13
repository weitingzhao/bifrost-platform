package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PipelineRunObservations lists every retained cicd run, without a label filter.
func (s *Service) PipelineRunObservations(ctx context.Context) ([]unstructured.Unstructured, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	var out []unstructured.Unstructured
	options := metav1.ListOptions{Limit: 100}
	seen := map[string]bool{}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").List(ctx, options)
		if err != nil {
			return nil, err
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

// PipelineRunObservations exposes the service's retained runs to runtime wiring.
func (h *Handler) PipelineRunObservations(ctx context.Context) ([]unstructured.Unstructured, error) {
	return h.svc.PipelineRunObservations(ctx)
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
