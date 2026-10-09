package workactions

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func planObject(pipeline, mode, trigger, modeLabel string, extra map[string]any) *unstructured.Unstructured {
	ref := map[string]any{"name": pipeline}
	for k, v := range extra {
		ref[k] = v
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"bifrost.io/trigger": trigger,
				"bifrost.io/mode":    modeLabel,
			},
		},
		"spec": map[string]any{
			"pipelineRef": ref,
			"params": []any{
				map[string]any{"name": "mode", "value": mode},
			},
		},
	}}
}

func TestPlanRunIdentity(t *testing.T) {
	ok := planObject("bifrost-apply-manifest", "plan", "platform-api", "plan", nil)
	if !planRunOK(ok, "bifrost-apply-manifest", "plan") {
		t.Fatal("a platform plan was not ready")
	}
	cases := []*unstructured.Unstructured{
		planObject("other", "plan", "platform-api", "plan", nil),
		planObject("bifrost-apply-manifest", "apply", "platform-api", "plan", nil),
		planObject("bifrost-apply-manifest", "plan", "someone-else", "plan", nil),
		planObject("bifrost-apply-manifest", "plan", "platform-api", "apply", nil),
		planObject("bifrost-apply-manifest", "plan", "platform-api", "plan", map[string]any{"resolver": "git"}),
		planObject("bifrost-apply-manifest", "plan", "platform-api", "plan", map[string]any{"params": []any{}}),
		planObject("bifrost-apply-manifest", "plan", "platform-api", "plan", map[string]any{"bundle": "x"}),
	}
	for i, obj := range cases {
		mode, _, _ := unstructured.NestedString(obj.Object, "spec", "params")
		_ = mode
		_, _, _, gotMode := paramsOf(obj)
		if planRunOK(obj, "bifrost-apply-manifest", gotMode) {
			t.Fatalf("case %d was treated as a platform plan", i)
		}
	}
}
