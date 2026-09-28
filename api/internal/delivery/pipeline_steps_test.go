package delivery

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAggregatePhaseStatus(t *testing.T) {
	tasks := []string{"a", "b", "c"}
	m := map[string]string{"a": "succeeded", "b": "succeeded", "c": "succeeded"}
	st, detail := aggregatePhaseStatus(tasks, m)
	if st != "succeeded" || detail != "3/3 done" {
		t.Fatalf("all succeeded: got %s %s", st, detail)
	}

	m = map[string]string{"a": "succeeded", "b": "running", "c": "pending"}
	st, _ = aggregatePhaseStatus(tasks, m)
	if st != "running" {
		t.Fatalf("partial: got %s", st)
	}

	m = map[string]string{"a": "failed", "b": "succeeded"}
	st, _ = aggregatePhaseStatus([]string{"a", "b"}, m)
	if st != "failed" {
		t.Fatalf("failed: got %s", st)
	}

	m = map[string]string{}
	st, _ = aggregatePhaseStatus(tasks, m)
	if st != "pending" {
		t.Fatalf("pending: got %s", st)
	}
}

func TestAggregateDeliverPlatformProdPhasesAllSucceeded(t *testing.T) {
	taskStatus := map[string]string{
		"preflight-stg":            "succeeded",
		"mirror-sync":              "succeeded",
		"clone-platform":           "succeeded",
		"clone-ui":                 "succeeded",
		"stage-api-dockerfile":     "succeeded",
		"stage-console-dockerfile": "succeeded",
		"build-platform-api":       "succeeded",
		"build-platform-console":   "succeeded",
		"rollout":                  "succeeded",
		"gitops-sync":              "succeeded",
	}
	phases := aggregateDeliverPlatformProdPhases(taskStatus)
	if len(phases) != 6 {
		t.Fatalf("expected 6 phases, got %d", len(phases))
	}
	for _, p := range phases {
		if p.Status != "succeeded" {
			t.Fatalf("phase %s: expected succeeded, got %s (%s)", p.ID, p.Status, p.Detail)
		}
	}
	if phases[0].ID != "preflight" || phases[0].Detail != "1/1 done" {
		t.Fatalf("preflight: %+v", phases[0])
	}
	if phases[2].ID != "clone" || phases[2].Detail != "2/2 done" {
		t.Fatalf("clone: %+v", phases[2])
	}
}

func TestAggregatePhasesForPipelinePlatformProdNotStg(t *testing.T) {
	taskStatus := map[string]string{"clone-platform": "succeeded", "clone-ui": "pending"}
	phases := aggregatePhasesForPipeline("bifrost-deliver-platform-prod", taskStatus)
	if len(phases) != 6 {
		t.Fatalf("expected 6 platform-prod phases, got %d", len(phases))
	}
	if phases[0].ID != "preflight" {
		t.Fatalf("first phase should be preflight, got %s", phases[0].ID)
	}
	if phases[2].ID != "clone" || phases[2].Detail != "1/2 done" {
		t.Fatalf("clone should be 1/2 not 1/7: %+v", phases[2])
	}
	// Trade STG defs would include Prepare — must not appear.
	for _, p := range phases {
		if p.ID == "prepare" || p.ID == "verify" {
			t.Fatalf("unexpected trade-stg phase %s in platform-prod", p.ID)
		}
	}
}

func TestAggregateDeliverStgPhasesBuildRunning(t *testing.T) {
	taskStatus := map[string]string{
		"clone-core": "succeeded", "clone-worker": "succeeded", "clone-socket": "succeeded",
		"clone-api": "succeeded", "clone-frontend": "succeeded", "clone-ui": "succeeded", "clone-infra": "succeeded",
		"prepare":        "succeeded",
		"build-all-apis": "running",
	}
	phases := aggregateDeliverStgPhases(taskStatus)
	if len(phases) != 6 {
		t.Fatalf("expected 6 phases, got %d", len(phases))
	}
	if phases[0].Status != "succeeded" || phases[0].ID != "clone" {
		t.Fatalf("clone: %+v", phases[0])
	}
	if phases[2].Status != "running" || phases[2].ID != "build" {
		t.Fatalf("build: %+v", phases[2])
	}
	if phases[3].Status != "pending" {
		t.Fatalf("rollout should be pending: %+v", phases[3])
	}
}

func TestAggregatePhasesForPipelineResearch(t *testing.T) {
	taskStatus := map[string]string{
		"mirror-sync":      "succeeded",
		"clone-research":   "succeeded",
		"build-research":   "succeeded",
		"pin-check":        "succeeded",
		"gitops-sync":      "succeeded",
		"rollout-research": "succeeded",
		"verify-research":  "failed",
	}
	phases := aggregatePhasesForPipeline("bifrost-deliver-research", taskStatus)
	if len(phases) != 7 {
		t.Fatalf("expected 7 research phases, got %d", len(phases))
	}
	if phases[0].ID != "mirror" || phases[2].ID != "build" || phases[3].ID != "pin-check" || phases[6].ID != "verify" {
		t.Fatalf("unexpected research phase ids: %+v", phases)
	}
	if phases[2].Status != "succeeded" {
		t.Fatalf("build: %+v", phases[2])
	}
	if phases[6].Status != "failed" {
		t.Fatalf("verify expected failed: %+v", phases[6])
	}
	for _, p := range phases {
		if p.ID == "prepare" {
			t.Fatalf("trade-stg prepare leaked into research phases")
		}
	}
}

func TestAggregatePhasesForPipelineResearchBuildOnly(t *testing.T) {
	taskStatus := map[string]string{
		"mirror-sync":      "succeeded",
		"clone-research":   "succeeded",
		"build-research":   "succeeded",
		"pin-check":        "succeeded",
		"gitops-sync":      "skipped",
		"rollout-research": "skipped",
		"verify-research":  "skipped",
	}
	phases := aggregatePhasesForPipeline("bifrost-deliver-research", taskStatus)
	want := []string{"succeeded", "succeeded", "succeeded", "succeeded", "skipped", "skipped", "skipped"}
	for i, p := range phases {
		if p.Status != want[i] {
			t.Fatalf("phase %s: got %s want %s", p.ID, p.Status, want[i])
		}
	}
}

func TestAggregatePhaseStatusSkippedMix(t *testing.T) {
	st, _ := aggregatePhaseStatus([]string{"a", "b"}, map[string]string{"a": "succeeded", "b": "skipped"})
	if st != "succeeded" {
		t.Fatalf("done+skipped: got %s", st)
	}
	st, _ = aggregatePhaseStatus([]string{"a", "b"}, map[string]string{"a": "skipped"})
	if st != "running" && st != "pending" {
		t.Fatalf("skipped+pending must not read as finished: got %s", st)
	}
}

func TestPipelineRunSkippedTasks(t *testing.T) {
	run := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"skippedTasks": []any{
				map[string]any{"name": "gitops-sync", "reason": "When Expressions evaluated to false"},
				map[string]any{"name": "rollout-research"},
				map[string]any{"reason": "no name"},
			},
		},
	}}
	got := pipelineRunSkippedTasks(run)
	if len(got) != 2 || got[0] != "gitops-sync" || got[1] != "rollout-research" {
		t.Fatalf("got %v", got)
	}
	if pipelineRunSkippedTasks(&unstructured.Unstructured{Object: map[string]any{}}) != nil {
		t.Fatalf("no status must yield nil")
	}
}
