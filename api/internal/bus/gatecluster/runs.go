package gatecluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// PipelineRunGVR is Tekton's PipelineRun.
var PipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}

// The labels of a run the gate reads.
const (
	labelPipeline = "tekton.dev/pipeline"
	labelRevision = "bifrost.io/revision"
)

// Slim is the run as the gate keeps it while it reads a page or a watch
// event: name, namespace, uid, resourceVersion, creationTimestamp, the two
// labels it reads, the revision param, and the Succeeded condition without
// its message. Everything else is left behind: status.pipelineSpec (77% of a
// listed run, measured 2026-11-15 on 1182 runs; the workers were OOMKilled
// at 512Mi listing them), status.childReferences, metadata.managedFields,
// annotations, the other params and conditions.
func Slim(run *unstructured.Unstructured) *unstructured.Unstructured {
	meta := map[string]any{}
	for _, k := range []string{"name", "namespace", "uid", "resourceVersion", "creationTimestamp"} {
		if v, ok, _ := unstructured.NestedFieldNoCopy(run.Object, "metadata", k); ok && v != nil {
			meta[k] = v
		}
	}
	labels := map[string]any{}
	for _, k := range []string{labelPipeline, labelRevision} {
		if v := run.GetLabels()[k]; v != "" {
			labels[k] = v
		}
	}
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	out := map[string]any{"apiVersion": run.GetAPIVersion(), "kind": run.GetKind(), "metadata": meta}
	params, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, p := range params {
		if m, ok := p.(map[string]any); ok && m["name"] == "revision" {
			if v, ok := m["value"].(string); ok {
				out["spec"] = map[string]any{"params": []any{map[string]any{"name": "revision", "value": v}}}
			}
		}
	}
	conds, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]any); ok && m["type"] == "Succeeded" {
			out["status"] = map[string]any{"conditions": []any{map[string]any{
				"type": "Succeeded", "status": m["status"], "reason": m["reason"]}}}
		}
	}
	return &unstructured.Unstructured{Object: out}
}

// runFacts is what the gate remembers of a run between events: a few
// strings, so 1600 retained runs cost the workers kilobytes, not the
// megabytes an informer's cache of whole runs would.
type runFacts struct {
	pipeline, commit string
	// status is the Succeeded condition's: "" or Unknown while it runs,
	// True or False once it finished.
	status, reason string
}

func (f runFacts) finished() bool { return f.status == "True" || f.status == "False" }

func factsOf(run *unstructured.Unstructured) runFacts {
	s := Slim(run)
	f := runFacts{pipeline: s.GetLabels()[labelPipeline]}
	if c := s.GetLabels()[labelRevision]; sha40.MatchString(c) {
		f.commit = c
	} else if params, _, _ := unstructured.NestedSlice(s.Object, "spec", "params"); len(params) == 1 {
		if v, _ := params[0].(map[string]any)["value"].(string); sha40.MatchString(v) {
			f.commit = v
		}
	}
	if conds, _, _ := unstructured.NestedSlice(s.Object, "status", "conditions"); len(conds) == 1 {
		m, _ := conds[0].(map[string]any)
		f.status, _ = m["status"].(string)
		f.reason, _ = m["reason"].(string)
	}
	return f
}

// finishedChange names how a finished run ended.
func finishedChange(f runFacts) string {
	switch {
	case f.status == "True":
		return ChangeSucceeded
	case strings.Contains(f.reason, "Cancel"):
		return ChangeCancelled
	case strings.Contains(f.reason, "Timeout") || strings.Contains(f.reason, "TimedOut"):
		return ChangeTimedOut
	}
	return ChangeFailed
}

func runBody(run string, f runFacts, change string) map[string]any {
	b := map[string]any{"subject": RunSubject(run, f.commit), "ref": RunRef(run), "change": change, "run": run}
	if f.pipeline != "" {
		b["pipeline"] = f.pipeline
	}
	if f.commit != "" {
		b["commit"] = f.commit
	}
	return b
}

