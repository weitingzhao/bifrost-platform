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
)

// deliverStgPhaseDefs — user-facing stages for bifrost-deliver-stg (Console Operate step bar).
var deliverStgPhaseDefs = []struct {
	ID    string
	Label string
	Tasks []string
}{
	{
		ID:    "clone",
		Label: "Clone",
		Tasks: []string{"clone-core", "clone-worker", "clone-socket", "clone-api", "clone-frontend", "clone-ui", "clone-infra"},
	},
	{ID: "prepare", Label: "Prepare", Tasks: []string{"prepare"}},
	{
		ID:    "build",
		Label: "Build",
		Tasks: []string{"stage-api-dockerfile", "build-all-apis", "stage-frontend-dockerfile", "build-frontend", "build-worker-socket"},
	},
	{ID: "rollout", Label: "Rollout", Tasks: []string{"rollout"}},
	{ID: "verify", Label: "Verify", Tasks: []string{"verify-stg"}},
	{ID: "gitops", Label: "GitOps", Tasks: []string{"gitops-sync"}},
}

var deliverPlatformPhaseDefs = []struct {
	ID    string
	Label string
	Tasks []string
}{
	{ID: "mirror", Label: "Mirror", Tasks: []string{"mirror-sync"}},
	{ID: "clone", Label: "Clone", Tasks: []string{"clone-platform", "clone-ui"}},
	{
		ID:    "build",
		Label: "Build",
		Tasks: []string{"stage-api-dockerfile", "stage-console-dockerfile", "build-platform-api", "build-platform-console"},
	},
	{ID: "rollout", Label: "Rollout", Tasks: []string{"rollout"}},
	{ID: "gitops", Label: "GitOps", Tasks: []string{"gitops-sync"}},
}

func aggregateDeliverPhases(defs []struct {
	ID    string
	Label string
	Tasks []string
}, taskStatus map[string]string) []PipelinePhaseView {
	out := make([]PipelinePhaseView, 0, len(defs))
	for _, def := range defs {
		status, detail := aggregatePhaseStatus(def.Tasks, taskStatus)
		out = append(out, PipelinePhaseView{
			ID:     def.ID,
			Label:  def.Label,
			Status: status,
			Detail: detail,
		})
	}
	return out
}

func aggregateDeliverStgPhases(taskStatus map[string]string) []PipelinePhaseView {
	return aggregateDeliverPhases(deliverStgPhaseDefs, taskStatus)
}

func aggregateDeliverPlatformPhases(taskStatus map[string]string) []PipelinePhaseView {
	return aggregateDeliverPhases(deliverPlatformPhaseDefs, taskStatus)
}

func aggregatePhaseStatus(tasks []string, taskStatus map[string]string) (status, detail string) {
	if len(tasks) == 0 {
		return "pending", ""
	}
	var done, running, failed, pending int
	for _, t := range tasks {
		st := taskStatus[t]
		if st == "" {
			st = "pending"
		}
		switch st {
		case "succeeded":
			done++
		case "running":
			running++
		case "failed":
			failed++
		default:
			pending++
		}
	}
	total := len(tasks)
	switch {
	case failed > 0:
		return "failed", fmt.Sprintf("%d/%d failed", failed, total)
	case done == total:
		return "succeeded", fmt.Sprintf("%d/%d done", done, total)
	case running > 0 || done > 0:
		return "running", fmt.Sprintf("%d/%d done", done, total)
	default:
		return "pending", fmt.Sprintf("0/%d started", total)
	}
}

func tektonTaskRunState(obj unstructured.Unstructured) string {
	cond, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found || len(cond) == 0 {
		return "running"
	}
	m, ok := cond[0].(map[string]any)
	if !ok {
		return "running"
	}
	st := strings.ToLower(fmt.Sprint(m["status"]))
	reason := strings.ToLower(fmt.Sprint(m["reason"]))
	if st == "true" || reason == "succeeded" || reason == "completed" {
		return "succeeded"
	}
	if st == "false" || reason == "failed" || reason == "pipelinerunfailed" || reason == "taskrunfailed" {
		return "failed"
	}
	if reason == "running" || reason == "pending" || st == "unknown" {
		return "running"
	}
	return "running"
}

func pipelineTaskFromTaskRun(obj unstructured.Unstructured, runName string) string {
	if labels := obj.GetLabels(); labels != nil {
		if t := labels["tekton.dev/pipelineTask"]; t != "" {
			return t
		}
	}
	name := obj.GetName()
	prefix := runName + "-"
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix)
	}
	return name
}

func (s *Service) PipelineRunSteps(ctx context.Context, namespace, runName string) PipelineRunStepsResponse {
	now := time.Now().UTC()
	ns := namespace
	if ns == "" {
		ns = s.PipelinesNamespace()
	}
	pipelineName := "bifrost-deliver-stg"
	out := PipelineRunStepsResponse{
		ClusterID:    s.clusterID(),
		Namespace:    ns,
		RunName:      runName,
		Pipeline:     pipelineName,
		Reachability: probe.ReachFail,
		Phases:       aggregateDeliverStgPhases(map[string]string{}),
		GeneratedAt:  now,
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		out.Detail = err.Error()
		return out
	}

	if run, getErr := dyn.Resource(pipelineRunGVR).Namespace(ns).Get(ctx, runName, metav1.GetOptions{}); getErr == nil {
		if ref, ok, _ := unstructured.NestedString(run.Object, "spec", "pipelineRef", "name"); ok && ref != "" {
			pipelineName = ref
			out.Pipeline = pipelineName
		}
	}
	switch pipelineName {
	case "bifrost-deliver-platform":
		out.Phases = aggregateDeliverPlatformPhases(map[string]string{})
	default:
		out.Phases = aggregateDeliverStgPhases(map[string]string{})
	}

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

	taskStatus := map[string]string{}
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
		tasks = append(tasks, PipelineTaskRunView{
			PipelineTask: pt,
			Name:         item.GetName(),
			Status:       st,
			Reason:       reason,
		})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].PipelineTask < tasks[j].PipelineTask })

	switch pipelineName {
	case "bifrost-deliver-platform":
		out.Phases = aggregateDeliverPlatformPhases(taskStatus)
	default:
		out.Phases = aggregateDeliverStgPhases(taskStatus)
	}
	out.Tasks = tasks
	out.Reachability = probe.ReachOK
	out.Detail = fmt.Sprintf("%d task(s) · %d phase(s)", len(tasks), len(out.Phases))
	return out
}
