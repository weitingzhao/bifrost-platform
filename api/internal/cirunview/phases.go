package cirunview

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type PipelinePhaseView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Phase tables group a pipeline's tasks into user-facing stages. Every task
// named here must be one the pipeline runs (bifrost-trade-infra
// k8s/cicd/tekton/pipeline-*.yaml): a name the pipeline lacks never gets a
// TaskRun, so its phase reads running or pending on a run that has finished.
// A pipeline without a table gets no phases.

// DeliverStgPhaseDefs — bifrost-deliver-stg.
var DeliverStgPhaseDefs = []struct {
	ID    string
	Label string
	Tasks []string
}{
	{
		ID:    "clone",
		Label: "Clone",
		Tasks: []string{"clone-core", "clone-worker", "clone-api", "clone-frontend", "clone-ui", "clone-infra"},
	},
	{ID: "prepare", Label: "Prepare", Tasks: []string{"prepare"}},
	{
		ID:    "build",
		Label: "Build",
		Tasks: []string{"stage-api-dockerfile", "build-all-apis", "stage-frontend-dockerfile", "build-frontend", "build-worker"},
	},
	{ID: "rollout", Label: "Rollout", Tasks: []string{"rollout"}},
	{ID: "verify", Label: "Verify", Tasks: []string{"verify-stg"}},
	{ID: "gitops", Label: "GitOps", Tasks: []string{"gitops-sync"}},
}

var DeliverPlatformPhaseDefs = []struct {
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

// DeliverPlatformProdPhaseDefs — bifrost-deliver-platform-prod. It builds
// nothing: it promotes the images STG built (TD-335).
var DeliverPlatformProdPhaseDefs = []struct {
	ID    string
	Label string
	Tasks []string
}{
	{ID: "preflight", Label: "Preflight", Tasks: []string{"preflight-stg"}},
	{ID: "mirror", Label: "Mirror", Tasks: []string{"mirror-sync"}},
	{ID: "clone", Label: "Clone", Tasks: []string{"check-source", "clone-platform"}},
	{ID: "schema", Label: "Schema", Tasks: []string{"agentrt-schema"}},
	{ID: "promote", Label: "Promote", Tasks: []string{"promote-images"}},
	{ID: "gitops", Label: "GitOps", Tasks: []string{"gitops-sync"}},
	{ID: "rollout", Label: "Rollout", Tasks: []string{"rollout"}},
}

var DeliverResearchPhaseDefs = []struct {
	ID    string
	Label string
	Tasks []string
}{
	{ID: "mirror", Label: "Mirror", Tasks: []string{"mirror-sync"}},
	{ID: "clone", Label: "Clone", Tasks: []string{"clone-research"}},
	{ID: "build", Label: "Build", Tasks: []string{"build-research"}},
	{ID: "pin-check", Label: "Pin check", Tasks: []string{"pin-check"}},
	{ID: "gitops", Label: "GitOps", Tasks: []string{"gitops-sync"}},
	{ID: "rollout", Label: "Rollout", Tasks: []string{"rollout-research"}},
	{ID: "verify", Label: "Verify", Tasks: []string{"verify-research"}},
}

// AggregatePhasesForPipeline groups a run's tasks into the pipeline's phases.
// runTasks is what this run itself runs (its status.pipelineSpec): a task the
// table names and the run does not have is left out of its phase, and a phase
// left with no task is not shown. nil means the run has not said yet, and the
// table is taken whole.
func AggregatePhasesForPipeline(pipelineName string, taskStatus map[string]string, runTasks map[string]bool) []PipelinePhaseView {
	switch pipelineName {
	case "bifrost-deliver-stg":
		return AggregateDeliverPhases(DeliverStgPhaseDefs, taskStatus, runTasks)
	case "bifrost-deliver-platform":
		return AggregateDeliverPhases(DeliverPlatformPhaseDefs, taskStatus, runTasks)
	case "bifrost-deliver-platform-prod":
		return AggregateDeliverPhases(DeliverPlatformProdPhaseDefs, taskStatus, runTasks)
	case "bifrost-deliver-research":
		return AggregateDeliverPhases(DeliverResearchPhaseDefs, taskStatus, runTasks)
	default:
		return []PipelinePhaseView{}
	}
}

// PipelineRunTaskNames is the tasks (finally tasks included) of the pipeline
// spec the run resolved; nil until Tekton has recorded it.
func PipelineRunTaskNames(run *unstructured.Unstructured) map[string]bool {
	var out map[string]bool
	for _, field := range []string{"tasks", "finally"} {
		items, _, _ := unstructured.NestedSlice(run.Object, "status", "pipelineSpec", field)
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				if name, ok := m["name"].(string); ok && name != "" {
					if out == nil {
						out = map[string]bool{}
					}
					out[name] = true
				}
			}
		}
	}
	return out
}

func AggregateDeliverPhases(defs []struct {
	ID    string
	Label string
	Tasks []string
}, taskStatus map[string]string, runTasks map[string]bool) []PipelinePhaseView {
	out := make([]PipelinePhaseView, 0, len(defs))
	for _, def := range defs {
		tasks := def.Tasks
		if runTasks != nil {
			tasks = make([]string, 0, len(def.Tasks))
			for _, t := range def.Tasks {
				if runTasks[t] {
					tasks = append(tasks, t)
				}
			}
			if len(tasks) == 0 {
				continue
			}
		}
		status, detail := AggregatePhaseStatus(tasks, taskStatus)
		out = append(out, PipelinePhaseView{
			ID:     def.ID,
			Label:  def.Label,
			Status: status,
			Detail: detail,
		})
	}
	return out
}

func AggregateDeliverStgPhases(taskStatus map[string]string) []PipelinePhaseView {
	return AggregateDeliverPhases(DeliverStgPhaseDefs, taskStatus, nil)
}

func AggregateDeliverPlatformProdPhases(taskStatus map[string]string) []PipelinePhaseView {
	return AggregateDeliverPhases(DeliverPlatformProdPhaseDefs, taskStatus, nil)
}

func AggregatePhaseStatus(tasks []string, taskStatus map[string]string) (status, detail string) {
	if len(tasks) == 0 {
		return "pending", ""
	}
	var done, running, failed, skipped, pending int
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
		case "skipped":
			skipped++
		default:
			pending++
		}
	}
	total := len(tasks)
	switch {
	case failed > 0:
		return "failed", fmt.Sprintf("%d/%d failed", failed, total)
	case skipped == total:
		return "skipped", "skipped"
	case done+skipped == total:
		return "succeeded", fmt.Sprintf("%d/%d done", done, total)
	case running > 0 || done > 0:
		return "running", fmt.Sprintf("%d/%d done", done, total)
	default:
		return "pending", fmt.Sprintf("0/%d started", total)
	}
}

// PipelineRunSkippedTasks lists status.skippedTasks[].name (when guards false,
// or downstream of a skip).
func PipelineRunSkippedTasks(run *unstructured.Unstructured) []string {
	items, found, _ := unstructured.NestedSlice(run.Object, "status", "skippedTasks")
	if !found {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}
