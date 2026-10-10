package approvals

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

var pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}

// A create that is stored and then reported as a timeout must not be retried
// under a second name. The approval is unknown and the cluster has one run.
func TestCreateTimeoutDoesNotRequeue(t *testing.T) {
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
	var creates atomic.Int32
	dyn.PrependReactor("create", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ca := action.(k8stesting.CreateAction)
		obj := ca.GetObject().DeepCopyObject()
		if err := dyn.Tracker().Create(ca.GetResource(), obj, ca.GetNamespace()); err != nil {
			return true, nil, err
		}
		creates.Add(1)
		return true, nil, apierrors.NewTimeoutError("timeout", 1)
	})
	work := &workactions.Service{
		Policy: &actuationpolicy.Policy{
			Delivery: actuationpolicy.Delivery{Namespace: "cicd", Pipeline: "apply-pipe", ServiceAccount: "sa"},
			Apply: actuationpolicy.Apply{
				Namespaces:       map[string]string{"bifrost-dev": "B"},
				Resources:        []actuationpolicy.Resource{{Kind: "ConfigMap"}},
				DaemonDeployment: "daemon",
			},
		},
		Clients: workactions.Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
		Now:     func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
	}
	actions.RegisterExecutor("apply_manifest", func(ctx context.Context, params map[string]any) (any, error) {
		return work.Apply(ctx, params["plan_id"].(string))
	})

	svc := New(t.TempDir()+"/approvals", nil)
	c := svc.create(context.Background(), "s", "apply_manifest", "apply the plan", "", map[string]any{"plan_id": planID})
	if c.Status != 201 {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	out := svc.approve(context.Background(), c.Approval.ID, "console")
	if out.Body["status"] != StatusUnknown {
		t.Fatalf("approve = %d %v", out.Status, out.Body)
	}
	errText, _ := out.Body["error"].(string)
	if !strings.Contains(errText, "needs checking") {
		t.Fatalf("error = %q, want the outcome needs checking", errText)
	}
	got, _ := svc.find(c.Approval.ID)
	if got.Status != StatusUnknown || got.Execution == nil || got.Execution.Attempts != 1 || !got.Execution.NextAttemptAt.IsZero() {
		t.Fatalf("stored = %s %#v", got.Status, got.Execution)
	}
	if creates.Load() != 1 {
		t.Fatalf("creates = %d, want 1", creates.Load())
	}

	// The same attempt adopts the run the timed-out create already stored.
	ctx := actions.WithCreateAttempt(context.Background(), c.Approval.ID, got.Execution.Attempts)
	if _, err := work.Apply(ctx, planID); err != nil {
		t.Fatalf("adopt = %v", err)
	}
	if creates.Load() != 1 {
		t.Fatalf("adopt created again: creates=%d", creates.Load())
	}

	// Unknown is not claimable, including after a requeue backoff would have elapsed.
	svc.SetClock(func() time.Time { return time.Now().UTC().Add(2 * time.Hour) })
	svc.retryDue(context.Background())
	got, _ = svc.find(c.Approval.ID)
	if got.Status != StatusUnknown || creates.Load() != 1 {
		t.Fatalf("after retry = %s creates=%d", got.Status, creates.Load())
	}
	list, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").List(context.Background(), metav1.ListOptions{LabelSelector: "bifrost.io/mode=apply"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("apply runs = %d, want 1", len(list.Items))
	}
	want := workactions.NameForAttempt("apply-"+planID, actions.CreateAttempt{ApprovalID: c.Approval.ID, Attempt: 1})
	if list.Items[0].GetName() != want {
		t.Fatalf("run name = %q, want %q", list.Items[0].GetName(), want)
	}
	if strings.Contains(list.Items[0].GetName(), "1800000000") {
		t.Fatalf("run name %q still uses the wall clock", list.Items[0].GetName())
	}
}
