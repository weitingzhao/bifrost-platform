package landingci

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The CI runs of a project as the Ops run view shows them (declaration
// standard §6): for each run which modules ran and why, whose verdict it
// stands on when it ran nothing itself, and how long each of its tasks took.
// Everything is read from the runs themselves: a project is the value of the
// bifrost.io/repo label its runs carry, and nothing here names a project, a
// pipeline or a module.

const (
	repoLabel = "bifrost.io/repo"
	// ScopeWhyResult is the changes task's one line on how run-modules was chosen.
	ScopeWhyResult = "scope-why"
	// testedByResult names the run whose verdict a run reused (ClaimsReuse).
	testedByResult = "tested-by"
)

// RunView is one CI run.
type RunView struct {
	Name     string `json:"name"`
	Pipeline string `json:"pipeline"`
	Revision string `json:"revision"`
	// Status is runStatus's: Succeeded, Failed, Running or Unknown.
	Status  string `json:"status"`
	Trigger string `json:"trigger,omitempty"` // the bifrost.io/trigger label
	// Full says the run is a daily full run: everything runs, whatever changed.
	Full      bool   `json:"full"`
	CreatedAt string `json:"created_at"`
	Seconds   int    `json:"seconds,omitempty"` // start to completion; 0 while it runs
	// TestedBy is the run whose verdict this one reused; it ran nothing itself.
	TestedBy string `json:"tested_by,omitempty"`
	// RunModules is the changes task's answer as written ("*" for every
	// module, otherwise the modules that ran, each between spaces) and
	// ScopeWhy its reason; Asked is false when the run has no such result
	// (a pipeline that does not ask, or a run from before it did).
	Asked      bool       `json:"asked"`
	RunModules string     `json:"run_modules,omitempty"`
	ScopeWhy   string     `json:"scope_why,omitempty"`
	Tasks      []TaskView `json:"tasks"`
}

// TaskView is one task of a run.
type TaskView struct {
	Task    string `json:"task"`
	Status  string `json:"status"`
	Seconds int    `json:"seconds,omitempty"` // creation to completion
	Waited  int    `json:"waited,omitempty"`  // creation to its first step starting
}

// RecentRuns lists the newest CI runs that carry repo in their bifrost.io/repo
// label, newest first, at most limit. A run that merges (it takes a branch) is
// not a CI run and is left out.
func (c *Client) RecentRuns(ctx context.Context, repo string, limit int) ([]RunView, error) {
	res, err := c.resource()
	if err != nil {
		return nil, err
	}
	var items []unstructured.Unstructured
	seen, cont := map[string]bool{}, ""
	for {
		list, err := res.List(ctx, metav1.ListOptions{Limit: 500, Continue: cont, LabelSelector: repoLabel + "=" + repo})
		if err != nil {
			return nil, err
		}
		for _, item := range list.Items {
			// The fake client of the tests ignores the selector; the label is checked again.
			params, counts := namedValues(item, "spec", "params")
			if item.GetLabels()[repoLabel] != repo || counts["revision"] != 1 || !fullSHA.MatchString(params["revision"]) || counts["branch"] > 0 {
				continue
			}
			items = append(items, item)
		}
		if cont = list.GetContinue(); cont == "" {
			break
		}
		if seen[cont] {
			return nil, fmt.Errorf("CI run pagination repeated")
		}
		seen[cont] = true
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].GetCreationTimestamp().Time, items[j].GetCreationTimestamp().Time
		if !a.Equal(b) {
			return a.After(b)
		}
		return items[i].GetName() > items[j].GetName()
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]RunView, 0, len(items))
	names := make([]string, 0, len(items))
	for _, item := range items {
		params, _ := namedValues(item, "spec", "params")
		results, _ := namedValues(item, "status", "results")
		v := RunView{Name: item.GetName(), Revision: params["revision"], Status: runStatus(item), Trigger: item.GetLabels()["bifrost.io/trigger"],
			CreatedAt: item.GetCreationTimestamp().UTC().Format(time.RFC3339), TestedBy: results[testedByResult], Tasks: []TaskView{}}
		v.Pipeline, _, _ = unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
		v.Full = v.Trigger == FullCITrigger
		v.Seconds = between(item, "startTime", "completionTime")
		out = append(out, v)
		names = append(names, v.Name)
	}
	if len(names) == 0 {
		return out, nil
	}
	d, err := c.Dynamic()
	if err != nil {
		return nil, err
	}
	// One list for the tasks of all of them.
	tasks, err := d.Resource(pipelineRunGVR.GroupVersion().WithResource("taskruns")).Namespace(c.namespace()).
		List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun in (" + strings.Join(names, ",") + ")"})
	if err != nil {
		return nil, fmt.Errorf("CI TaskRuns: %w", err)
	}
	index := map[string]*RunView{}
	for i := range out {
		index[out[i].Name] = &out[i]
	}
	for _, task := range tasks.Items {
		v := index[task.GetLabels()["tekton.dev/pipelineRun"]]
		if v == nil {
			continue
		}
		t := TaskView{Task: task.GetLabels()["tekton.dev/pipelineTask"], Status: runStatus(task)}
		created := task.GetCreationTimestamp().Time
		if done := stamp(task, "status", "completionTime"); !done.IsZero() && !created.IsZero() {
			t.Seconds = int(done.Sub(created).Seconds())
		}
		if first := firstStepStart(task); !first.IsZero() && !created.IsZero() {
			t.Waited = int(first.Sub(created).Seconds())
		}
		v.Tasks = append(v.Tasks, t)
		results, _ := namedValuesKept(task, "status", "results")
		if value, ok := results[RunModulesResult]; ok {
			v.Asked, v.RunModules, v.ScopeWhy = true, strings.Trim(value, "\r\n"), strings.TrimSpace(results[ScopeWhyResult])
		}
	}
	for i := range out {
		sort.Slice(out[i].Tasks, func(a, b int) bool { return out[i].Tasks[a].Task < out[i].Tasks[b].Task })
	}
	return out, nil
}

// namedValuesKept is namedValues without trimming: the spaces around a
// module's name in run-modules are part of the value.
func namedValuesKept(item unstructured.Unstructured, fields ...string) (map[string]string, map[string]int) {
	list, _, _ := unstructured.NestedSlice(item.Object, fields...)
	values, counts := map[string]string{}, map[string]int{}
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		counts[name]++
		values[name] = value
	}
	return values, counts
}

func stamp(item unstructured.Unstructured, fields ...string) time.Time {
	s, _, _ := unstructured.NestedString(item.Object, fields...)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func between(item unstructured.Unstructured, from, to string) int {
	a, b := stamp(item, "status", from), stamp(item, "status", to)
	if a.IsZero() || b.IsZero() {
		return 0
	}
	return int(b.Sub(a).Seconds())
}

// firstStepStart is when the first of a TaskRun's steps started.
func firstStepStart(task unstructured.Unstructured) time.Time {
	steps, _, _ := unstructured.NestedSlice(task.Object, "status", "steps")
	var first time.Time
	for _, value := range steps {
		step, _ := value.(map[string]any)
		for _, state := range []string{"terminated", "running"} {
			s, _, _ := unstructured.NestedString(step, state, "startedAt")
			if t, err := time.Parse(time.RFC3339, s); err == nil && (first.IsZero() || t.Before(first)) {
				first = t
			}
		}
	}
	return first
}