// started appends ci.run.started unless the run's stream already holds an
// event (said before a restart, or its finish was said first).
func (g *Gate) started(ctx context.Context, run string, f runFacts) {
	g.append(ctx, RunStream(run), TypeRunStarted, func(lastType string, _ map[string]any) (map[string]any, bool) {
		return runBody(run, f, ChangeCreated), lastType == ""
	})
}

// finished appends ci.run.finished once per run: not when the run's
// stream's last event already is one.
func (g *Gate) finished(ctx context.Context, run string, f runFacts) {
	g.append(ctx, RunStream(run), TypeRunFinished, func(lastType string, _ map[string]any) (map[string]any, bool) {
		b := runBody(run, f, finishedChange(f))
		b["status"] = f.status
		if r := f.reason; r != "" {
			if len(r) > 64 {
				r = r[:64]
			}
			b["reason"] = r
		}
		return b, lastType != TypeRunFinished
	})
}

// observeRun takes a run the gate sees, from a list (listed) or a watch
// event, and says what changed against what it knew. A run it did not know
// is new, unless this is the first list (history is not replayed at
// startup); a run it knew unfinished that is finished now finished.
func (g *Gate) observeRun(ctx context.Context, obj *unstructured.Unstructured, first bool) {
	name := obj.GetName()
	if !nameRE.MatchString(name) {
		return
	}
	f := factsOf(obj)
	prev, known := g.runs[name]
	g.runs[name] = f
	if first {
		return
	}
	if !known {
		g.started(ctx, name, f)
	}
	if f.finished() && (!known || !prev.finished()) {
		g.finished(ctx, name, f)
	}
}

// listRuns lists the namespace's runs page by page (100 at a time), slims
// each, and observes it; it forgets runs the cluster no longer keeps. It
// returns the list's resourceVersion, where the watch starts.
func (g *Gate) listRuns(ctx context.Context, first bool) (string, error) {
	dyn, err := g.Dynamic()
	if err != nil {
		return "", err
	}
	res := dyn.Resource(PipelineRunGVR).Namespace(g.Namespace)
	seen := map[string]bool{}
	options := metav1.ListOptions{Limit: 100}
	cursors := map[string]bool{}
	var rv string
	for {
		page, err := res.List(ctx, options)
		if err != nil {
			return "", err
		}
		for i := range page.Items {
			s := Slim(&page.Items[i])
			page.Items[i] = unstructured.Unstructured{}
			seen[s.GetName()] = true
			g.observeRun(ctx, s, first)
		}
		rv = page.GetResourceVersion()
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			break
		}
		if cursors[options.Continue] {
			return "", fmt.Errorf("pipeline run pagination repeated a cursor")
		}
		cursors[options.Continue] = true
	}
	for name := range g.runs {
		if !seen[name] {
			delete(g.runs, name)
		}
	}
	return rv, nil
}

// errGone says the watch's resourceVersion is too old: list again.
var errGone = errors.New("the watch's resourceVersion is gone")

// consume reads one watch until it ends, observing each run; it returns the
// last resourceVersion seen, and errGone when the server says to list again.
func consume(ctx context.Context, w watch.Interface, rv string, observe func(watch.EventType, *unstructured.Unstructured)) (string, error) {
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return rv, ctx.Err()
		case ev, ok := <-w.ResultChan():
			if !ok {
				return rv, nil
			}
			if ev.Type == watch.Error {
				err := apierrors.FromObject(ev.Object)
				if apierrors.IsGone(err) || apierrors.IsResourceExpired(err) {
					return rv, errGone
				}
				return rv, err
			}
			obj, ok := ev.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			if v := obj.GetResourceVersion(); v != "" {
				rv = v
			}
			if ev.Type == watch.Bookmark {
				continue
			}
			observe(ev.Type, obj)
		}
	}
}

func (g *Gate) observeRunEvent(ctx context.Context) func(watch.EventType, *unstructured.Unstructured) {
	return func(t watch.EventType, obj *unstructured.Unstructured) {
		if t == watch.Deleted {
			delete(g.runs, obj.GetName())
			return
		}
		g.observeRun(ctx, Slim(obj), false)
	}
}
