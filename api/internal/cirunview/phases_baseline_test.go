package cirunview

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"testing"
)

// Phase tables group a pipeline's tasks into user-facing stages. Every task
// named here must be one the pipeline runs (bifrost-trade-infra
// k8s/cicd/tekton/pipeline-*.yaml): a name the pipeline lacks never gets a
// TaskRun, so its phase reads running or pending on a run that has finished.
// A pipeline without a table gets no phases.

// baselineDeliverStgPhaseDefs — bifrost-deliver-stg.
var baselineDeliverStgPhaseDefs = []struct {
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

var baselineDeliverPlatformPhaseDefs = []struct {
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

// baselineDeliverPlatformProdPhaseDefs — bifrost-deliver-platform-prod. It builds
// nothing: it promotes the images STG built (TD-335).
var baselineDeliverPlatformProdPhaseDefs = []struct {
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

var baselineDeliverResearchPhaseDefs = []struct {
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

// baselineAggregatePhasesForPipeline groups a run's tasks into the pipeline's phases.
// runTasks is what this run itself runs (its status.pipelineSpec): a task the
// table names and the run does not have is left out of its phase, and a phase
// left with no task is not shown. nil means the run has not said yet, and the
// table is taken whole.
func baselineAggregatePhasesForPipeline(pipelineName string, taskStatus map[string]string, runTasks map[string]bool) []PipelinePhaseView {
	switch pipelineName {
	case "bifrost-deliver-stg":
		return baselineAggregateDeliverPhases(baselineDeliverStgPhaseDefs, taskStatus, runTasks)
	case "bifrost-deliver-platform":
		return baselineAggregateDeliverPhases(baselineDeliverPlatformPhaseDefs, taskStatus, runTasks)
	case "bifrost-deliver-platform-prod":
		return baselineAggregateDeliverPhases(baselineDeliverPlatformProdPhaseDefs, taskStatus, runTasks)
	case "bifrost-deliver-research":
		return baselineAggregateDeliverPhases(baselineDeliverResearchPhaseDefs, taskStatus, runTasks)
	default:
		return []PipelinePhaseView{}
	}
}

// baselinePipelineRunTaskNames is the tasks (finally tasks included) of the pipeline
// spec the run resolved; nil until Tekton has recorded it.
func baselinePipelineRunTaskNames(run *unstructured.Unstructured) map[string]bool {
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

func baselineAggregateDeliverPhases(defs []struct {
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
		status, detail := baselineAggregatePhaseStatus(tasks, taskStatus)
		out = append(out, PipelinePhaseView{
			ID:     def.ID,
			Label:  def.Label,
			Status: status,
			Detail: detail,
		})
	}
	return out
}

func baselineAggregatePhaseStatus(tasks []string, taskStatus map[string]string) (status, detail string) {
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

// baselinePipelineRunSkippedTasks lists status.skippedTasks[].name (when guards false,
// or downstream of a skip).
func baselinePipelineRunSkippedTasks(run *unstructured.Unstructured) []string {
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

func TestPipelinePhaseProjectionOriginalParity(t *testing.T) {
	names := []string{"unknown", "bifrost-deliver-stg", "bifrost-deliver-platform", "bifrost-deliver-platform-prod", "bifrost-deliver-research"}
	states := []string{"", "pending", "running", "failed", "succeeded", "skipped", "unknown"}
	for _, name := range names {
		for _, state := range states {
			for _, runTasks := range []map[string]bool{nil, {}, {"clone-platform": true, "gitops-sync": true, "preflight-stg": true, "verify-stg": true}} {
				taskStatus := map[string]string{}
				for _, defs := range [][]struct {
					ID    string
					Label string
					Tasks []string
				}{DeliverStgPhaseDefs, DeliverPlatformPhaseDefs, DeliverPlatformProdPhaseDefs, DeliverResearchPhaseDefs} {
					for _, def := range defs {
						for _, task := range def.Tasks {
							taskStatus[task] = state
						}
					}
				}
				got, want := AggregatePhasesForPipeline(name, taskStatus, runTasks), baselineAggregatePhasesForPipeline(name, taskStatus, runTasks)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s %s: %#v != %#v", name, state, got, want)
				}
			}
		}
	}
	for _, a := range states {
		for _, b := range states {
			x, y := AggregatePhaseStatus([]string{"a", "b"}, map[string]string{"a": a, "b": b})
			u, v := baselineAggregatePhaseStatus([]string{"a", "b"}, map[string]string{"a": a, "b": b})
			if x != u || y != v {
				t.Fatal("priority mismatch")
			}
		}
	}
	for _, raw := range []map[string]any{{}, {"status": map[string]any{"pipelineSpec": map[string]any{"tasks": []any{nil, map[string]any{"name": "a"}}, "finally": []any{map[string]any{"name": "z"}}}, "skippedTasks": []any{nil, map[string]any{"name": "a"}, map[string]any{"name": ""}}}}} {
		obj := unstructured.Unstructured{Object: raw}
		if !reflect.DeepEqual(PipelineRunTaskNames(&obj), baselinePipelineRunTaskNames(&obj)) || !reflect.DeepEqual(PipelineRunSkippedTasks(&obj), baselinePipelineRunSkippedTasks(&obj)) {
			t.Fatal("task list mismatch")
		}
	}
}
