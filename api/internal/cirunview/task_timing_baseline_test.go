package cirunview

import (
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strings"
	"time"
)

func baselineTimingString(obj unstructured.Unstructured, path ...string) string {
	value, _, _ := unstructured.NestedString(obj.Object, path...)
	return value
}

func baselineTaskTiming(obj unstructured.Unstructured, run string) TimingTask {
	out := TimingTask{Name: obj.GetName(), Task: baselinePipelineTaskFromTaskRun(obj, run), Status: baselineTektonTaskRunState(obj),
		Created: obj.GetCreationTimestamp().UTC().Format(time.RFC3339), Finished: baselineTimingString(obj, "status", "completionTime"),
		Results: baselineTimingResults(&obj), Steps: []TimingStep{}}
	items, _, _ := unstructured.NestedSlice(obj.Object, "status", "steps")
	for _, item := range items {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		step := TimingStep{Name: name, Status: "waiting"}
		if state, ok := m["terminated"].(map[string]any); ok {
			step.Status = "terminated"
			step.Started, _ = state["startedAt"].(string)
			step.Finished, _ = state["finishedAt"].(string)
			if code, ok := state["exitCode"].(int64); ok {
				step.ExitCode = &code
				if code == 0 {
					step.Status = "succeeded"
				} else {
					step.Status = "failed"
				}
			}
		} else if state, ok := m["running"].(map[string]any); ok {
			step.Status = "running"
			step.Started, _ = state["startedAt"].(string)
		}
		out.Steps = append(out.Steps, step)
	}
	return out
}

func baselineTektonTaskRunState(obj unstructured.Unstructured) string {
	cond, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found || len(cond) == 0 {
		return "running"
	}
	m, ok := cond[0].(map[string]any)
	if !ok {
		return "running"
	}
	st := strings.ToLower(fmt.Sprint(m["status"]))
	reason := strings.ToLower(fmt.Sprint(m["reason"]))
	if st == "true" || reason == "succeeded" || reason == "completed" {
		return "succeeded"
	}
	if st == "false" || reason == "failed" || reason == "pipelinerunfailed" || reason == "taskrunfailed" {
		return "failed"
	}
	if reason == "running" || reason == "pending" || st == "unknown" {
		return "running"
	}
	return "running"
}

func baselinePipelineTaskFromTaskRun(obj unstructured.Unstructured, runName string) string {
	if labels := obj.GetLabels(); labels != nil {
		if t := labels["tekton.dev/pipelineTask"]; t != "" {
			return t
		}
	}
	name := obj.GetName()
	prefix := runName + "-"
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix)
	}
	return name
}

func baselinePipelineRunResults(run *unstructured.Unstructured) []Result {
	items, _, _ := unstructured.NestedSlice(run.Object, "status", "results")
	out := make([]Result, 0, len(items))
	for _, item := range items {
		result, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := result["name"].(string)
		value, ok := result["value"].(string)
		if !ok {
			raw, err := json.Marshal(result["value"])
			if err != nil {
				continue
			}
			value = string(raw)
		}
		out = append(out, Result{Name: name, Value: value})
	}
	return out
}

func baselineTimingResults(obj *unstructured.Unstructured) []Result {
	rows := baselinePipelineRunResults(obj)
	out := make([]Result, len(rows))
	for i, row := range rows {
		out[i] = Result(row)
	}
	return out
}
