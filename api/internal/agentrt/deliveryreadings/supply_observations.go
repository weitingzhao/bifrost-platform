package deliveryreadings

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sort"
	"time"
)

type DockerfileConfigMapView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Present         bool     `json:"present"`
	ResourceVersion string   `json:"resource_version,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	FileKeys        []string `json:"file_keys,omitempty"`
	ApproxBytes     int      `json:"approx_bytes,omitempty"`
	Detail          string   `json:"detail,omitempty"`
}

type SupplyChainTaskRunView struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Task           string `json:"task"`
	Actuation      string `json:"actuation,omitempty"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	StartTime      string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
}

func DockerfileCMFrom(cm *corev1.ConfigMap) DockerfileConfigMapView {
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

func TaskRunSummaryFrom(obj unstructured.Unstructured) SupplyChainTaskRunView {
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
