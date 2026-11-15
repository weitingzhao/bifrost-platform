package landingci

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Origin: 2026-11-15, the workers held every retained run with the Pipeline
// definition Tekton copies into each run's status (77% of the listed bytes,
// read by nothing) and were OOMKilled at 512Mi in STG. The runs this client
// keeps come without that copy and without managedFields; what its readers
// use is untouched.
func TestKeptRunsComeWithoutTheCopiedPipeline(t *testing.T) {
	run := drillCI(t, "ci-1", "True", time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC))
	setField(t, run, map[string]any{"tasks": []any{map[string]any{"name": "build"}}}, "status", "pipelineSpec")
	setField(t, run, []any{map[string]any{"manager": "controller"}}, "metadata", "managedFields")
	c, _ := fakeClient(run)
	items, err := c.pipelineRuns(context.Background(), "bifrost-ci-platform")
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	got := items[0].Object
	if _, has, _ := unstructured.NestedFieldNoCopy(got, "status", "pipelineSpec"); has {
		t.Fatal("the copied Pipeline is still in the run")
	}
	if _, has, _ := unstructured.NestedFieldNoCopy(got, "metadata", "managedFields"); has {
		t.Fatal("managedFields is still in the run")
	}
	conditions, _, _ := unstructured.NestedSlice(got, "status", "conditions")
	params, _, _ := unstructured.NestedSlice(got, "spec", "params")
	if len(conditions) != 1 || len(params) != 1 || items[0].GetName() != "ci-1" || items[0].GetLabels()["bifrost.io/repo"] != "bifrost-platform" {
		t.Fatalf("a field the readers use is gone: %+v", got)
	}
}
