package landingci

import (
	"context"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clienttesting "k8s.io/client-go/testing"
)

func fullParams(t *testing.T, c *Client, name string) (map[string]string, map[string]string) {
	t.Helper()
	res, err := c.resource()
	if err != nil {
		t.Fatal(err)
	}
	obj, err := res.Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := namedValues(*obj, "spec", "params")
	return params, obj.GetLabels()
}

func TestFullCIEnsureOneRunPerCommitPerDay(t *testing.T) {
	t.Parallel()
	c, _ := drillService(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	name, err := c.EnsureFullRun(ctx, factSHA1, day)
	if err != nil || name != "ci-full-"+factSHA1[:12]+"-20261106" {
		t.Fatal(name, err)
	}
	again, err := c.EnsureFullRun(ctx, factSHA1, day.Add(20*time.Hour))
	if err != nil || again != name {
		t.Fatal("the same day is the same run", again, err)
	}
	next, err := c.EnsureFullRun(ctx, factSHA1, day.AddDate(0, 0, 1))
	if err != nil || next == name {
		t.Fatal("the next day is another run", next, err)
	}
	params, labels := fullParams(t, c, name)
	want := map[string]string{"revision": factSHA1, "uiRevision": "main", "repo-url": PlatformRepoURL}
	if !reflect.DeepEqual(params, want) {
		t.Fatal(params)
	}
	if labels["bifrost.io/trigger"] != FullCITrigger || labels["bifrost.io/repo"] != "bifrost-platform" || labels["bifrost.io/revision"] != factSHA1 {
		t.Fatal(labels)
	}
	if _, err := c.EnsureFullRun(ctx, "main", day); err == nil {
		t.Fatal("a branch name was accepted")
	}
}

func TestFullCIParamOnlyWhenOn(t *testing.T) {
	t.Parallel()
	c, _ := drillService(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	off, err := c.EnsureFullRun(ctx, factSHA1, day)
	if err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, c, off); params[APIScopeRequestParam] != "" {
		t.Fatal("the param was sent while off", params)
	}
	on := &Client{Dynamic: c.Dynamic, FullScopeParam: true}
	name, err := on.EnsureFullRun(ctx, factSHA2, day)
	if err != nil {
		t.Fatal(err)
	}
	if params, _ := fullParams(t, on, name); params[APIScopeRequestParam] != APIScopeFull {
		t.Fatal("the param was not sent while on", params)
	}
	// The run of that name without the param is another spec: a collision.
	if _, err := on.EnsureFullRun(ctx, factSHA1, day); err == nil || err.Error() != "landing CI name collision" {
		t.Fatal("a run of another spec was adopted", err)
	}
}

func TestFullCIRunsListsStatusAndFailedTests(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	red := drillCI(t, FullRunName(factSHA1, now), "False", now)
	red.SetLabels(map[string]string{"bifrost.io/trigger": FullCITrigger, "bifrost.io/repo": "bifrost-platform"})
	setField(t, red, []any{map[string]any{"name": FailedTestsResult, "value": "TestA, TestB\nTestC"}}, "status", "results")
	green := drillCI(t, FullRunName(factSHA1, now.AddDate(0, 0, -1)), "True", now.Add(-24*time.Hour))
	green.SetLabels(map[string]string{"bifrost.io/trigger": FullCITrigger})
	old := drillCI(t, FullRunName(factSHA1, now.AddDate(0, 0, -30)), "True", now.AddDate(0, 0, -30))
	old.SetLabels(map[string]string{"bifrost.io/trigger": FullCITrigger})
	landingRun := drillCI(t, "landing-ci-000000000000000000000000-1", "False", now)
	landingRun.SetLabels(map[string]string{"bifrost.io/trigger": "agentrt-landing-drill"})
	c, _ := drillService(t, red, green, old, landingRun)
	runs, err := c.FullRuns(context.Background(), now.AddDate(0, 0, -14))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Name != green.GetName() || runs[0].Status != "Succeeded" || runs[0].HasFailedTests {
		t.Fatal(runs)
	}
	r := runs[1]
	if r.Name != red.GetName() || r.Status != "Failed" || r.Revision != factSHA1 || !r.HasFailedTests ||
		!reflect.DeepEqual(r.FailedTests, []string{"TestA", "TestB", "TestC"}) || !r.CreatedAt.Equal(now) {
		t.Fatal(r)
	}
}

// 2026-11-14: the daily full CI was the only run that would catch a module
// wrongly left out of a landing's CI, and a red one reached nobody: a log
// line, a note, and a goal for a dispatch lane that was being removed; it
// also named only Go tests. A red full run names every pipeline task that
// failed, Go or not, and not the ones that passed; a run none of whose
// TaskRuns failed (Failure tells that one by the run itself) names none.
func TestFullRunFailedTasksNamesEveryFailedTask(t *testing.T) {
	t.Parallel()
	run := failureObject("PipelineRun", "ci-full-x", map[string]any{"conditions": succeeded("False", "Tasks Completed: 4 (Failed: 3)"),
		"childReferences": []any{
			map[string]any{"kind": "TaskRun", "name": "ci-full-x-web", "pipelineTaskName": "web"},
			map[string]any{"kind": "TaskRun", "name": "ci-full-x-web-retry", "pipelineTaskName": "web"},
			map[string]any{"kind": "TaskRun", "name": "ci-full-x-lint", "pipelineTaskName": "lint"},
			map[string]any{"kind": "TaskRun", "name": "ci-full-x-build", "pipelineTaskName": "build"},
		}})
	failed := func(name string) *unstructured.Unstructured {
		return failureObject("TaskRun", name, map[string]any{"conditions": succeeded("False", "exited with code 1")})
	}
	build := failureObject("TaskRun", "ci-full-x-build", map[string]any{"conditions": succeeded("True", "ok")})
	timedOut := failureObject("PipelineRun", "ci-full-y", map[string]any{"conditions": succeeded("False", "PipelineRun timed out")})
	c, _ := fakeClient(run, failed("ci-full-x-web"), failed("ci-full-x-web-retry"), failed("ci-full-x-lint"), build, timedOut)
	tasks, err := c.FullRunFailedTasks(context.Background(), "ci-full-x")
	if err != nil || !reflect.DeepEqual(tasks, []string{"lint", "web"}) {
		t.Fatal(tasks, err)
	}
	if tasks, err = c.FullRunFailedTasks(context.Background(), "ci-full-y"); err != nil || len(tasks) != 0 {
		t.Fatal("a run with no failed TaskRun named tasks", tasks, err)
	}
}

// modulesRun is a CI run of factSHA1 with a changes TaskRun; results are
// that TaskRun's.
func modulesRun(t *testing.T, name string, at time.Time, results []any) (*unstructured.Unstructured, *unstructured.Unstructured) {
	t.Helper()
	run := drillCI(t, name, "True", at)
	setField(t, run, []any{
		map[string]any{"kind": "TaskRun", "name": name + "-other", "pipelineTaskName": "other"},
		map[string]any{"kind": "TaskRun", "name": name + "-changes", "pipelineTaskName": changesTask},
	}, "status", "childReferences")
	status := map[string]any{"conditions": succeeded("True", "ok")}
	if results != nil {
		status["results"] = results
	}
	return run, failureObject("TaskRun", name+"-changes", status)
}

// 2026-11-14 (the same origin as above): the reconciliation of a red full
// run reads, per landed commit, which modules its own CI run ran. Of two
// runs of one revision the newer answers; a full run of that revision is
// not a landing's run; a run whose changes task gave no such result is
// passed over; the value keeps the spaces around the names; no run is "".
func TestRunModulesReadsTheNewestRunOfTheRevision(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	result := func(v string) []any {
		return []any{map[string]any{"name": "other", "value": "x"}, map[string]any{"name": RunModulesResult, "value": v}}
	}
	oldRun, oldTask := modulesRun(t, "ci-old", now.Add(-3*time.Hour), result("*"))
	newRun, newTask := modulesRun(t, "ci-new", now.Add(-2*time.Hour), result(" api repo \n"))
	// Newer still, but its changes task wrote no run-modules.
	bareRun, bareTask := modulesRun(t, "ci-bare", now.Add(-time.Hour), []any{map[string]any{"name": "other", "value": "x"}})
	// The newest of all is a full run of the revision.
	fullRun, fullTask := modulesRun(t, FullRunName(factSHA1, now), now, result("*"))
	fullRun.SetLabels(map[string]string{"bifrost.io/trigger": FullCITrigger})
	c, _ := fakeClient(oldRun, oldTask, newRun, newTask, bareRun, bareTask, fullRun, fullTask)
	run, modules, err := c.RunModules(context.Background(), factSHA1)
	if err != nil || run != "ci-new" || modules != " api repo " {
		t.Fatalf("run %q modules %q err %v", run, modules, err)
	}
	if run, modules, err = c.RunModules(context.Background(), factSHA2); err != nil || run != "" || modules != "" {
		t.Fatalf("a revision without a run: %q %q %v", run, modules, err)
	}
	// Only the full run and the bare one: none answers.
	c, _ = fakeClient(bareRun, bareTask, fullRun, fullTask)
	if run, modules, err = c.RunModules(context.Background(), factSHA1); err != nil || run != "" || modules != "" {
		t.Fatalf("no run with the result: %q %q %v", run, modules, err)
	}
	// The run of main's push after a landing is newer and says "*", but it
	// reused the landing run's verdict (tested-by): the landing's own run is
	// the one that answers. Origin: 2026-11-14, review of this function before
	// it landed; every landing on main is followed by such a run.
	reuseRun, reuseTask := modulesRun(t, "ci-reuse", now.Add(-30*time.Minute), result("*"))
	if err = unstructured.SetNestedSlice(reuseRun.Object, []any{map[string]any{"name": "tested-by", "value": "ci-new"}}, "status", "results"); err != nil {
		t.Fatal(err)
	}
	c, _ = fakeClient(newRun, newTask, reuseRun, reuseTask)
	if run, modules, err = c.RunModules(context.Background(), factSHA1); err != nil || run != "ci-new" || modules != " api repo " {
		t.Fatalf("a newer run that reused the landing's: %q %q %v", run, modules, err)
	}
	// The same reading for any project: its runs are the ones with its label,
	// whatever their pipeline. Origin: 2026-11-14, the check route of the
	// reconciliation takes a repository name; nothing in it is the platform's own.
	for _, o := range []*unstructured.Unstructured{newRun, reuseRun} {
		o.SetLabels(map[string]string{repoLabel: "proj"})
	}
	c, _ = fakeClient(newRun, newTask, reuseRun, reuseTask, oldRun, oldTask)
	if run, modules, err = c.RunModulesOf(context.Background(), "proj", factSHA1); err != nil || run != "ci-new" || modules != " api repo " {
		t.Fatalf("by project: %q %q %v", run, modules, err)
	}
	if run, _, err = c.RunModulesOf(context.Background(), "another", factSHA1); err != nil || run != "" {
		t.Fatalf("another project has no such run: %q %v", run, err)
	}
	// Every module: the star is kept as it is.
	c, _ = fakeClient(oldRun, oldTask)
	if run, modules, err = c.RunModules(context.Background(), factSHA1); err != nil || run != "ci-old" || modules != "*" {
		t.Fatalf("the star: %q %q %v", run, modules, err)
	}
}

// 2026-11-17, measured on PROD: the daily full CI's look at its own runs, once
// a minute, listed every run of the namespace (five pages of 2,058) to find a
// handful: 47 LISTs in ten minutes. The list asks for the trigger label the
// run is created with; a run without the tekton.dev/pipeline label (Tekton's
// controller writes it when it first reaches the run, and did not for 23
// minutes on 2026-11-17 00:01) is still listed. Positive control: without the
// selector in pipelineRuns the first assertion fails.
func TestFullRunsListsByTheTriggerLabelOnly(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	fresh := drillCI(t, FullRunName(factSHA1, now), "Unknown", now)
	fresh.SetLabels(map[string]string{"bifrost.io/trigger": FullCITrigger})
	c, d := drillService(t, fresh)
	runs, err := c.FullRuns(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var selectors []string
	for _, a := range d.Actions() {
		if l, ok := a.(clienttesting.ListAction); ok {
			selectors = append(selectors, l.GetListRestrictions().Labels.String())
		}
	}
	if !reflect.DeepEqual(selectors, []string{"bifrost.io/trigger=" + FullCITrigger}) {
		t.Fatal("the list did not ask for the trigger label alone", selectors)
	}
	if len(runs) != 1 || runs[0].Name != fresh.GetName() {
		t.Fatal("a run Tekton has not labelled yet is missing", runs)
	}
}
