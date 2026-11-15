package workactions

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. The release policy covers
// an apply only of objects the plan says the applier applied before. The
// summary carries that word only when the plan gives it as true: a plan of
// today, which gives ownership alone, reads as "cannot tell".
func TestSummaryCarriesAppliedBeforeOnlyWhenThePlanSaysSo(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        bool
	}{
		{"the plan says so", `,"applied_before":true`, true},
		{"the plan says not", `,"applied_before":false`, false},
		{"a plan of today: ownership alone", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := planObject("pipeline", "plan", "platform-api", "plan", nil)
			plan.SetAPIVersion("tekton.dev/v1")
			plan.SetKind("PipelineRun")
			plan.SetName("plan-test")
			plan.SetNamespace("delivery")
			plan.Object["status"] = map[string]any{
				"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
				"results": []any{
					map[string]any{"name": "policy", "value": "pass"},
					map[string]any{"name": "objects", "value": `[{"namespace":"delivery","kind":"Task","name":"x","ownership":"ssa"` + tc.field + `}]`},
				},
			}
			dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), plan)
			svc := &Service{
				Policy:  &actuationpolicy.Policy{Delivery: actuationpolicy.Delivery{Namespace: "delivery", Pipeline: "pipeline"}},
				Clients: Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
			}
			summary, err := svc.Summarize(context.Background(), "plan-test")
			if err != nil || !summary.Ready || len(summary.Objects) != 1 {
				t.Fatalf("summary = %+v err %v", summary, err)
			}
			if got := summary.Objects[0].AppliedBefore; got != tc.want {
				t.Fatalf("applied before = %v, want %v", got, tc.want)
			}
		})
	}
}
