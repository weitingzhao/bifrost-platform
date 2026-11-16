package delivery

import (
	"encoding/json"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func baselinePipelineRunFromUnstructured(obj unstructured.Unstructured, pipelineName string) PipelineRunView {
	view := PipelineRunView{
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

func baselinePipelineRunStartedAt(view PipelineRunView) int64 {
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

func TestPipelineObservationOriginalParity(t *testing.T) {
	fixtures := []string{
		`{}`, `{"metadata":{"name":"ci-123","namespace":"ns","labels":{"bifrost.io/revision":"fallback"}}}`,
		`{"spec":{"pipelineRef":{"name":"actual"},"params":[null,3,{"name":"revision","value":"first"},{"name":"revision","value":""},{"name":"revision","value":"last"}]},"status":{"conditions":[{"status":"False","reason":"Failed"},{"status":"True"}],"startTime":"2026-11-16T01:02:03Z","completionTime":"bad"}}`,
		`{"spec":{"pipelineRef":{"name":4},"params":"wrong"},"status":{"conditions":["wrong"],"startTime":3}}`,
		`{"status":{"conditions":[{"status":"","reason":3}]}}`,
	}
	for _, fixture := range fixtures {
		var raw map[string]any
		if err := json.Unmarshal([]byte(fixture), &raw); err != nil {
			t.Fatal(err)
		}
		obj := unstructured.Unstructured{Object: raw}
		got, want := pipelineRunFromUnstructured(obj, "ci"), baselinePipelineRunFromUnstructured(obj, "ci")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %#v != %#v", fixture, got, want)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Fatal("JSON mismatch")
		}
	}
	for _, view := range []PipelineRunView{{}, {Name: "ci-123", Pipeline: "ci"}, {Name: "ci--1", Pipeline: "ci"}, {Name: "other-123", Pipeline: "ci"}, {Name: "ci-no", Pipeline: "ci"}, {Name: "ci-123", Pipeline: "ci", StartTime: "bad"}, {Name: "ci-123", Pipeline: "ci", StartTime: "2026-11-16T01:02:03.123Z"}} {
		if got, want := pipelineRunStartedAt(view), baselinePipelineRunStartedAt(view); got != want {
			t.Fatalf("%#v: %d != %d", view, got, want)
		}
	}
}
