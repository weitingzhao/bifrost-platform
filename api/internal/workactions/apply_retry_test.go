package workactions

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

// TD-276: a plan whose apply failed must be appliable again. Two Apply calls
// for one ready plan create two distinct runs, both labelled with the plan.
func TestApplyTwiceCreatesTwoRuns(t *testing.T) {
	const planID = "plan-1dfc9375-1791567153"
	plan := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata": map[string]any{
			"name":      planID,
			"namespace": "cicd",
			"labels": map[string]any{
				"bifrost.io/trigger": "platform-api",
				"bifrost.io/mode":    "plan",
			},
		},
		"spec": map[string]any{
			"pipelineRef": map[string]any{"name": "apply-pipe"},
			"params": []any{
				map[string]any{"name": "mode", "value": "plan"},
				map[string]any{"name": "repo", "value": "example-repo"},
				map[string]any{"name": "path", "value": "k8s/app"},
				map[string]any{"name": "commit", "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
		},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
			"results": []any{
				map[string]any{"name": "policy", "value": "pass"},
				map[string]any{"name": "objects", "value": `[{"namespace":"bifrost-dev","kind":"ConfigMap","name":"x","tier":"B"}]`},
			},
		},
	}}
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, plan)
	now := time.Unix(1791567200, 0).UTC()
	svc := &Service{
		Policy: &actuationpolicy.Policy{
			Delivery: actuationpolicy.Delivery{Namespace: "cicd", Pipeline: "apply-pipe", ServiceAccount: "sa"},
			Apply: actuationpolicy.Apply{
				Namespaces:       map[string]string{"bifrost-dev": "B"},
				Resources:        []actuationpolicy.Resource{{Kind: "ConfigMap"}},
				DaemonDeployment: "daemon",
			},
		},
		Clients: Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
		Now:     func() time.Time { return now },
	}
	first, err := svc.Apply(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(90 * time.Second)
	second, err := svc.Apply(context.Background(), planID)
	if err != nil {
		t.Fatalf("second apply of the same plan: %v", err)
	}
	a, b := first["apply_run"].(string), second["apply_run"].(string)
	if a == "" || a == b {
		t.Fatalf("apply runs %q and %q, want two distinct names", a, b)
	}
	list, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").List(context.Background(), metav1.ListOptions{LabelSelector: "bifrost.io/plan=" + planID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("runs labelled with the plan = %d, want 2", len(list.Items))
	}
}

func TestApplyRunNameFitsALabel(t *testing.T) {
	long := "plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	name := applyRunName(long, time.Unix(1791567200, 0))
	if len(name) > 63 {
		t.Fatalf("name %q is %d bytes", name, len(name))
	}
	if name[len(name)-11:] != "-1791567200" {
		t.Fatalf("name %q lost the attempt suffix", name)
	}
}
