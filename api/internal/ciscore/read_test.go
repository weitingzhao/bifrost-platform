package ciscore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Origin of every case in this file — ISSUE-20 (2026-11-16): CI scores every
// repository that uses it, and its users score CI; only what is measured is
// an item. The reads are narrow because of 2026-11-14: a loop that read every
// run each round took the workers past their memory limit.

var ciScoreNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func ciStamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ciRunObject is a run as the cluster keeps it, completed an hour before
// ciScoreNow after secs seconds.
func ciRunObject(name, base, status string, secs int, results ...string) unstructured.Unstructured {
	done := ciScoreNow.Add(-time.Hour)
	var list []any
	for _, kv := range results {
		k, v, _ := strings.Cut(kv, "=")
		list = append(list, map[string]any{"name": k, "type": "string", "value": v})
	}
	return unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name, "creationTimestamp": ciStamp(done.Add(-time.Duration(secs+2) * time.Second))},
		"spec":     map[string]any{"params": []any{map[string]any{"name": "revision", "value": "r"}, map[string]any{"name": "base", "value": base}}},
		"status": map[string]any{"startTime": ciStamp(done.Add(-time.Duration(secs) * time.Second)), "completionTime": ciStamp(done),
			"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}, "results": list},
	}}
}

func ciTaskObject(run, task, status string, started time.Time, results ...string) unstructured.Unstructured {
	var list []any
	for _, kv := range results {
		k, v, _ := strings.Cut(kv, "=")
		list = append(list, map[string]any{"name": k, "type": "string", "value": v})
	}
	return unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": run + "-" + task, "labels": map[string]any{"tekton.dev/pipelineRun": run, "tekton.dev/pipelineTask": task}},
		// 2026-11-16: status.startTime is when the controller took the TaskRun (0 s after the
		// run's creation in all 573 runs measured); the pod's first step ran a median of 9 s
		// later. startTime here is an hour early, so a start lag read from it comes out wrong.
		"status": map[string]any{"startTime": ciStamp(started.Add(-time.Hour)), "results": list,
			"steps": []any{
				map[string]any{"name": "later", "terminated": map[string]any{"startedAt": ciStamp(started.Add(5 * time.Second))}},
				map[string]any{"name": "first", "terminated": map[string]any{"startedAt": ciStamp(started)}},
			},
			"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}},
	}}
}

func ciItem(t *testing.T, items []Item, id string) Item {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %s", id)
	return Item{}
}

