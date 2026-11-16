package cirunview

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strconv"
	"strings"
	"time"
)

type PipelineObservation struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Pipeline       string `json:"pipeline"`
	Revision       string `json:"revision,omitempty"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	StartTime      string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
}

func PipelineRunObservation(obj unstructured.Unstructured, pipelineName string) PipelineObservation {
	view := PipelineObservation{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Pipeline:  pipelineName,
		Status:    "Unknown",
	}
	if ref, ok, _ := unstructured.NestedString(obj.Object, "spec", "pipelineRef", "name"); ok && ref != "" {
		view.Pipeline = ref
	}
	if params, found, _ := unstructured.NestedSlice(obj.Object, "spec", "params"); found {
		for _, p := range params {
			if pm, ok := p.(map[string]any); ok {
				if name, _ := pm["name"].(string); name == "revision" {
					if val, _ := pm["value"].(string); val != "" {
						view.Revision = val
					}
				}
			}
		}
	}
	if view.Revision == "" {
		if labels := obj.GetLabels(); labels != nil {
			if rev, ok := labels["bifrost.io/revision"]; ok && rev != "" {
				view.Revision = rev
			}
		}
	}
	cond, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if found && len(cond) > 0 {
		if m, ok := cond[0].(map[string]any); ok {
			if st, ok := m["status"].(string); ok && st != "" {
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

func PipelineRunStartedAt(view PipelineObservation) int64 {
	if view.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, view.StartTime); err == nil {
			return t.UnixMilli()
		}
	}
	prefix := view.Pipeline + "-"
	if strings.HasPrefix(view.Name, prefix) {
		if sec, err := strconv.ParseInt(strings.TrimPrefix(view.Name, prefix), 10, 64); err == nil {
			return sec * 1000
		}
	}
	return 0
}
