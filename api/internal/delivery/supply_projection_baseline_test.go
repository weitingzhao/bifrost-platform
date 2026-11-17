//bifrost:owner delivery

package delivery

import (
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"sort"
	"testing"
	"time"
)

func baselineDockerfileCMFrom(cm *corev1.ConfigMap) DockerfileConfigMapView {
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	size := 0
	for _, k := range keys {
		size += len(cm.Data[k])
	}
	updated := cm.CreationTimestamp.UTC().Format(time.RFC3339)
	if len(cm.ManagedFields) > 0 {
		// resourceVersion changes on update; use it as a change indicator
		_ = cm.ResourceVersion
	}
	return DockerfileConfigMapView{
		Name:            cm.Name,
		Namespace:       cm.Namespace,
		Present:         true,
		ResourceVersion: cm.ResourceVersion,
		UpdatedAt:       updated,
		FileKeys:        keys,
		ApproxBytes:     size,
	}
}

func baselineTaskRunSummaryFrom(obj unstructured.Unstructured) SupplyChainTaskRunView {
	view := SupplyChainTaskRunView{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Task:      "",
		Status:    "Unknown",
	}
	if ref, ok, _ := unstructured.NestedString(obj.Object, "spec", "taskRef", "name"); ok {
		view.Task = ref
	}
	if act, ok := obj.GetLabels()["bifrost.io/supply-chain"]; ok {
		view.Actuation = act
	}
	cond, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if found && len(cond) > 0 {
		if m, ok := cond[0].(map[string]any); ok {
			if st, ok := m["status"].(string); ok {
				view.Status = st
			}
			if reason, ok := m["reason"].(string); ok {
				view.Reason = reason
			}
		}
	}
	if t, ok, _ := unstructured.NestedString(obj.Object, "status", "startTime"); ok {
		view.StartTime = t
	}
	if t, ok, _ := unstructured.NestedString(obj.Object, "status", "completionTime"); ok {
		view.CompletionTime = t
	}
	return view
}

func TestSupplyObservationOriginalParity(t *testing.T) {
	for _, data := range []map[string]string{nil, {}, {"z": "last", "a": "first"}, {"Dockerfile": "FROM scratch\n"}} {
		cm := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns", ResourceVersion: "42", CreationTimestamp: metav1.NewTime(time.Date(2026, 11, 16, 1, 2, 3, 0, time.UTC)), ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "manager"}}}, Data: data}
		if got, want := dockerfileCMFrom(&cm), baselineDockerfileCMFrom(&cm); !reflect.DeepEqual(got, want) {
			t.Fatalf("cm %#v != %#v", got, want)
		}
	}
	for _, raw := range []string{`{}`, `{"metadata":{"name":"run","namespace":"ns","labels":{"bifrost.io/supply-chain":"mirror-sync"}},"spec":{"taskRef":{"name":"task"}},"status":{"conditions":[{"status":"True","reason":"Succeeded"}],"startTime":"time","completionTime":"done"}}`, `{"spec":{"taskRef":{"name":3}},"status":{"conditions":[null,{"status":"False"}],"startTime":4}}`, `{"status":{"conditions":[{"status":"","reason":""}]}}`} {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		obj := unstructured.Unstructured{Object: m}
		if got, want := taskRunSummaryFrom(obj), baselineTaskRunSummaryFrom(obj); !reflect.DeepEqual(got, want) {
			t.Fatalf("task %#v != %#v", got, want)
		}
	}
}
