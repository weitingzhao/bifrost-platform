package cirunview

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// The task summary of a finished run, kept on the run itself (2026-11-17,
// shared notes duty/from-ci-w1-2026-11-17-taskrun-readers-questioned.md).
// Tekton's controller reads every object again every ten hours; 8,352 of the
// 9,491 TaskRuns in cicd that night were of finished runs whose pods were long
// gone, kept two days only because a few readers wanted a few of their fields
// (ciscore, landingci's RunModules and RecentRuns, the timings page, the run
// list). The summary holds those fields; the run is kept two days and the
// summary goes with it, so no table is needed. Readers move to it one by one,
// each after reading both side by side; then a finished run's TaskRuns need
// not outlive its pods by long.
const SummaryAnnotation = "bifrost.io/tasks"

// SummaryVersion is the summary's shape.
const SummaryVersion = 1

// summaryValueMax cuts each kept result: the readers want short ones
// (run-modules, api-scope, outcome), and an annotation is not a log.
const summaryValueMax = 256

// TaskSummary is one TaskRun of the run, as its readers use it. Status and
// Reason are the Succeeded condition's, as Tekton wrote them; each reader
// keeps its own reading of them (TaskRunState, landingci's runStatus).
type TaskSummary struct {
	Task      string `json:"task"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Created   string `json:"created,omitempty"`
	Started   string `json:"started,omitempty"` // the first step's start (not status.startTime)
	Completed string `json:"completed,omitempty"`
	// FailedStep is the first step that ended with a non-zero exit code.
	FailedStep string            `json:"failed_step,omitempty"`
	Results    map[string]string `json:"results,omitempty"`
}

// RunSummary is the summary annotation's value.
type RunSummary struct {
	V     int           `json:"v"`
	Tasks []TaskSummary `json:"tasks"`
}

// Summarize reads the TaskRuns of one run into its summary, tasks in name order.
func Summarize(taskRuns []unstructured.Unstructured) RunSummary {
	out := RunSummary{V: SummaryVersion, Tasks: make([]TaskSummary, 0, len(taskRuns))}
	for _, tr := range taskRuns {
		t := TaskSummary{Task: tr.GetLabels()["tekton.dev/pipelineTask"], Name: tr.GetName()}
		if conds, _, _ := unstructured.NestedSlice(tr.Object, "status", "conditions"); len(conds) > 0 {
			if c, ok := conds[0].(map[string]any); ok {
				t.Status, _ = c["status"].(string)
				t.Reason, _ = c["reason"].(string)
			}
		}
		if c := tr.GetCreationTimestamp(); !c.IsZero() {
			t.Created = c.UTC().Format(time.RFC3339)
		}
		t.Completed, _, _ = unstructured.NestedString(tr.Object, "status", "completionTime")
		steps, _, _ := unstructured.NestedSlice(tr.Object, "status", "steps")
		var first time.Time
		for _, raw := range steps {
			st, _ := raw.(map[string]any)
			for _, state := range []string{"terminated", "running"} {
				m, _ := st[state].(map[string]any)
				if at, err := time.Parse(time.RFC3339, fmt.Sprint(m["startedAt"])); err == nil && (first.IsZero() || at.Before(first)) {
					first = at
				}
			}
			if term, ok := st["terminated"].(map[string]any); ok && t.FailedStep == "" {
				if code, ok := term["exitCode"].(int64); ok && code != 0 {
					t.FailedStep, _ = st["name"].(string)
				}
			}
		}
		if !first.IsZero() {
			t.Started = first.UTC().Format(time.RFC3339)
		}
		results, _, _ := unstructured.NestedSlice(tr.Object, "status", "results")
		for _, raw := range results {
			r, _ := raw.(map[string]any)
			name, _ := r["name"].(string)
			value, ok := r["value"].(string)
			if name == "" || !ok {
				continue
			}
			if len(value) > summaryValueMax {
				value = value[:summaryValueMax]
			}
			if t.Results == nil {
				t.Results = map[string]string{}
			}
			t.Results[name] = value
		}
		out.Tasks = append(out.Tasks, t)
	}
	sort.Slice(out.Tasks, func(i, j int) bool {
		if out.Tasks[i].Task != out.Tasks[j].Task {
			return out.Tasks[i].Task < out.Tasks[j].Task
		}
		return out.Tasks[i].Name < out.Tasks[j].Name
	})
	return out
}

// ReadSummary is a run's summary; false when it has none (not finished, not
// written yet, or written in a shape this code does not know).
func ReadSummary(run unstructured.Unstructured) (RunSummary, bool) {
	raw, ok := run.GetAnnotations()[SummaryAnnotation]
	if !ok {
		return RunSummary{}, false
	}
	var s RunSummary
	if err := json.Unmarshal([]byte(raw), &s); err != nil || s.V != SummaryVersion {
		return RunSummary{}, false
	}
	return s, true
}

// runFinished: the run's Succeeded condition is True or False.
func runFinished(run unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, raw := range conds {
		c, _ := raw.(map[string]any)
		if c["type"] == "Succeeded" {
			return c["status"] == "True" || c["status"] == "False"
		}
	}
	return false
}

// WriteSummary writes the summary of a finished run that has none. It says
// whether it wrote one; a run not finished, already summarized or gone is
// not an error.
func WriteSummary(ctx context.Context, dyn dynamic.Interface, namespace, name string) (bool, error) {
	run, err := dyn.Resource(pipelineRunGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return writeSummaryOf(ctx, dyn, namespace, *run)
}

func writeSummaryOf(ctx context.Context, dyn dynamic.Interface, namespace string, run unstructured.Unstructured) (bool, error) {
	if !runFinished(run) {
		return false, nil
	}
	if _, has := run.GetAnnotations()[SummaryAnnotation]; has {
		return false, nil
	}
	trs, err := dyn.Resource(taskRunGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + run.GetName()})
	if err != nil {
		return false, fmt.Errorf("taskruns of %s: %w", run.GetName(), err)
	}
	body, err := json.Marshal(Summarize(trs.Items))
	if err != nil {
		return false, err
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{SummaryAnnotation: string(body)}}})
	if err != nil {
		return false, err
	}
	if _, err := dyn.Resource(pipelineRunGVR).Namespace(namespace).Patch(ctx, run.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("annotate %s: %w", run.GetName(), err)
	}
	return true, nil
}

// SummaryPass is one reconciliation: every finished run of the namespace
// without a summary gets one. A run whose TaskRuns are gone gets a summary
// with no tasks, which says just that.
type SummaryPass struct {
	Runs, Written, Failed int
	FirstError            string
	// Forbidden: the identity may not annotate runs; the pass stopped at the first refusal.
	Forbidden bool
}

// WriteMissingSummaries is a SummaryPass over the namespace's runs, a page at a time.
func WriteMissingSummaries(ctx context.Context, dyn dynamic.Interface, namespace string) (SummaryPass, error) {
	var out SummaryPass
	opts := metav1.ListOptions{Limit: 100}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace(namespace).List(ctx, opts)
		if err != nil {
			return out, err
		}
		for _, run := range page.Items {
			out.Runs++
			wrote, err := writeSummaryOf(ctx, dyn, namespace, run)
			switch {
			case apierrors.IsForbidden(err):
				// This identity may not annotate runs (STG's has no patch on
				// pipelineruns in cicd, 2026-11-17): every other run would be
				// read and refused the same way. Stop the pass.
				out.Failed++
				out.FirstError = err.Error()
				out.Forbidden = true
				return out, nil
			case err != nil:
				out.Failed++
				if out.FirstError == "" {
					out.FirstError = err.Error()
				}
			case wrote:
				out.Written++
			}
		}
		if page.GetContinue() == "" {
			return out, nil
		}
		opts.Continue = page.GetContinue()
	}
}
