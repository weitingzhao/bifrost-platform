package workactions

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

// Origin: #607 (2026-11-17): the card said executed while the apply run had
// failed. The service reads an apply run's own Succeeded condition: running
// is not done, True is ok, False carries the run's words, an unreadable run
// is an error and never "done".
func TestApplyRunOutcomeReadsTheRunsCondition(t *testing.T) {
	run := func(name, status, reason, message string) *unstructured.Unstructured {
		o := map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": name, "namespace": "delivery"}}
		if status != "" {
			o["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status, "reason": reason, "message": message}}}
		}
		return &unstructured.Unstructured{Object: o}
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}: "PipelineRunList"},
		run("fresh", "", "", ""), run("going", "Unknown", "Running", ""), run("good", "True", "Succeeded", ""),
		run("bad", "False", "Failed", "commit is not reachable from main"))
	svc := &Service{
		Policy:  &actuationpolicy.Policy{Delivery: actuationpolicy.Delivery{Namespace: "delivery", Pipeline: "pipeline"}},
		Clients: Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
	}
	ctx := context.Background()
	for name, want := range map[string][3]bool{"fresh": {false, false, false}, "going": {false, false, false}, "good": {true, true, false}, "bad": {true, false, false}} {
		done, ok, why, err := svc.ApplyRunOutcome(ctx, name)
		if err != nil || done != want[0] || ok != want[1] {
			t.Fatalf("%s: done %v ok %v why %q err %v", name, done, ok, why, err)
		}
	}
	if _, _, why, _ := svc.ApplyRunOutcome(ctx, "bad"); !strings.Contains(why, "not reachable from main") {
		t.Fatalf("the failure's words: %q", why)
	}
	if done, _, _, err := svc.ApplyRunOutcome(ctx, "absent"); err == nil || done {
		t.Fatalf("an unreadable run: done %v err %v", done, err)
	}
}
