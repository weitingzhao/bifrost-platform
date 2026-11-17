package gatecluster

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// 2026-11-17: head-release reads its runs from this cache, so the cache keeps
// what a delivery shipped (every *Revision param) beside the display params,
// and nothing else.
func TestCacheKeepsTheRevisions(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "bifrost-deliver-platform-1", "namespace": "cicd"},
		"spec": map[string]any{"pipelineRef": map[string]any{"name": "bifrost-deliver-platform"}, "params": []any{
			map[string]any{"name": "revision", "value": "a"},
			map[string]any{"name": "uiRevision", "value": "b"},
			map[string]any{"name": "infraRevision", "value": "c"},
			map[string]any{"name": "dockerfile", "value": "big"},
		}},
	}}
	r := RunOf(obj)
	if r.Params["revision"] != "a" || r.Params["uiRevision"] != "b" || r.Params["infraRevision"] != "c" {
		t.Fatalf("params %v", r.Params)
	}
	if _, ok := r.Params["dockerfile"]; ok {
		t.Fatal("the cache kept a param nothing reads")
	}
	params, _, _ := unstructured.NestedSlice(func() map[string]any { o := r.Object(); return o.Object }(), "spec", "params")
	if len(params) != 3 {
		t.Fatalf("object params %v", params)
	}
}
