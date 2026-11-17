package gatecluster

import (
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The gate's run map is also a read cache (bus step 2, batch 2): the
// display reads of runs (delivery's pipeline runs, supply chain and release
// context) read it instead of listing every run of the namespace on each
// page poll. Nothing that gates a release, a CI verdict or a promotion reads
// it (bus/RULES.md §10 R3: those wait for a side-by-side with the CI line).

// The labels and params a display read uses; the cache keeps no other.
const labelRepo = "bifrost.io/repo"

var (
	displayLabels = []string{labelPipeline, labelRevision, labelRepo, LabelCause, LabelCauseEnv}
	displayParams = []string{"revision", "repo", "repo-url", "url"}
)

// keptParam says whether the cache keeps a param: the display ones, and every
// *Revision (uiRevision, infraRevision), which head-release reads to know
// what a delivery shipped (2026-11-17: its reads listed every run of its
// pipeline each pass, ~470 LISTs in 10 minutes).
func keptParam(name string) bool {
	for _, k := range displayParams {
		if k == name {
			return true
		}
	}
	return strings.HasSuffix(name, "Revision")
}

// Run is a run as the gate keeps it, for itself and for the read cache: what
// the gate and the display reads use of it and nothing more. Everything else
// is left behind: status.pipelineSpec (77% of a listed run, measured
// 2026-11-15 on 1182 runs; the workers were OOMKilled at 512Mi listing them),
// status.childReferences, metadata.managedFields, annotations, the
// condition's message, the other params, labels and conditions.
// A Run handed out is read-only; its maps are shared.
type Run struct {
	Name, Namespace string
	// Labels holds only displayLabels; Params only displayParams (string values).
	Labels map[string]string
	Params map[string]string
	// PipelineRef is spec.pipelineRef.name.
	PipelineRef string
	Created     time.Time
	// StartTime and CompletionTime are status.startTime and
	// status.completionTime as the cluster writes them (RFC 3339).
	StartTime, CompletionTime string
	// Status and Reason are the Succeeded condition's ("" while it has none).
	Status, Reason string
}

// Pipeline is the run's pipeline: its tekton.dev/pipeline label, else its
// pipelineRef.
func (r Run) Pipeline() string {
	if p := r.Labels[labelPipeline]; p != "" {
		return p
	}
	return r.PipelineRef
}

// Finished: the run has a completion time, or a Succeeded condition True or False.
func (r Run) Finished() bool {
	return r.CompletionTime != "" || r.Status == "True" || r.Status == "False"
}

// RunOf takes from a run what the cache keeps.
func RunOf(obj *unstructured.Unstructured) Run {
	r := Run{Name: obj.GetName(), Namespace: obj.GetNamespace(), Created: obj.GetCreationTimestamp().Time}
	if l := obj.GetLabels(); len(l) > 0 {
		for _, k := range displayLabels {
			if v := l[k]; v != "" {
				if r.Labels == nil {
					r.Labels = map[string]string{}
				}
				r.Labels[k] = v
			}
		}
	}
	r.PipelineRef, _, _ = unstructured.NestedString(obj.Object, "spec", "pipelineRef", "name")
	params, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
	for _, p := range params {
		m, _ := p.(map[string]any)
		name, _ := m["name"].(string)
		v, ok := m["value"].(string)
		if !ok {
			continue
		}
		if keptParam(name) {
			if r.Params == nil {
				r.Params = map[string]string{}
			}
			r.Params[name] = v
		}
	}
	r.StartTime, _, _ = unstructured.NestedString(obj.Object, "status", "startTime")
	r.CompletionTime, _, _ = unstructured.NestedString(obj.Object, "status", "completionTime")
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]any); ok && m["type"] == "Succeeded" {
			r.Status, _ = m["status"].(string)
			r.Reason, _ = m["reason"].(string)
		}
	}
	return r
}

// Object is the run as a PipelineRun object holding only what the cache
// keeps, so a reader written for listed runs reads it unchanged.
func (r Run) Object() unstructured.Unstructured {
	meta := map[string]any{"name": r.Name, "namespace": r.Namespace}
	if !r.Created.IsZero() {
		meta["creationTimestamp"] = r.Created.UTC().Format(time.RFC3339)
	}
	if len(r.Labels) > 0 {
		labels := map[string]any{}
		for k, v := range r.Labels {
			labels[k] = v
		}
		meta["labels"] = labels
	}
	spec := map[string]any{}
	if r.PipelineRef != "" {
		spec["pipelineRef"] = map[string]any{"name": r.PipelineRef}
	}
	var params []any
	names := make([]string, 0, len(r.Params))
	for k := range r.Params {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		params = append(params, map[string]any{"name": k, "value": r.Params[k]})
	}
	if len(params) > 0 {
		spec["params"] = params
	}
	status := map[string]any{}
	if r.StartTime != "" {
		status["startTime"] = r.StartTime
	}
	if r.CompletionTime != "" {
		status["completionTime"] = r.CompletionTime
	}
	if r.Status != "" || r.Reason != "" {
		status["conditions"] = []any{map[string]any{"type": "Succeeded", "status": r.Status, "reason": r.Reason}}
	}
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": meta, "spec": spec, "status": status}}
}

// Synced says the cache answers: the runs were listed in full at least once
// and the last list or watch did not fail. A reader falls back to its own
// list when it is false.
func (g *Gate) Synced() bool { return g.synced.Load() }

// Snapshot is every run the cache holds, by name.
func (g *Gate) Snapshot() []Run { return g.pick(func(Run) bool { return true }) }

// RunsOf is the runs whose tekton.dev/pipeline label is pipeline, by name.
func (g *Gate) RunsOf(pipeline string) []Run {
	return g.pick(func(r Run) bool { return r.Labels[labelPipeline] == pipeline })
}

// Running is the runs not finished, by name.
func (g *Gate) Running() []Run { return g.pick(func(r Run) bool { return !r.Finished() }) }

// Get is one run by name.
func (g *Gate) Get(name string) (Run, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	f, ok := g.runs[name]
	return f.run, ok
}

// Latest is the newest run (by creation, then name) of a pipeline (its label).
func (g *Gate) Latest(pipeline string) (Run, bool) {
	runs := g.RunsOf(pipeline)
	if len(runs) == 0 {
		return Run{}, false
	}
	best := runs[0]
	for _, r := range runs[1:] {
		if r.Created.After(best.Created) || r.Created.Equal(best.Created) && r.Name > best.Name {
			best = r
		}
	}
	return best, true
}

// Objects is RunsOf(pipeline) as objects (Run.Object), or every run when
// pipeline is "": the form the delivery readers take.
func (g *Gate) Objects(pipeline string) []unstructured.Unstructured {
	var runs []Run
	if pipeline == "" {
		runs = g.Snapshot()
	} else {
		runs = g.RunsOf(pipeline)
	}
	out := make([]unstructured.Unstructured, len(runs))
	for i, r := range runs {
		out[i] = r.Object()
	}
	return out
}

func (g *Gate) pick(keep func(Run) bool) []Run {
	g.mu.RLock()
	out := make([]Run, 0, len(g.runs))
	for _, f := range g.runs {
		if keep(f.run) {
			out = append(out, f.run)
		}
	}
	g.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
