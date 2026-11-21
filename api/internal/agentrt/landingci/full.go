package landingci

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Full CI (the daily full run): with incremental Go tests per landing
// (R5:5.7-1), the whole suite of a commit runs on a schedule, a run of the
// platform's pipeline with the spec a landing's CI has. The controller starts
// them (agentrt/fullci); this file only creates and lists them.
const (
	FullCITrigger = "agentrt-full-ci"
	// FailedTestsResult names the run's failed tests, separated by spaces,
	// commas or lines, when the pipeline gives that result.
	FailedTestsResult = "failed-tests"
	// APIScopeRequestParam asks test-api for the whole API. It is sent only
	// where the pipeline declares it (Client.FullScopeParam); until then
	// every run is full anyway.
	APIScopeRequestParam = "api-scope-request"
	// BaseParamName names a platform landing CI's base M0, for test-api's
	// scoped tests. It is sent only where the pipeline declares it
	// (Client.BaseParam); a full run never sends it.
	BaseParamName = "base"
	// fullTestTask is the pipeline task whose log names the failed tests.
	fullTestTask = "test-api"
	// changesTask is the pipeline task that decides which modules of the
	// repository's declaration a run runs, and RunModulesResult its TaskRun
	// result saying so: "*" for every module, otherwise the names of the
	// modules that ran, each between spaces (" api repo ").
	changesTask      = "changes"
	RunModulesResult = "run-modules"
)

var fullRunName = regexp.MustCompile(`^ci-full-[0-9a-f]{12}-[0-9]{8}$`)

// FullRunName is the run of revision on day (the date as day's zone has
// it): one name per commit per day.
func FullRunName(revision string, day time.Time) string {
	if len(revision) < 12 {
		return ""
	}
	return "ci-full-" + revision[:12] + "-" + day.Format("20060102")
}

// FullRun is one full run as listed.
type FullRun struct {
	Name, Revision string
	// Status is runStatus's: Succeeded, Failed, Running or Unknown.
	Status    string
	CreatedAt time.Time
	// FailedTests is the run's failed-tests result; HasFailedTests says
	// whether the run gave that result at all.
	FailedTests    []string
	HasFailedTests bool
}

// EnsureFullRun creates the full run of revision on day and returns its
// name. Its spec is EnsureLandingCI's without base (whatever BaseParam, so
// the run stays full), plus api-scope-request=full when FullScopeParam is set; an existing run of that name with another spec or
// other labels is a collision, as in ensure.
func (c *Client) EnsureFullRun(ctx context.Context, revision string, day time.Time) (string, error) {
	if !fullSHA.MatchString(revision) || day.IsZero() {
		return "", fmt.Errorf("invalid full CI revision")
	}
	name := FullRunName(revision, day)
	params := map[string]string{"revision": revision, "uiRevision": "main", "repo-url": platformRepoURL}
	if c.FullScopeParam {
		params[APIScopeRequestParam] = APIScopeFull
	}
	labels := map[string]string{"tekton.dev/pipeline": "bifrost-ci-platform", "bifrost.io/repo": "bifrost-platform",
		"bifrost.io/trigger": FullCITrigger, "bifrost.io/revision": revision}
	return name, c.create(ctx, name, "bifrost-ci-platform", params, labels)
}

