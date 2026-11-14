package landingci

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Failure reads what a red landing CI run failed on (R5:5.7-10), for the
// next round of the task whose commit it tested: the run's failed TaskRuns
// (its childReferences), each with its Succeeded condition's message and,
// when Logs can read it, the end of the log of its first step that exited
// non-zero. A run whose TaskRuns name no failure is told by its own
// condition's message. Reads only; it judges nothing.
func (c *Client) Failure(ctx context.Context, run string) (landing.CIFailure, error) {
	f := landing.CIFailure{Run: run, TaskRuns: []landing.CIFailedTask{}}
	res, err := c.resource()
	if err != nil {
		return f, err
	}
	item, err := res.Get(ctx, run, metav1.GetOptions{})
	if err != nil {
		return f, err
	}
	f.Pipeline, _, _ = unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
	d, err := c.Dynamic()
	if err != nil {
		return f, err
	}
	children, _, _ := unstructured.NestedSlice(item.Object, "status", "childReferences")
	for _, value := range children {
		if len(f.TaskRuns) == landing.CIFailedTasksMax {
			break
		}
		child, _ := value.(map[string]any)
		name, _ := child["name"].(string)
		if child["kind"] != "TaskRun" || name == "" {
			continue
		}
		task, err := d.Resource(pipelineRunGVR.GroupVersion().WithResource("taskruns")).Namespace(c.namespace()).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return f, fmt.Errorf("landing CI TaskRun %s: %w", name, err)
		}
		if runStatus(*task) != "Failed" {
			continue
		}
		t := landing.CIFailedTask{Name: name, Message: cutFront(conditionMessage(*task), landing.CIFailureMessageBytes)}
		t.PipelineTask, _ = child["pipelineTaskName"].(string)
		pod, _, _ := unstructured.NestedString(task.Object, "status", "podName")
		if step, container := failedStep(*task); step != "" {
			t.Step = step
			if c.Logs != nil && pod != "" && container != "" {
				if log, err := c.Logs(ctx, pod, container); err == nil {
					t.LogTail = cutBack(strings.TrimRight(log, " \t\r\n"), landing.CIFailureLogBytes)
				}
			}
		}
		f.TaskRuns = append(f.TaskRuns, t)
	}
	if len(f.TaskRuns) == 0 {
		f.TaskRuns = append(f.TaskRuns, landing.CIFailedTask{Name: run, Message: cutFront(conditionMessage(*item), landing.CIFailureMessageBytes)})
	}
	return f, nil
}

// conditionMessage is the message of an object's Succeeded condition.
func conditionMessage(item unstructured.Unstructured) string {
	conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	for _, cond := range conds {
		if m, _ := cond.(map[string]any); m["type"] == "Succeeded" {
			msg, _ := m["message"].(string)
			return msg
		}
	}
	return ""
}

// failedStep is a TaskRun's first step that terminated with a non-zero exit
// code: its name and container.
func failedStep(task unstructured.Unstructured) (string, string) {
	steps, _, _ := unstructured.NestedSlice(task.Object, "status", "steps")
	for _, value := range steps {
		step, _ := value.(map[string]any)
		code, found, _ := unstructured.NestedInt64(step, "terminated", "exitCode")
		if !found || code == 0 {
			continue
		}
		name, _ := step["name"].(string)
		container, _ := step["container"].(string)
		return name, container
	}
	return "", ""
}

// cutFront is the start of s, at most max bytes without splitting a
// character.
func cutFront(s string, max int) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// cutBack is the end of s, at most max bytes without splitting a character.
func cutBack(s string, max int) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) <= max {
		return s
	}
	cut := len(s) - max
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}