// Origin: as the file's — the answer is computed from the objects' own
// fields (base parameter, tested-by and api-scope results, the scope task's
// run-modules as written with its newline, conditions and times), and the
// cluster is asked only by pipeline, by task and by the failed runs' names.
func TestCIScorecardReadsRunsAsTheClusterKeepsThem(t *testing.T) {
	var asked []string
	created := ciScoreNow.Add(-time.Hour - 102*time.Second)
	src := Source{
		Runs: func(_ context.Context, pipeline string) ([]unstructured.Unstructured, error) {
			asked = append(asked, "runs "+pipeline)
			switch pipeline {
			case "bifrost-ci-platform":
				var out []unstructured.Unstructured
				for _, name := range []string{"a", "b", "c", "d", "e"} {
					out = append(out, ciRunObject("p-"+name, "", "True", 100, "api-scope=full\n", "tested-by="))
				}
				return append(out, ciRunObject("p-reuse", "0123abcd", "True", 5, "tested-by=p-a"), ciRunObject("p-red", "", "False", 100, "tested-by=")), nil
			case "bifrost-ci-infra":
				return []unstructured.Unstructured{ciRunObject("i-a", strings.Repeat("0", 40), "True", 60)}, nil
			}
			return []unstructured.Unstructured{ciRunObject("land-a", "", "True", 140, "outcome=landed"), ciRunObject("land-red", "", "False", 30)}, nil
		},
		Tasks: func(_ context.Context, selector string) ([]unstructured.Unstructured, error) {
			asked = append(asked, "tasks "+selector)
			switch {
			case strings.HasPrefix(selector, "tekton.dev/pipeline=bifrost-ci-platform,"):
				var out []unstructured.Unstructured
				for _, name := range []string{"a", "b", "c", "d"} {
					out = append(out, ciTaskObject("p-"+name, "changes", "True", created.Add(time.Minute), "run-modules=*\n"),
						ciTaskObject("p-"+name, "clone", "True", created.Add(4*time.Second)))
				}
				return append(out, ciTaskObject("p-e", "changes", "True", created.Add(time.Minute), "run-modules= api repo \n")), nil
			case strings.HasPrefix(selector, "tekton.dev/pipelineRun in ("):
				return []unstructured.Unstructured{ciTaskObject("p-red", "clone", "True", created), ciTaskObject("p-red", "test-api", "False", created),
					ciTaskObject("land-red", "land", "False", created)}, nil
			}
			return nil, nil
		},
		Declaration: func(_ context.Context, repo string) Declaration {
			return Declaration{Declared: repo == "bifrost-platform", Units: 12}
		},
		Cleanup: func(context.Context) (time.Time, error) { return ciScoreNow.Add(-30 * time.Minute), nil },
	}
	out, err := ReadScorecard(context.Background(), src, ciScoreNow)
	if err != nil {
		t.Fatal(err)
	}
	wantAsked := []string{
		"runs bifrost-ci-platform", "tasks tekton.dev/pipeline=bifrost-ci-platform,tekton.dev/pipelineTask in (clone,changes)",
		"runs bifrost-ci-infra", "tasks tekton.dev/pipeline=bifrost-ci-infra,tekton.dev/pipelineTask in (clone,changes)",
		"runs bifrost-land", "tasks tekton.dev/pipelineRun in (p-red,land-red)",
	}
	if strings.Join(asked, "\n") != strings.Join(wantAsked, "\n") {
		t.Errorf("asked the cluster:\n%s\nwant:\n%s", strings.Join(asked, "\n"), strings.Join(wantAsked, "\n"))
	}
	if len(out.Repos) != 2 || out.Repos[0].Repo != "bifrost-platform" || out.Repos[1].Repo != "bifrost-trade-infra" {
		t.Fatalf("repos = %+v", out.Repos)
	}
	platform, infra := out.Repos[0], out.Repos[1]
	for id, want := range map[string]string{
		"landing_time":    "pass median 100 s",                                         // six runs with no base that reused nothing; the reuse is left out
		"follows_change":  "unmeasured 0 incremental and 5 full runs, needs 3 of each", // api-scope read without its newline
		"declares_units":  "pass 12 modules",
		"sides_separated": "pass console skipped in 1 of 5 runs",
		"red_ratio":       "fail 1 failed of 7",
	} {
		if it := ciItem(t, platform.Items, id); it.Verdict+" "+it.Value != want {
			t.Errorf("platform %s = %q, want %q", id, it.Verdict+" "+it.Value, want)
		}
	}
	if platform.Tier != "full" || platform.Overall != "fail" || infra.Tier != "full" {
		t.Errorf("platform tier %s overall %s, infra tier %s", platform.Tier, platform.Overall, infra.Tier)
	}
	for id, want := range map[string]string{
		"landing_time":    "unmeasured 1 runs, needs 5", // an all-zero base is no base
		"follows_change":  "unmeasured the pipeline reports no api-scope",
		"declares_units":  "fail no bifrost.units.yaml",
		"sides_separated": "unmeasured no changes task reports run-modules",
	} {
		if it := ciItem(t, infra.Items, id); it.Verdict+" "+it.Value != want {
			t.Errorf("infra %s = %q, want %q", id, it.Verdict+" "+it.Value, want)
		}
	}
	for id, want := range map[string]string{
		"push_to_verdict":  "pass median 140 s",
		"start_lag":        "pass median 4 s",
		"own_failures":     "pass 0 of 2 failed runs failed in CI's own tasks",
		"cleanup_keeps_up": "pass 0.5 h since it last succeeded",
	} {
		if it := ciItem(t, out.Provider.Items, id); it.Verdict+" "+it.Value != want {
			t.Errorf("provider %s = %q, want %q", id, it.Verdict+" "+it.Value, want)
		}
	}
}

