package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

func tektonTaskRunState(obj unstructured.Unstructured) string { return cirunview.TaskRunState(obj) }

func pipelineTaskFromTaskRun(obj unstructured.Unstructured, runName string) string {
	return cirunview.PipelineTask(obj, runName)
}

func (s *Service) PipelineRunSteps(ctx context.Context, namespace, runName string) PipelineRunStepsResponse {
	now := time.Now().UTC()
	ns := namespace
	if ns == "" {
		ns = s.PipelinesNamespace()
	}
	// The pipeline is the one the run names; until the run is read there is
	// none, and so no phases.
	pipelineName := ""
	out := PipelineRunStepsResponse{
		ClusterID:    s.clusterID(),
		Namespace:    ns,
		RunName:      runName,
		Reachability: probe.ReachFail,
		Phases:       []PipelinePhaseView{},
		Results:      []PipelineRunResult{},
		GeneratedAt:  now,
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		out.Detail = err.Error()
		return out
	}

	skippedTasks := []string{}
	var runTasks map[string]bool
	if run, getErr := dyn.Resource(pipelineRunGVR).Namespace(ns).Get(ctx, runName, metav1.GetOptions{}); getErr == nil {
		if ref, ok, _ := unstructured.NestedString(run.Object, "spec", "pipelineRef", "name"); ok && ref != "" {
			pipelineName = ref
		} else {
			// Inline pipelineSpec runs have no pipelineRef; Tekton identifies
			// their pipeline with this label. Readers use it to list the run.
			pipelineName = run.GetLabels()["tekton.dev/pipeline"]
		}
		out.Pipeline = pipelineName
		out.Results = cirunview.Results(run)
		skippedTasks = cirunview.PipelineRunSkippedTasks(run)
		runTasks = cirunview.PipelineRunTaskNames(run)
	}
	out.Phases = cirunview.AggregatePhasesForPipeline(pipelineName, map[string]string{}, runTasks)

	list, err := dyn.Resource(taskRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", runName),
	})
	if err != nil {
		out.Detail = fmt.Sprintf("list taskruns: %v", err)
		return out
	}
	if len(list.Items) == 0 {
		all, listErr := dyn.Resource(taskRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
		if listErr == nil {
			prefix := runName + "-"
			for _, item := range all.Items {
				if strings.HasPrefix(item.GetName(), prefix) {
					list.Items = append(list.Items, item)
				}
			}
		}
	}

	// Skipped tasks never get a TaskRun; without this they would read as pending
	// on a run that already finished.
	taskStatus := map[string]string{}
	for _, name := range skippedTasks {
		taskStatus[name] = "skipped"
	}
	tasks := make([]PipelineTaskRunView, 0, len(list.Items))
	for _, item := range list.Items {
		pt := pipelineTaskFromTaskRun(item, runName)
		st := tektonTaskRunState(item)
		taskStatus[pt] = st
		reason := ""
		if cond, found, _ := unstructured.NestedSlice(item.Object, "status", "conditions"); found && len(cond) > 0 {
			if m, ok := cond[0].(map[string]any); ok {
				if r, ok := m["reason"].(string); ok {
					reason = r
				}
			}
		}
		started, _, _ := unstructured.NestedString(item.Object, "status", "startTime")
		completed, _, _ := unstructured.NestedString(item.Object, "status", "completionTime")
		tasks = append(tasks, PipelineTaskRunView{
			PipelineTask:   pt,
			Name:           item.GetName(),
			Status:         st,
			Reason:         reason,
			StartTime:      started,
			CompletionTime: completed,
		})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].PipelineTask < tasks[j].PipelineTask })

	out.Phases = cirunview.AggregatePhasesForPipeline(pipelineName, taskStatus, runTasks)
	out.Tasks = tasks
	out.Reachability = probe.ReachOK
	out.Detail = fmt.Sprintf("%d task(s) · %d phase(s)", len(tasks), len(out.Phases))
	return out
}
