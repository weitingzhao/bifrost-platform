package gatecluster

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// The TaskRuns of the namespace, kept for the display reads of what a run did
// (a run's steps, a pipeline's timings; bus step 2, 2026-11-18: each page poll
// listed them, up to 296 LISTs in 10 minutes). Only a cache-reading api process
// watches them (Gate.WatchTasks). What gates a verdict keeps its own read.

// TaskRunGVR is Tekton's TaskRun.
var TaskRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}

// The labels a display read uses of a TaskRun.
const (
	labelTaskRunOf = "tekton.dev/pipelineRun"
	labelTask      = "tekton.dev/pipelineTask"
)

// TaskResult is one result of a TaskRun.
type TaskResult struct{ Name, Value string }

// TaskStep is one step of a TaskRun, as status.steps says it.
type TaskStep struct {
	Name                  string
	Running, Terminated   bool
	StartedAt, FinishedAt string
	ExitCode              int64
	HasExit               bool
}

// TaskRun is a TaskRun as the cache keeps it: what the display reads show and
// nothing more (the copy of the Task in status.taskSpec is most of a listed
// one: 117 MB for 9,799 TaskRuns, 8 MB of steps and results, 2026-11-18).
type TaskRun struct {
	Name, Namespace, PipelineRun, Task string
	Created                            time.Time
	StartTime, CompletionTime          string
	Status, Reason                     string
	Results                            []TaskResult
	Steps                              []TaskStep
}

// TaskRunOf takes from a TaskRun what the cache keeps.
func TaskRunOf(obj *unstructured.Unstructured) TaskRun {
	l := obj.GetLabels()
	t := TaskRun{Name: obj.GetName(), Namespace: obj.GetNamespace(), PipelineRun: l[labelTaskRunOf], Task: l[labelTask], Created: obj.GetCreationTimestamp().Time}
	t.StartTime, _, _ = unstructured.NestedString(obj.Object, "status", "startTime")
	t.CompletionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if len(conds) > 0 {
		if m, ok := conds[0].(map[string]any); ok {
			t.Status, _ = m["status"].(string)
			t.Reason, _ = m["reason"].(string)
		}
	}
	results, _, _ := unstructured.NestedSlice(obj.Object, "status", "results")
	for _, r := range results {
		m, _ := r.(map[string]any)
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		t.Results = append(t.Results, TaskResult{name, value})
	}
	steps, _, _ := unstructured.NestedSlice(obj.Object, "status", "steps")
	for _, s := range steps {
		m, _ := s.(map[string]any)
		st := TaskStep{}
		st.Name, _ = m["name"].(string)
		if x, ok := m["terminated"].(map[string]any); ok {
			st.Terminated = true
			st.StartedAt, _ = x["startedAt"].(string)
			st.FinishedAt, _ = x["finishedAt"].(string)
			st.ExitCode, st.HasExit = x["exitCode"].(int64)
		} else if x, ok := m["running"].(map[string]any); ok {
			st.Running = true
			st.StartedAt, _ = x["startedAt"].(string)
		}
		t.Steps = append(t.Steps, st)
	}
	return t
}

// Object is the TaskRun as an object holding only what the cache keeps, so a
// reader written for listed TaskRuns reads it unchanged.
func (t TaskRun) Object() unstructured.Unstructured {
	labels := map[string]any{}
	if t.PipelineRun != "" {
		labels[labelTaskRunOf] = t.PipelineRun
	}
	if t.Task != "" {
		labels[labelTask] = t.Task
	}
	meta := map[string]any{"name": t.Name, "namespace": t.Namespace, "labels": labels}
	if !t.Created.IsZero() {
		meta["creationTimestamp"] = t.Created.UTC().Format(time.RFC3339)
	}
	status := map[string]any{}
	if t.StartTime != "" {
		status["startTime"] = t.StartTime
	}
	if t.CompletionTime != "" {
		status["completionTime"] = t.CompletionTime
	}
	if t.Status != "" || t.Reason != "" {
		status["conditions"] = []any{map[string]any{"type": "Succeeded", "status": t.Status, "reason": t.Reason}}
	}
	if len(t.Results) > 0 {
		out := make([]any, len(t.Results))
		for i, r := range t.Results {
			out[i] = map[string]any{"name": r.Name, "value": r.Value}
		}
		status["results"] = out
	}
	if len(t.Steps) > 0 {
		out := make([]any, len(t.Steps))
		for i, s := range t.Steps {
			m := map[string]any{"name": s.Name}
			switch {
			case s.Terminated:
				x := map[string]any{"startedAt": s.StartedAt, "finishedAt": s.FinishedAt}
				if s.HasExit {
					x["exitCode"] = s.ExitCode
				}
				m["terminated"] = x
			case s.Running:
				m["running"] = map[string]any{"startedAt": s.StartedAt}
			}
			out[i] = m
		}
		status["steps"] = out
	}
	return unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "metadata": meta, "status": status}}
}

// TasksSynced says the TaskRun cache answers: listed in full at least once,
// and the last list or watch did not fail.
func (g *Gate) TasksSynced() bool { return g.tasksSynced.Load() }