// Origin: as the file's — not read is not none: runs that could not be read
// fail the answer, anything else that could not be read is unmeasured with
// the reason, never a pass.
func TestCIScorecardUnreadIsNotAVerdict(t *testing.T) {
	src := Source{
		Runs: func(context.Context, string) ([]unstructured.Unstructured, error) { return nil, nil },
		Tasks: func(context.Context, string) ([]unstructured.Unstructured, error) {
			return nil, errors.New("taskruns is forbidden")
		},
		Declaration: func(context.Context, string) Declaration { return Declaration{Err: "gitea: HTTP 502"} },
		Cleanup: func(context.Context) (time.Time, error) {
			return time.Time{}, errors.New("cronjobs.batch is forbidden")
		},
	}
	out, err := ReadScorecard(context.Background(), src, ciScoreNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range append(append([]Item{}, out.Repos[0].Items...), out.Provider.Items...) {
		if it.ID != "own_failures" && it.Verdict != Unmeasured {
			t.Errorf("%s = %s %q, want unmeasured", it.ID, it.Verdict, it.Value)
		}
	}
	if it := ciItem(t, out.Provider.Items, "cleanup_keeps_up"); !strings.Contains(it.Value, "forbidden") {
		t.Errorf("cleanup_keeps_up = %q, want the reason", it.Value)
	}
	if out.Repos[0].Tier != "none" || out.Provider.Overall != Unmeasured {
		t.Errorf("tier %s, provider %s", out.Repos[0].Tier, out.Provider.Overall)
	}
	src.Runs = func(context.Context, string) ([]unstructured.Unstructured, error) { return nil, errors.New("timeout") }
	if _, err := ReadScorecard(context.Background(), src, ciScoreNow); err == nil || !strings.Contains(err.Error(), "runs of bifrost-ci-platform: timeout") {
		t.Errorf("err = %v, want the failed read of the runs", err)
	}
}

// Origin: as the file's, and incident #63 (2026-11-11, a page's reads starved
// the platform) — the page asks on every visit; the cluster is read at most
// once in scorecardKeep, and a failed read is not kept.
func TestCIScorecardIsKeptTenMinutes(t *testing.T) {
	reads, fail := 0, true
	src := Source{
		Runs: func(_ context.Context, pipeline string) ([]unstructured.Unstructured, error) {
			if pipeline == "bifrost-ci-platform" {
				reads++
			}
			if fail {
				return nil, errors.New("timeout")
			}
			return nil, nil
		},
		Tasks:       func(context.Context, string) ([]unstructured.Unstructured, error) { return nil, nil },
		Declaration: func(context.Context, string) Declaration { return Declaration{} },
		Cleanup:     func(context.Context) (time.Time, error) { return time.Time{}, nil },
	}
	var cache Cache
	if _, err := cache.Read(context.Background(), src, ciScoreNow); err == nil {
		t.Fatal("a failed read answered")
	}
	fail = false
	for _, after := range []time.Duration{time.Second, 5 * time.Minute, 10 * time.Minute} {
		out, err := cache.Read(context.Background(), src, ciScoreNow.Add(after))
		if err != nil || out.GeneratedAt != "2026-11-16T12:00:01Z" {
			t.Fatalf("after %s: generated %s, %v", after, out.GeneratedAt, err)
		}
	}
	if reads != 2 {
		t.Errorf("read the cluster %d times, want 2 (the failed one and one kept)", reads)
	}
	if out, _ := cache.Read(context.Background(), src, ciScoreNow.Add(10*time.Minute+2*time.Second)); out.GeneratedAt != "2026-11-16T12:10:02Z" || reads != 3 {
		t.Errorf("after the keep: generated %s, %d reads", out.GeneratedAt, reads)
	}
}
