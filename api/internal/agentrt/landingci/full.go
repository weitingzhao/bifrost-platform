package landingci

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Full CI (the daily full run): with incremental Go tests per landing
// (R5:5.7-1), the whole suite of a commit runs on a schedule, a run of the
// platform's pipeline with the spec a landing's CI has. The controller starts
// them (controller/fullci.go); this file only creates and lists them.
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
	params := map[string]string{"revision": revision, "uiRevision": "main", "repo-url": PlatformRepoURL}
	if c.FullScopeParam {
		params[APIScopeRequestParam] = APIScopeFull
	}
	labels := map[string]string{"tekton.dev/pipeline": "bifrost-ci-platform", "bifrost.io/repo": "bifrost-platform",
		"bifrost.io/trigger": FullCITrigger, "bifrost.io/revision": revision}
	return name, c.create(ctx, name, "bifrost-ci-platform", params, labels)
}

// FullRuns lists the full runs created at or after since, oldest first.
func (c *Client) FullRuns(ctx context.Context, since time.Time) ([]FullRun, error) {
	items, err := c.pipelineRuns(ctx, "bifrost-ci-platform")
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