// FullRuns lists the full runs created at or after since, oldest first.
func (c *Client) FullRuns(ctx context.Context, since time.Time) ([]FullRun, error) {
	const pipeline = "bifrost-ci-platform"
	listed := func() ([]unstructured.Unstructured, error) {
		return c.pipelineRuns(ctx, pipeline, "bifrost.io/trigger="+FullCITrigger)
	}
	var items []unstructured.Unstructured
	var err error
	if c.GateRuns != nil {
		items, err = c.GateRuns(pipeline, func(run *unstructured.Unstructured) bool {
			return run.GetLabels()["bifrost.io/trigger"] == FullCITrigger
		}, listed)
	} else {
		items, err = listed()
	}
	if err != nil {
		return nil, err
	}
	var out []FullRun
	for _, item := range items {
		created := item.GetCreationTimestamp().Time
		if item.GetLabels()["bifrost.io/trigger"] != FullCITrigger || !fullRunName.MatchString(item.GetName()) || created.Before(since) {
			continue
		}
		params, counts := namedValues(item, "spec", "params")
		if counts["revision"] != 1 || !fullSHA.MatchString(params["revision"]) {
			continue
		}
		r := FullRun{Name: item.GetName(), Revision: params["revision"], Status: runStatus(item), CreatedAt: created}
		results, _ := namedValues(item, "status", "results")
		if r.Status == "Failed" && len(results) == 0 {
			// The bus run cache keeps no results: a red run's come from the run.
			res, err := c.resource()
			if err != nil {
				return nil, err
			}
			run, err := res.Get(ctx, item.GetName(), metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			results, _ = namedValues(*run, "status", "results")
		}
		if value, ok := results[FailedTestsResult]; ok {
			r.HasFailedTests = true
			r.FailedTests = strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r' })
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// FullRunTestLog is the end of the log of the run's failed test-api step
// (Failure), "" when that task did not fail or its log cannot be read.
func (c *Client) FullRunTestLog(ctx context.Context, run string) (string, error) {
	f, err := c.Failure(ctx, run)
	if err != nil {
		return "", err
	}
	for _, t := range f.TaskRuns {
		if t.PipelineTask == fullTestTask {
			return t.LogTail, nil
		}
	}
	return "", nil
}

// FullRunFailedTasks names the pipeline tasks of the run's failed TaskRuns
// (Failure), sorted, each once. A run none of whose TaskRuns failed (a
// timeout, a cancel) gives none: Failure tells that one by the run itself,
// which is no pipeline task.
func (c *Client) FullRunFailedTasks(ctx context.Context, run string) ([]string, error) {
	f, err := c.Failure(ctx, run)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range f.TaskRuns {
		if t.PipelineTask != "" {
			out = append(out, t.PipelineTask)
		}
	}
	sort.Strings(out)
	return slices.Compact(out), nil
}

// RunModules reads which modules the CI run of revision ran: among the
// pipeline's runs of exactly that revision that are not full runs and did not
// reuse another run's verdict, the newest whose changes TaskRun gives the
// run-modules result. It returns that
// run and the result as written but for the newlines around it, so the
// spaces around a module's name stay ("*" is every module). No such run is
// "", "", nil. A TaskRun that cannot be read is an error: an older run's
// answer is not taken for a newer run's.
func (c *Client) RunModules(ctx context.Context, revision string) (run, modules string, err error) {
	items, err := c.pipelineRuns(ctx, "bifrost-ci-platform")
	if err != nil {
		return "", "", err
	}
	return c.runModulesAmong(ctx, items, revision)
}

// RunModulesOf is RunModules for any project: the runs are those that carry
// repo in their bifrost.io/repo label, whatever their pipeline.
func (c *Client) RunModulesOf(ctx context.Context, repo, revision string) (run, modules string, err error) {
	res, err := c.resource()
	if err != nil {
		return "", "", err
	}
	list, err := res.List(ctx, metav1.ListOptions{LabelSelector: repoLabel + "=" + repo})
	if err != nil {
		return "", "", err
	}
	var items []unstructured.Unstructured
	for _, item := range list.Items {
		if item.GetLabels()[repoLabel] == repo { // the tests' fake client ignores the selector
			items = append(items, item)
		}
	}
	return c.runModulesAmong(ctx, items, revision)
}

func (c *Client) runModulesAmong(ctx context.Context, items []unstructured.Unstructured, revision string) (run, modules string, err error) {
	var of []unstructured.Unstructured
	for _, item := range items {
		params, counts := namedValues(item, "spec", "params")
		if counts["revision"] != 1 || params["revision"] != revision || item.GetLabels()["bifrost.io/trigger"] == FullCITrigger {
			continue
		}
		// A run that reused another run's verdict (the run of main's push
		// after a landing) ran nothing itself: its changes task sees a commit
		// on main and answers "*", which would hide what the landing's own run
		// left out.
		if ClaimsReuse(item) {
			continue
		}
		of = append(of, item)
	}
	sort.Slice(of, func(i, j int) bool {
		a, b := of[i].GetCreationTimestamp().Time, of[j].GetCreationTimestamp().Time
		if !a.Equal(b) {
			return a.After(b)
		}
		return of[i].GetName() > of[j].GetName()
	})
	d, err := c.Dynamic()
	if err != nil {
		return "", "", err
	}
	for _, item := range of {
		children, _, _ := unstructured.NestedSlice(item.Object, "status", "childReferences")
		for _, value := range children {
			child, _ := value.(map[string]any)
			name, _ := child["name"].(string)
			if child["kind"] != "TaskRun" || name == "" || child["pipelineTaskName"] != changesTask {
				continue
			}
			task, err := d.Resource(pipelineRunGVR.GroupVersion().WithResource("taskruns")).Namespace(c.namespace()).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return "", "", fmt.Errorf("CI TaskRun %s: %w", name, err)
			}
			results, _, _ := unstructured.NestedSlice(task.Object, "status", "results")
			for _, entry := range results {
				m, _ := entry.(map[string]any)
				if m["name"] != RunModulesResult {
					continue
				}
				value, _ := m["value"].(string)
				return item.GetName(), strings.Trim(value, "\r\n"), nil
			}
		}
	}
	return "", "", nil
}
