package cirunview

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAggregatePhaseStatus(t *testing.T) {
	tasks := []string{"a", "b", "c"}
	m := map[string]string{"a": "succeeded", "b": "succeeded", "c": "succeeded"}
	st, detail := AggregatePhaseStatus(tasks, m)
	if st != "succeeded" || detail != "3/3 done" {
		t.Fatalf("all succeeded: got %s %s", st, detail)
	}

	m = map[string]string{"a": "succeeded", "b": "running", "c": "pending"}
	st, _ = AggregatePhaseStatus(tasks, m)
	if st != "running" {
		t.Fatalf("partial: got %s", st)
	}

	m = map[string]string{"a": "failed", "b": "succeeded"}
	st, _ = AggregatePhaseStatus([]string{"a", "b"}, m)
	if st != "failed" {
		t.Fatalf("failed: got %s", st)
	}

	m = map[string]string{}
	st, _ = AggregatePhaseStatus(tasks, m)
	if st != "pending" {
		t.Fatalf("pending: got %s", st)
	}
}

func TestAggregateDeliverPlatformProdPhasesAllSucceeded(t *testing.T) {
	// The nine tasks of run bifrost-deliver-platform-prod-a58af341e037a522-1.
	taskStatus := map[string]string{
		"freeze-check":   "succeeded",
		"preflight-stg":  "succeeded",
		"mirror-sync":    "succeeded",
		"check-source":   "succeeded",
		"clone-platform": "succeeded",
		"agentrt-schema": "succeeded",
		"promote-images": "succeeded",
		"gitops-sync":    "succeeded",
		"rollout":        "succeeded",
	}
	phases := AggregateDeliverPlatformProdPhases(taskStatus)
	if len(phases) != 7 {
		t.Fatalf("expected 7 phases, got %d", len(phases))
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
	taskStatus := map[string]string{"check-source": "succeeded", "clone-platform": "pending"}
	phases := AggregatePhasesForPipeline("bifrost-deliver-platform-prod", taskStatus, nil)
	if len(phases) != 7 {
		t.Fatalf("expected 7 platform-prod phases, got %d", len(phases))
	}
	if phases[0].ID != "preflight" {
		t.Fatalf("first phase should be preflight, got %s", phases[0].ID)
	}
	if phases[2].ID != "clone" || phases[2].Detail != "1/2 done" {
		t.Fatalf("clone should be 1/2 not 1/7: %+v", phases[2])
	}
	// Trade STG defs would include Prepare — must not appear.
	for _, p := range phases {
		if p.ID == "prepare" || p.ID == "verify" || p.ID == "build" {
			t.Fatalf("unexpected trade-stg phase %s in platform-prod", p.ID)
		}
	}
}

func TestAggregateDeliverStgPhasesBuildRunning(t *testing.T) {
	taskStatus := map[string]string{
		"clone-core": "succeeded", "clone-worker": "succeeded",
		"clone-api": "succeeded", "clone-frontend": "succeeded", "clone-ui": "succeeded", "clone-infra": "succeeded",
		"prepare":        "succeeded",
		"build-all-apis": "running",
	}
	phases := AggregateDeliverStgPhases(taskStatus)
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
	phases := AggregatePhasesForPipeline("bifrost-deliver-research", taskStatus, nil)
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
	phases := AggregatePhasesForPipeline("bifrost-deliver-research", taskStatus, nil)
	want := []string{"succeeded", "succeeded", "succeeded", "succeeded", "skipped", "skipped", "skipped"}
	for i, p := range phases {
		if p.Status != want[i] {
			t.Fatalf("phase %s: got %s want %s", p.ID, p.Status, want[i])
		}
	}
}

func TestAggregatePhaseStatusSkippedMix(t *testing.T) {
	st, _ := AggregatePhaseStatus([]string{"a", "b"}, map[string]string{"a": "succeeded", "b": "skipped"})
	if st != "succeeded" {
		t.Fatalf("done+skipped: got %s", st)
	}
	st, _ = AggregatePhaseStatus([]string{"a", "b"}, map[string]string{"a": "skipped"})
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
	got := PipelineRunSkippedTasks(run)
	if len(got) != 2 || got[0] != "gitops-sync" || got[1] != "rollout-research" {
		t.Fatalf("got %v", got)
	}
	if PipelineRunSkippedTasks(&unstructured.Unstructured{Object: map[string]any{}}) != nil {
		t.Fatalf("no status must yield nil")
	}
}

// pipelineTasks is what each pipeline with a phase table runs, read from
// `kubectl -n cicd get pipeline <name>` on 2026-11-13 (the same lists as
// bifrost-trade-infra k8s/cicd/tekton/pipeline-*.yaml on main). The
// definitions live in another repository, so this copy is what the tables
// are held to; update both when a pipeline's tasks change.
var pipelineTasks = map[string][]string{
	"bifrost-deliver-stg": {"freeze-check", "mirror-sync", "clone-core", "clone-worker", "clone-api", "clone-frontend", "clone-ui", "clone-infra",
		"prepare", "stage-api-dockerfile", "build-all-apis", "stage-frontend-dockerfile", "build-frontend", "build-worker", "rollout", "verify-stg", "gitops-sync"},
	"bifrost-deliver-platform": {"freeze-check", "mirror-sync", "clone-infra", "clone-platform", "clone-ui", "stage-api-dockerfile", "stage-console-dockerfile",
		"build-platform-api", "build-platform-console", "agentrt-migrate", "promote-images", "gitops-sync", "rollout"},
	"bifrost-deliver-platform-prod": {"freeze-check", "preflight-stg", "mirror-sync", "check-source", "clone-platform", "agentrt-schema", "promote-images", "gitops-sync", "rollout"},
	"bifrost-deliver-research": {"freeze-check", "release-window", "validate-revision", "mirror-sync", "clone-research", "clone-infra", "lint-test",
		"build-research", "pin-check", "gitops-sync", "rollout-research", "verify-research"},
}

// A run whose every task succeeded has every phase succeeded. A table that
// names a task the pipeline does not run fails here: that phase would never
// leave running or pending.
func TestPhaseTablesMatchWhatThePipelinesRun(t *testing.T) {
	for pipeline, tasks := range pipelineTasks {
		done := map[string]string{}
		for _, task := range tasks {
			done[task] = "succeeded"
		}
		phases := AggregatePhasesForPipeline(pipeline, done, nil)
		if len(phases) == 0 {
			t.Errorf("%s has no phase table", pipeline)
		}
		for _, p := range phases {
			if p.Status != "succeeded" {
				t.Errorf("%s: phase %s is %s (%s) on a run whose tasks all succeeded", pipeline, p.ID, p.Status, p.Detail)
			}
		}
	}
}

// Origin: 2026-11-15, the STG platform delivery dropping mirror-sync and
// merging the two stage-*-dockerfile tasks into their builds (infra
// ciw1/stg-deliver-fewer-pods). With the table taken whole, a finished run of
// that pipeline read Mirror = pending "0/1 started" and Build = running "2/4
// done" for ever. The run says what it runs; the table only groups.
func TestPhasesLeaveOutTasksTheRunDoesNotHave(t *testing.T) {
	run := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"pipelineSpec": map[string]any{
		"tasks": []any{
			map[string]any{"name": "freeze-check"}, map[string]any{"name": "clone-platform"}, map[string]any{"name": "clone-ui"},
			map[string]any{"name": "build-platform-api"}, map[string]any{"name": "build-platform-console"},
			map[string]any{"name": "rollout"}, map[string]any{"name": "gitops-sync"},
		},
	}}}}
	runTasks := PipelineRunTaskNames(run)
	done := map[string]string{}
	for name := range runTasks {
		done[name] = "succeeded"
	}
	phases := AggregatePhasesForPipeline("bifrost-deliver-platform", done, runTasks)
	ids := []string{}
	for _, p := range phases {
		ids = append(ids, p.ID)
		if p.Status != "succeeded" {
			t.Errorf("phase %s is %s (%s) on a run whose tasks all succeeded", p.ID, p.Status, p.Detail)
		}
	}
	if got, want := strings.Join(ids, ","), "clone,build,rollout,gitops"; got != want {
		t.Fatalf("phases = %s, want %s (Mirror has no task in this run)", got, want)
	}
	for _, p := range phases {
		if p.ID == "build" && p.Detail != "2/2 done" {
			t.Errorf("build detail = %q, want 2/2 done", p.Detail)
		}
	}
	// The control: the same finished run read without what it runs is the bug.
	for _, p := range AggregatePhasesForPipeline("bifrost-deliver-platform", done, nil) {
		if p.ID == "mirror" && p.Status != "pending" {
			t.Errorf("control: mirror = %s, want pending when the table is taken whole", p.Status)
		}
	}
	// A run Tekton has not resolved yet says nothing, and the table stands.
	if PipelineRunTaskNames(&unstructured.Unstructured{Object: map[string]any{}}) != nil {
		t.Fatal("a run with no pipelineSpec must yield nil, not an empty set")
	}
}

func TestPipelineWithoutAPhaseTableGetsNoPhases(t *testing.T) {
	for _, pipeline := range []string{"bifrost-ci-platform", "bifrost-deliver-prod", "bifrost-build-market-data", ""} {
		phases := AggregatePhasesForPipeline(pipeline, map[string]string{"clone-core": "succeeded"}, nil)
		if phases == nil || len(phases) != 0 {
			t.Errorf("%q: phases = %#v, want an empty list", pipeline, phases)
		}
	}
}
