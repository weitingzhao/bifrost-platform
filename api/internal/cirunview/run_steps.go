package cirunview

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The two Tekton resources a run observation is read from.
var (
	pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	taskRunGVR     = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}
)

// TaskRunView is one TaskRun of a run, as the steps route answers it.
type TaskRunView struct {
	PipelineTask string `json:"pipeline_task"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	// StartTime and CompletionTime are the TaskRun's own (RFC 3339), empty
	// until it has them: a reader that waits for a run reports how long each
	// task took without reading the cluster itself.
	StartTime      string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
}

// RunSteps is what one PipelineRun is observed to have done: its pipeline,
// phases, tasks and results. An observation, never evidence a gate reads.
type RunSteps struct {
	ClusterID    string              `json:"cluster_id"`
	Namespace    string              `json:"namespace"`
	RunName      string              `json:"run_name"`
	Pipeline     string              `json:"pipeline"`
	Reachability probe.Reachability  `json:"reachability"`
	Detail       string              `json:"detail"`
	Phases       []PipelinePhaseView `json:"phases"`
	Tasks        []TaskRunView       `json:"tasks,omitempty"`
	Results      []Result            `json:"results"`
	GeneratedAt  time.Time           `json:"generated_at"`
}

// ReadRunSteps reads one PipelineRun and its TaskRuns and answers what the
// run did. The caller owns the cluster: client is its way to one, clusterID
// its name for it, namespace where its runs live. A cluster that cannot be
// reached is an answer (Reachability, Detail), not an error.
func ReadRunSteps(ctx context.Context, client func() (dynamic.Interface, error), clusterID, namespace, runName string) RunSteps {
	now := time.Now().UTC()
	// The pipeline is the one the run names; until the run is read there is
	// none, and so no phases.
	pipelineName := ""
	out := RunSteps{
		ClusterID:    clusterID,
		Namespace:    namespace,
		RunName:      runName,
		Reachability: probe.ReachFail,
		Phases:       []PipelinePhaseView{},
		Results:      []Result{},
		GeneratedAt:  now,
	}

	dyn, err := client()
	if err != nil {
		out.Detail = err.Error()
		return out
	}

	skippedTasks := []string{}
	var runTasks map[string]bool
	if run, getErr := dyn.Resource(pipelineRunGVR).Namespace(namespace).Get(ctx, runName, metav1.GetOptions{}); getErr == nil {
		if ref, ok, _ := unstructured.NestedString(run.Object, "spec", "pipelineRef", "name"); ok && ref != "" {
			pipelineName = ref
		} else {
			// Inline pipelineSpec runs have no pipelineRef; Tekton identifies
			// their pipeline with this label. Readers use it to list the run.
			pipelineName = run.GetLabels()["tekton.dev/pipeline"]
		}
		out.Pipeline = pipelineName
		out.Results = Results(run)
		skippedTasks = PipelineRunSkippedTasks(run)
		runTasks = PipelineRunTaskNames(run)
	}
	out.Phases = AggregatePhasesForPipeline(pipelineName, map[string]string{}, runTasks)

	list, err := dyn.Resource(taskRunGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", runName),
	})
	if err != nil {
		out.Detail = fmt.Sprintf("list taskruns: %v", err)
		return out
	}
	if len(list.Items) == 0 {
		all, listErr := dyn.Resource(taskRunGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
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
	tasks := make([]TaskRunView, 0, len(list.Items))
	for _, item := range list.Items {
		pt := PipelineTask(item, runName)
		st := TaskRunState(item)
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
		tasks = append(tasks, TaskRunView{
			PipelineTask:   pt,
			Name:           item.GetName(),
			Status:         st,
			Reason:         reason,
			StartTime:      started,
			CompletionTime: completed,
		})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].PipelineTask < tasks[j].PipelineTask })

	out.Phases = AggregatePhasesForPipeline(pipelineName, taskStatus, runTasks)
	out.Tasks = tasks
	out.Reachability = probe.ReachOK
	out.Detail = fmt.Sprintf("%d task(s) · %d phase(s)", len(tasks), len(out.Phases))
	return out
}
