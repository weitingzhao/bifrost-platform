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

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// PipelineRunGVR is Tekton's PipelineRun.
var PipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}

// The labels of a run the gate reads.
const (
	labelPipeline = "tekton.dev/pipeline"
	labelRevision = "bifrost.io/revision"
	// LabelCause names the bus event a run was started for (ev-<n>): whoever
	// starts a run in answer to an event labels it so, and the run's
	// ci.run.started and ci.run.finished carry that event as their cause, so
	// the loop event → run → result can be followed (2026-11-17: of a day's
	// 2,464 bus events none had a cause).
	LabelCause = "bifrost.io/cause"
	// LabelCauseEnv is the environment whose ledger the cause's number is in.
	LabelCauseEnv = "bifrost.io/cause-env"
)

// runFacts is what the gate remembers of a run between events: a few
// strings, so 1600 retained runs cost the workers kilobytes, not the
// megabytes an informer's cache of whole runs would.
type runFacts struct {
	pipeline, commit string
	// cause is the ledger event the run answers (its LabelCause), 0 if none;
	// causeEnv the environment whose ledger that number is in.
	cause    int64
	causeEnv string
	// status is the Succeeded condition's: "" or Unknown while it runs,
	// True or False once it finished.
	status, reason string
	// run is what the read cache answers of it (cache.go).
	run Run
}

func (f runFacts) finished() bool { return f.status == "True" || f.status == "False" }

func factsOf(run *unstructured.Unstructured) runFacts { return factsFrom(RunOf(run)) }

// factsFrom is what the gate reads of a run: the pipeline label, the commit
// (the revision label, else the revision param, when either is a full SHA),
// and the Succeeded condition.
func factsFrom(r Run) runFacts {
	f := runFacts{pipeline: r.Labels[labelPipeline], status: r.Status, reason: r.Reason, run: r}
	if c := r.Labels[labelRevision]; sha40.MatchString(c) {
		f.commit = c
	} else if v := r.Params["revision"]; sha40.MatchString(v) {
		f.commit = v
	}
	if n, ok := relay.ParseEventID(r.Labels[LabelCause]); ok {
		f.cause = n
		f.causeEnv = r.Labels[LabelCauseEnv]
	}
	return f
}

// causeOf is the run's cause in this gate's ledger: only when the label names
// this environment (PROD and STG watch the same runs; an event number means
// one ledger's event).
func (g *Gate) causeOf(f runFacts) int64 {
	if f.cause == 0 || g.Env == "" || f.causeEnv != g.Env {
		return 0
	}
	return f.cause
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
	g.append(ctx, RunStream(run), TypeRunStarted, g.causeOf(f), func(lastType string, _ map[string]any) (map[string]any, bool) {
		return runBody(run, f, ChangeCreated), lastType == ""
	})
}

// finished appends ci.run.finished once per run: not when the run's
// stream's last event already is one. Once the ledger holds it, OnFinished
// is told.
func (g *Gate) finished(ctx context.Context, run string, f runFacts) {
	agrees := g.append(ctx, RunStream(run), TypeRunFinished, g.causeOf(f), func(lastType string, _ map[string]any) (map[string]any, bool) {
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
	if agrees && g.OnFinished != nil {
		g.OnFinished(run, f.pipeline)
	}
}

// observeRun takes a run the gate sees, from a list (listed) or a watch
// event, and says what changed against what it knew. A run it did not know
// is new, unless this is the first list (history is not replayed at
// startup); a run it knew unfinished that is finished now finished. A
// cache-only gate (CacheOnly) only remembers it.
func (g *Gate) observeRun(ctx context.Context, r Run, first bool) {
	name := r.Name
	if !nameRE.MatchString(name) {
		return
	}
	f := factsFrom(r)
	g.mu.Lock()
	prev, known := g.runs[name]
	g.runs[name] = f
	g.mu.Unlock()
	if first || g.CacheOnly {
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
			r := RunOf(&page.Items[i])
			page.Items[i] = unstructured.Unstructured{}
			seen[r.Name] = true
			g.observeRun(ctx, r, first)
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
	g.mu.Lock()
	for name := range g.runs {
		if !seen[name] {
			delete(g.runs, name)
		}
	}
	g.mu.Unlock()
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
			g.mu.Lock()
			delete(g.runs, obj.GetName())
			g.mu.Unlock()
			return
		}
		g.observeRun(ctx, RunOf(obj), false)
	}
}
