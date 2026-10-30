package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/yaml"
)

// Kubernetes responses, not ledger records: the real run constructor and
// ObserveRun validate every child reference before the controller sees it.
func TestB5HObservationProductionContract(t *testing.T) {
	t.Parallel()
	in := delivery.NamedDeliverInput{Batch: 1, Try: 1, IntentSeq: 7, P: commitA, U: commitB, Infra: commitA}
	for _, status := range []string{"Succeeded", "Failed"} {
		o := b5hObservation(t, in, status, "fixture-uid", "d", "f")
		if err := o.CheckInputs(delivery.STGContent{P: in.P, U: in.U, Infra: in.Infra}); err != nil {
			t.Fatal(err)
		}
		if status == "Succeeded" {
			if _, err := o.ExternalContent(); err != nil {
				t.Fatal(err)
			}
		}
		if o.Identity.UID != "fixture-uid" || len(o.Children) < 6 {
			t.Fatal("missing actual run/child identities", o.Identity)
		}
	}
}

func b5hObservation(t *testing.T, in delivery.NamedDeliverInput, status, uid, apiDigest, consoleDigest string) delivery.STGRunObservation {
	t.Helper()
	data, err := os.ReadFile("../../delivery/testdata/stg_delivery_pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data, err = yaml.YAMLToJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := unstructured.UnstructuredJSONScheme.Decode(data, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pipe := decoded.(*unstructured.Unstructured)
	run, err := delivery.PrepareNamedDeliverRun(in, pipe)
	if err != nil {
		t.Fatal(err)
	}
	run.SetUID(types.UID(uid))
	run.SetResourceVersion("1")
	state := map[string]string{"Succeeded": "True", "Failed": "False", "Running": "Unknown"}[status]
	cond := []any{map[string]any{"type": "Succeeded", "status": state}}
	defs, _, _ := unstructured.NestedSlice(pipe.Object, "spec", "tasks")
	run.Object["status"] = map[string]any{"conditions": cond, "results": []any{}, "pipelineSpec": map[string]any{"tasks": defs}}
	refs := []any{}
	objects := []runtime.Object{run}
	ctl := true
	for _, v := range defs {
		def := v.(map[string]any)
		name := def["name"].(string)
		ref, ok := def["taskRef"].(map[string]any)
		if !ok {
			continue
		}
		child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "spec": map[string]any{"taskRef": ref}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}, "results": []any{}}}}
		child.SetName("task-" + name)
		child.SetNamespace("cicd")
		child.SetUID(types.UID("uid-" + name))
		child.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "tekton.dev/v1", Kind: "PipelineRun", Name: run.GetName(), UID: run.GetUID(), Controller: &ctl}})
		params, results := []any{}, []any{}
		commits := map[string]string{"clone-platform": in.P, "clone-ui": in.U, "clone-infra": in.Infra}
		repos := map[string]string{"clone-platform": "bifrost-platform", "clone-ui": "bifrost-ui", "clone-infra": "bifrost-trade-infra"}
		if commit := commits[name]; commit != "" {
			params = append(params, map[string]any{"name": "url", "value": "http://gitea.cicd.svc.cluster.local:3000/bifrost/" + repos[name] + ".git"})
			results = append(results, map[string]any{"name": "commit", "value": commit})
		}
		if name == "build-platform-api" || name == "build-platform-console" {
			d := "sha256:" + strings.Repeat(apiDigest, 64)
			if name == "build-platform-console" {
				d = "sha256:" + strings.Repeat(consoleDigest, 64)
			}
			results = append(results, map[string]any{"name": "digest", "value": d})
		}
		if name == "gitops-sync" {
			for key, value := range map[string]string{"application": "bifrost-platform-stg", "apiDigest": "sha256:" + strings.Repeat(apiDigest, 64), "consoleDigest": "sha256:" + strings.Repeat(consoleDigest, 64), "deliverRun": run.GetName()} {
				params = append(params, map[string]any{"name": key, "value": value})
			}
		}
		child.Object["spec"].(map[string]any)["params"] = params
		child.Object["status"].(map[string]any)["results"] = results
		refs = append(refs, map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "name": child.GetName(), "pipelineTaskName": name})
		objects = append(objects, child)
	}
	run.Object["status"].(map[string]any)["childReferences"] = refs
	client := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
	adapter := &delivery.STGReleaseAdapter{Dynamic: func() (dynamic.Interface, error) { return client, nil }}
	obs, err := adapter.ObserveRun(context.Background(), delivery.STGRunIdentity{Namespace: "cicd", Name: run.GetName(), UID: run.GetUID()})
	if err != nil {
		t.Fatal(err)
	}
	return obs
}