// TaskObjects is the TaskRuns of one PipelineRun, by name.
func (g *Gate) TaskObjects(run string) []unstructured.Unstructured {
	g.taskMu.RLock()
	kept := g.tasksByRun[run]
	out := make([]unstructured.Unstructured, 0, len(kept))
	names := make([]string, 0, len(kept))
	for n := range kept {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, kept[n].Object())
	}
	g.taskMu.RUnlock()
	return out
}

func (g *Gate) keepTask(t TaskRun) {
	g.taskMu.Lock()
	defer g.taskMu.Unlock()
	if prev, ok := g.taskRunOf[t.Name]; ok && prev != t.PipelineRun {
		delete(g.tasksByRun[prev], t.Name)
	}
	g.taskRunOf[t.Name] = t.PipelineRun
	if g.tasksByRun[t.PipelineRun] == nil {
		g.tasksByRun[t.PipelineRun] = map[string]TaskRun{}
	}
	g.tasksByRun[t.PipelineRun][t.Name] = t
}

func (g *Gate) dropTask(name string) {
	g.taskMu.Lock()
	defer g.taskMu.Unlock()
	if run, ok := g.taskRunOf[name]; ok {
		delete(g.tasksByRun[run], name)
		if len(g.tasksByRun[run]) == 0 {
			delete(g.tasksByRun, run)
		}
		delete(g.taskRunOf, name)
	}
}

// listTasks replaces the cache with a full list, a page at a time, and says
// the list's resourceVersion.
func (g *Gate) listTasks(ctx context.Context) (string, error) {
	dyn, err := g.Dynamic()
	if err != nil {
		return "", err
	}
	res := dyn.Resource(TaskRunGVR).Namespace(g.Namespace)
	seen := map[string]bool{}
	cursors := map[string]bool{}
	options := metav1.ListOptions{Limit: 100}
	var rv string
	for {
		page, err := res.List(ctx, options)
		if err != nil {
			return "", err
		}
		for i := range page.Items {
			t := TaskRunOf(&page.Items[i])
			page.Items[i] = unstructured.Unstructured{}
			seen[t.Name] = true
			g.keepTask(t)
		}
		rv = page.GetResourceVersion()
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			break
		}
		if cursors[options.Continue] {
			return "", fmt.Errorf("task run list repeated a cursor")
		}
		cursors[options.Continue] = true
	}
	g.taskMu.RLock()
	var gone []string
	for name := range g.taskRunOf {
		if !seen[name] {
			gone = append(gone, name)
		}
	}
	g.taskMu.RUnlock()
	for _, name := range gone {
		g.dropTask(name)
	}
	return rv, nil
}

// runTasks lists the TaskRuns and watches them from there, as runRuns does for
// the runs; any failure marks the cache unsynced until a list succeeds.
func (g *Gate) runTasks(ctx context.Context) {
	g.followTaskRuns(ctx, "task runs", "", g.listTasks, func(t watch.EventType, obj *unstructured.Unstructured) {
		if t == watch.Deleted {
			g.dropTask(obj.GetName())
			return
		}
		g.keepTask(TaskRunOf(obj))
	}, &g.tasksSynced)
}

// followTaskRuns is the list-then-watch of TaskRuns under a label selector.
func (g *Gate) followTaskRuns(ctx context.Context, what, selector string, listAll func(context.Context) (string, error),
	observe func(watch.EventType, *unstructured.Unstructured), synced *atomic.Bool) {
	resync := g.ResyncEvery
	if resync <= 0 {
		resync = 30 * time.Minute
	}
	list := true
	var rv string
	var listed time.Time
	for ctx.Err() == nil {
		if list {
			v, err := listAll(ctx)
			if err != nil {
				synced.Store(false)
				metricErrors.Add(1)
				g.logf("bus gate-cluster: list %s: %v", what, err)
				if !g.sleep(ctx) {
					return
				}
				continue
			}
			rv, list, listed = v, false, g.now()
			synced.Store(true)
		}
		dyn, err := g.Dynamic()
		var w watch.Interface
		if err == nil {
			w, err = dyn.Resource(TaskRunGVR).Namespace(g.Namespace).Watch(ctx, metav1.ListOptions{
				LabelSelector: selector, ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: watchTimeout()})
		}
		if err != nil {
			synced.Store(false)
			metricRestarts.Add(1)
			if !apierrors.IsGone(err) {
				g.logf("bus gate-cluster: watch %s: %v", what, err)
			}
			list = true
			if !g.sleep(ctx) {
				return
			}
			continue
		}
		rv, err = consume(ctx, w, rv, observe)
		if ctx.Err() != nil {
			return
		}
		metricRestarts.Add(1)
		if err != nil {
			if !errors.Is(err, errGone) {
				g.logf("bus gate-cluster: watch %s ended: %v", what, err)
			}
			synced.Store(false)
			list = true
		}
		if g.now().Sub(listed) >= resync {
			list = true
		}
	}
}
