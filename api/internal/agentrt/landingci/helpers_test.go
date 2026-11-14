package landingci

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

// Shared fixtures of the full-CI and failure tests: a client over a fake
// cluster, and a bifrost-ci-platform run with a Succeeded condition.

const factSHA1 = "1111111111111111111111111111111111111111"
const factSHA2 = "2222222222222222222222222222222222222222"

func drillService(t *testing.T, objs ...runtime.Object) (*Client, *dynfake.FakeDynamicClient) {
	t.Helper()
	d := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, objs...)
	s := &Client{Dynamic: func() (dynamic.Interface, error) { return d, nil }}
	return s, d
}

func drillCI(t *testing.T, name, status string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": name, "namespace": "cicd", "labels": map[string]any{"bifrost.io/repo": "bifrost-platform"}},
		"spec":   map[string]any{"params": []any{map[string]any{"name": "revision", "value": factSHA1}}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}},
	}}
	if err := unstructured.SetNestedField(o.Object, "bifrost-ci-platform", "spec", "pipelineRef", "name"); err != nil {
		t.Fatal(err)
	}
	o.SetCreationTimestamp(metav1.NewTime(at))
	return o
}

func setField(t *testing.T, obj *unstructured.Unstructured, value any, fields ...string) {
	t.Helper()
	if err := unstructured.SetNestedField(obj.Object, value, fields...); err != nil {
		t.Fatal(err)
	}
}

func fakeClient(objects ...*unstructured.Unstructured) (*Client, *dynfake.FakeDynamicClient) {
	objs := []runtime.Object{}
	for _, o := range objects {
		objs = append(objs, o)
	}
	d := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, objs...)
	return &Client{Dynamic: func() (dynamic.Interface, error) { return d, nil }}, d
}
