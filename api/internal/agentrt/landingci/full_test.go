package landingci

import (
	"context"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
