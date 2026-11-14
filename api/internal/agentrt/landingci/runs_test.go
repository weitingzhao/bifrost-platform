package landingci

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The Ops run view (Owner, 2026-11-13: he accepts what the page shows, and
// the page's data is read from the source). Origin of the cases: the three
// kinds of run seen on 2026-11-14 after the changes task began to write
// run-modules: a landing's run that left a module out (ci-platform-nw2jm), the
// run of main's push that reused it, and a daily full run; and a merge run,
// which carries the same project label and is not a CI run. The project is
// the label's value: the names here are not the platform's.
func TestRecentRunsReadsWhatEachRunRan(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	run := func(name, repo string, at time.Time, results []any) *unstructured.Unstructured {
		o := drillCI(t, name, "True", at)
		labels := o.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[repoLabel] = repo
		o.SetLabels(labels)
		setField(t, o, at.Format(time.RFC3339), "status", "startTime")
		setField(t, o, at.Add(200*time.Second).Format(time.RFC3339), "status", "completionTime")
		if results != nil {
			setField(t, o, results, "status", "results")
		}
		return o
	}
	task := func(runName, name string, at time.Time, status string, results []any) *unstructured.Unstructured {
		o := failureObject("TaskRun", runName+"-"+name, map[string]any{"conditions": succeeded(status, "x"),
			"completionTime": at.Add(100 * time.Second).Format(time.RFC3339),
			"steps":          []any{map[string]any{"name": "s", "terminated": map[string]any{"startedAt": at.Add(6 * time.Second).Format(time.RFC3339)}}}})
		o.SetLabels(map[string]string{"tekton.dev/pipelineRun": runName, "tekton.dev/pipelineTask": name})
		o.SetCreationTimestamp(run("x", "x", at, nil).GetCreationTimestamp())
		if results != nil {
			setField(t, o, results, "status", "results")
		}
		return o
	}
	landed := run("ci-landing", "proj", now.Add(-3*time.Hour), []any{map[string]any{"name": testedByResult, "value": ""}})
	reuse := run("ci-reuse", "proj", now.Add(-2*time.Hour), []any{map[string]any{"name": testedByResult, "value": "ci-landing"}})
	full := run("ci-full-aaaaaaaaaaaa-20261114", "proj", now.Add(-time.Hour), nil)
	full.SetLabels(map[string]string{repoLabel: "proj", "bifrost.io/trigger": FullCITrigger})
	merge := run("land-1", "proj", now, nil)
	params, _, _ := unstructured.NestedSlice(merge.Object, "spec", "params")
	setField(t, merge, append(params, map[string]any{"name": "branch", "value": "refs/heads/ready/x"}), "spec", "params")
	other := run("ci-other", "another", now, nil)
	c, _ := fakeClient(landed, reuse, full, merge, other,
		task("ci-landing", "changes", now.Add(-3*time.Hour), "True", []any{
			map[string]any{"name": RunModulesResult, "value": " svc repo "}, map[string]any{"name": ScopeWhyResult, "value": "not run, their keys are the base's: web\n"}}),
		task("ci-landing", "test-web", now.Add(-3*time.Hour), "False", nil),
		task("ci-reuse", "changes", now.Add(-2*time.Hour), "True", []any{map[string]any{"name": RunModulesResult, "value": "*"}}),
		task("ci-other", "changes", now, "True", nil))
	got, err := c.RecentRuns(context.Background(), "proj", 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("runs: %+v %v", got, err)
	}
	if got[0].Name != full.GetName() || !got[0].Full || got[0].Asked || len(got[0].Tasks) != 0 {
		t.Errorf("the full run: %+v", got[0])
	}
	if got[1].Name != "ci-reuse" || got[1].TestedBy != "ci-landing" || got[1].RunModules != "*" || !got[1].Asked {
		t.Errorf("the reuse run: %+v", got[1])
	}
	l := got[2]
	if l.Name != "ci-landing" || l.TestedBy != "" || !l.Asked || l.RunModules != " svc repo " || l.ScopeWhy != "not run, their keys are the base's: web" ||
		l.Seconds != 200 || l.Status != "Succeeded" || l.Pipeline != "bifrost-ci-platform" || len(l.Tasks) != 2 {
		t.Fatalf("the landing's run: %+v", l)
	}
	if l.Tasks[0] != (TaskView{Task: "changes", Status: "Succeeded", Seconds: 100, Waited: 6}) || l.Tasks[1].Task != "test-web" || l.Tasks[1].Status != "Failed" {
		t.Errorf("its tasks: %+v", l.Tasks)
	}
	// The newest only, and a project with no run is an empty list, not an error.
	if got, err = c.RecentRuns(context.Background(), "proj", 1); err != nil || len(got) != 1 || got[0].Name != full.GetName() {
		t.Errorf("limit: %+v %v", got, err)
	}
	if got, err = c.RecentRuns(context.Background(), "nobody", 5); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no runs: %+v %v", got, err)
	}
}
