package workactions

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

func TestPlanOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, namespace, kind, ownership string
		refused                          bool
		migrated                         int
	}{
		{"migration", "delivery", "Task", `,"ownership":"migrate"`, false, 1},
		{"other namespace", "app", "ConfigMap", `,"ownership":"migrate"`, true, 0},
		{"other kind", "delivery", "ConfigMap", `,"ownership":"migrate"`, true, 0},
		{"unknown", "delivery", "Task", `,"ownership":"future"`, true, 0},
		{"unreadable", "delivery", "Task", `,"ownership":42`, true, 0},
		{"legacy", "app", "ConfigMap", "", false, 0},
		{"ssa", "delivery", "Task", `,"ownership":"ssa"`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			plan := planObject("pipeline", "plan", "platform-api", "plan", nil)
			plan.SetAPIVersion("tekton.dev/v1")
			plan.SetKind("PipelineRun")
			plan.SetName("plan-test")
			plan.SetNamespace("delivery")
			plan.Object["status"] = map[string]any{
				"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
				"results": []any{
					map[string]any{"name": "policy", "value": "pass"},
					map[string]any{"name": "objects", "value": fmt.Sprintf(`[{"namespace":%q,"kind":%q,"name":"x"%s}]`, tc.namespace, tc.kind, tc.ownership)},
				},
			}
			dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), plan)
			svc := &Service{
				Policy: &actuationpolicy.Policy{
					Delivery: actuationpolicy.Delivery{Namespace: "delivery", Pipeline: "pipeline"},
					Apply: actuationpolicy.Apply{
						Namespaces:    map[string]string{"delivery": "C", "app": "B"},
						Resources:     []actuationpolicy.Resource{{Kind: "ConfigMap"}},
						CicdResources: []actuationpolicy.Resource{{Kind: "Task"}},
					},
				},
				Clients: Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
			}
			summary, err := svc.Summarize(ctx, "plan-test")
			if tc.name == "unreadable" {
				if err == nil {
					t.Fatal("unreadable ownership accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				wantOwnership := ""
				if tc.ownership != "" {
					wantOwnership = map[string]string{"migration": "migrate", "other namespace": "migrate", "other kind": "migrate", "unknown": "future", "ssa": "ssa"}[tc.name]
				}
				if len(summary.Objects) != 1 || summary.Objects[0].Ownership != wantOwnership {
					t.Fatalf("summary lost ownership: %+v", summary)
				}
				if !tc.refused && summary.Migrated != tc.migrated {
					t.Fatalf("summary migrated = %d", summary.Migrated)
				}
			}
			result, err := svc.Apply(ctx, "plan-test")
			if tc.refused {
				if err == nil {
					t.Fatal("apply accepted invalid ownership")
				}
				for _, action := range dyn.Actions() {
					if action.GetVerb() == "create" {
						t.Fatal("refused apply created a run")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result["migrated"] != tc.migrated {
				t.Fatalf("result: %+v", result)
			}
			if _, err := dyn.Resource(pipelineRunGVR).Namespace("delivery").Get(ctx, result["apply_run"].(string), metav1.GetOptions{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
