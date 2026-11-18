package cirunview

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
)

func summaryRun(name, status string) *unstructured.Unstructured {
	obj := map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{}}
	if status != "" {
		obj["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}}
	}
	return &unstructured.Unstructured{Object: obj}
}

func summaryTaskRun(run, name, task, status string, exit int64, results map[string]string) *unstructured.Unstructured {
	var res []any
	for k, v := range results {
		res = append(res, map[string]any{"name": k, "value": v})
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd", "creationTimestamp": "2026-11-17T20:00:00Z",
			"labels": map[string]any{"tekton.dev/pipelineRun": run, "tekton.dev/pipelineTask": task}},
		"status": map[string]any{
			"conditions":     []any{map[string]any{"type": "Succeeded", "status": status, "reason": map[string]string{"True": "Succeeded", "False": "Failed"}[status]}},
			"completionTime": "2026-11-17T20:03:00Z",
			"steps": []any{
				map[string]any{"name": "prepare", "terminated": map[string]any{"startedAt": "2026-11-17T20:00:09Z", "exitCode": int64(0)}},
				map[string]any{"name": "test", "terminated": map[string]any{"startedAt": "2026-11-17T20:00:20Z", "exitCode": exit}},
			},
			"results": res,
		}}}
}

// 2026-11-17, shared notes duty/from-ci-w1-2026-11-17-taskrun-readers-questioned.md:
// a finished run's TaskRuns were kept two days for a few fields; the summary on
// the run holds exactly those (the Succeeded condition as written, the first
// step's start, the failed step, the results cut short), tasks in name order.
// Positive control: summarizing without the exit-code check leaves FailedStep
// empty and the second check fails.
func TestSummaryHoldsWhatTheReadersUse(t *testing.T) {
	long := strings.Repeat("x", 400)
	s := Summarize([]unstructured.Unstructured{
		*summaryTaskRun("r", "r-test-api", "test-api", "False", 2, map[string]string{"log": long}),
		*summaryTaskRun("r", "r-changes", "changes", "True", 0, map[string]string{"run-modules": "* "}),
	})
	if s.V != SummaryVersion || len(s.Tasks) != 2 || s.Tasks[0].Task != "changes" || s.Tasks[1].Task != "test-api" {
		t.Fatalf("summary %+v", s)
	}
	c, f := s.Tasks[0], s.Tasks[1]
	if c.Status != "True" || c.Reason != "Succeeded" || c.Results["run-modules"] != "* " || c.FailedStep != "" ||
		f.Status != "False" || f.FailedStep != "test" || len(f.Results["log"]) != summaryValueMax {
		t.Fatalf("tasks %+v", s.Tasks)
	}
	if c.Started != "2026-11-17T20:00:09Z" || c.Created != "2026-11-17T20:00:00Z" || c.Completed != "2026-11-17T20:03:00Z" {
		t.Fatalf("times %+v", c)
	}
}

// Same origin: the summary is written once, onto a finished run only, and
// reads back; the reconciliation gives every finished run without one its
// summary. Positive control: without the "already summarized" check the
// second WriteSummary writes again.
func TestSummaryIsWrittenOnceOntoFinishedRuns(t *testing.T) {
	ctx := context.Background()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList"},
		summaryRun("done-1", "True"), summaryRun("done-2", "False"), summaryRun("running-1", "Unknown"), summaryRun("new-1", ""),
		summaryTaskRun("done-1", "done-1-build", "build", "True", 0, map[string]string{"digest": "sha256:a"}),
	)
	for i, want := range []bool{true, false} {
		wrote, err := WriteSummary(ctx, dyn, "cicd", "done-1")
		if err != nil || wrote != want {
			t.Fatalf("write %d: %v %v", i, wrote, err)
		}
	}
	for _, name := range []string{"running-1", "new-1", "gone-1"} {
		if wrote, err := WriteSummary(ctx, dyn, "cicd", name); err != nil || wrote {
			t.Fatalf("%s: wrote=%v err=%v", name, wrote, err)
		}
	}
	got, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").Get(ctx, "done-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := ReadSummary(*got)
	if !ok || len(s.Tasks) != 1 || s.Tasks[0].Results["digest"] != "sha256:a" {
		t.Fatalf("read back %v %+v", ok, s)
	}
	pass, err := WriteMissingSummaries(ctx, dyn, "cicd")
	if err != nil || pass.Runs != 4 || pass.Written != 1 || pass.Failed != 0 {
		t.Fatalf("pass %+v %v", pass, err)
	}
	if again, _ := WriteMissingSummaries(ctx, dyn, "cicd"); again.Written != 0 {
		t.Fatalf("again %+v", again)
	}
}
