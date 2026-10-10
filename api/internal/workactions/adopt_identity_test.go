package workactions

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

func readyPlan(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata": map[string]any{
			"name":      name,
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
}

func applyPolicy() *actuationpolicy.Policy {
	return &actuationpolicy.Policy{
		Delivery: actuationpolicy.Delivery{Namespace: "cicd", Pipeline: "apply-pipe", ServiceAccount: "sa"},
		Apply: actuationpolicy.Apply{
			Namespaces:       map[string]string{"bifrost-dev": "B"},
			Resources:        []actuationpolicy.Resource{{Kind: "ConfigMap"}},
			DaemonDeployment: "daemon",
		},
	}
}

func TestAdoptRequiresApprovalIdentity(t *testing.T) {
	const planID = "plan-1dfc9375-1791567153"
	att := actions.CreateAttempt{ApprovalID: "appr_0123456789abcdef", Attempt: 2}
	hash := strings.Repeat("ab", 32)
	name := NameForAttempt("apply-"+planID, att)
	foreign := readyPlan(name)
	foreign.SetAnnotations(map[string]string{
		actions.AnnApprovalID: "appr_ffffffffffffffff",
		actions.AnnAttempt:    "2",
		actions.AnnParamsHash: strings.Repeat("cd", 32),
	})
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, readyPlan(planID), foreign)
	var creates atomic.Int32
	dyn.PrependReactor("create", "pipelineruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		creates.Add(1)
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "tekton.dev", Resource: "pipelineruns"}, name)
	})
	svc := &Service{
		Policy:  applyPolicy(),
		Clients: Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
		Now:     func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
	}
	ctx := actions.WithParamsHash(actions.WithCreateAttempt(context.Background(), att.ApprovalID, att.Attempt), hash)
	_, err := svc.Apply(ctx, planID)
	if !actions.IsUncertain(err) || !strings.Contains(err.Error(), name) {
		t.Fatalf("GET adopt = %v, want unknown naming %s", err, name)
	}
	if creates.Load() != 0 {
		t.Fatalf("mismatch created a run: %d", creates.Load())
	}

	// AlreadyExists uses the same check. The first GET misses; create collides;
	// the second GET finds the foreign object.
	var gets atomic.Int32
	dyn.PrependReactor("get", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.(k8stesting.GetAction).GetName() != name {
			return false, nil, nil
		}
		if gets.Add(1) == 1 {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "tekton.dev", Resource: "pipelineruns"}, name)
		}
		return false, nil, nil
	})
	_, err = svc.Apply(ctx, planID)
	if !actions.IsUncertain(err) || !strings.Contains(err.Error(), "cicd/"+name) {
		t.Fatalf("AlreadyExists adopt = %v", err)
	}
	if creates.Load() != 1 {
		t.Fatalf("creates = %d, want the one colliding create", creates.Load())
	}

	foreign.SetAnnotations(map[string]string{
		actions.AnnApprovalID: att.ApprovalID,
		actions.AnnAttempt:    "2",
		actions.AnnParamsHash: hash,
	})
	if err := dyn.Tracker().Update(pipelineRunGVR, foreign, "cicd"); err != nil {
		t.Fatal(err)
	}
	before := creates.Load()
	_, err = svc.Apply(ctx, planID)
	if !actions.IsUncertain(err) || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "checked by hand") {
		t.Fatalf("matching annotations were adopted: %v", err)
	}
	if creates.Load() != before {
		t.Fatalf("matching annotations created a run: %d", creates.Load())
	}
}

func TestJobAdoptRequiresApprovalIdentity(t *testing.T) {
	att := actions.CreateAttempt{ApprovalID: "appr_0123456789abcdef", Attempt: 1}
	hash := strings.Repeat("ab", 32)
	ctx := actions.WithParamsHash(actions.WithCreateAttempt(context.Background(), att.ApprovalID, att.Attempt), hash)
	name := NameForAttempt("reconcile-manual", att)
	cs := fake.NewSimpleClientset(
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "reconcile", Namespace: "monitoring"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "monitoring",
			Annotations: map[string]string{actions.AnnApprovalID: "someone-else"},
		}},
	)
	svc := &Service{
		Policy: &actuationpolicy.Policy{Jobs: actuationpolicy.Jobs{Namespaces: map[string]string{"monitoring": "B"}}},
		Clients: Clients{
			Kube:    func() (kubernetes.Interface, error) { return cs, nil },
			Dynamic: func() (dynamic.Interface, error) { return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), nil },
		},
	}
	_, err := svc.CreateJobFromCronJob(ctx, "monitoring", "reconcile", "approval")
	if !actions.IsUncertain(err) || !strings.Contains(err.Error(), name) {
		t.Fatalf("job adopt = %v", err)
	}

	fresh := fake.NewSimpleClientset(&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "reconcile", Namespace: "monitoring"}})
	svc.Clients.Kube = func() (kubernetes.Interface, error) { return fresh, nil }
	out, err := svc.CreateJobFromCronJob(ctx, "monitoring", "reconcile", "approval")
	if err != nil {
		t.Fatal(err)
	}
	got, err := fresh.BatchV1().Jobs("monitoring").Get(context.Background(), out["job"].(string), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations[actions.AnnApprovalID] != att.ApprovalID || got.Annotations[actions.AnnAttempt] != "1" || got.Annotations[actions.AnnParamsHash] != hash {
		t.Fatalf("job annotations = %#v", got.Annotations)
	}
	if got.Annotations["cronjob.kubernetes.io/instantiate"] != "manual" {
		t.Fatalf("instantiate annotation = %#v", got.Annotations)
	}
}

func TestProbeAdoptRequiresApprovalIdentity(t *testing.T) {
	att := actions.CreateAttempt{ApprovalID: "appr_0123456789abcdef", Attempt: 1}
	hash := strings.Repeat("ef", 32)
	ctx := actions.WithParamsHash(actions.WithCreateAttempt(context.Background(), att.ApprovalID, att.Attempt), hash)
	name := NameForAttempt("probe", att)
	cs := fake.NewSimpleClientset(&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "monitoring",
		Annotations: map[string]string{
			actions.AnnApprovalID: att.ApprovalID,
			actions.AnnAttempt:    "1",
			actions.AnnParamsHash: strings.Repeat("00", 32),
		},
	}})
	svc := &Service{
		Policy: &actuationpolicy.Policy{Probe: actuationpolicy.Probe{
			Namespaces: map[string]string{"monitoring": "B"},
			Images:     []string{"docker.io/example/curl:1"},
		}},
		Clients: Clients{Kube: func() (kubernetes.Interface, error) { return cs, nil }},
	}
	_, err := svc.Probe(ctx, "monitoring", "example/curl:1", []string{"echo"}, []string{"--password", "SYNTHETIC_SECRET"}, "", 30, "approval")
	if !actions.IsUncertain(err) || !strings.Contains(err.Error(), "monitoring/"+name) {
		t.Fatalf("probe adopt = %v", err)
	}
}
