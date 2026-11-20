package evalreadings

import (
	"context"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// failedTaskUnrecorded labels a failed run the evidence ledger has no failed
// row with task names for (a land-* run writes none; neither does a run whose
// tasks did not say).
const failedTaskUnrecorded = "unrecorded"

// FailedTasks is what the CI failure reading asks of the verification
// domain: the pipeline tasks a run failed on, as its evidence ledger row says
// (statement.predicate.failed_tasks); none when the ledger has no failed row
// for the run. The server lends it, reading through the evidence package; this
// package does not import another domain (codeguard domain walls).
type FailedTasks func(ctx context.Context, repo, commit, run string) ([]string, error)

// ciFailureReadings: the failed kind=ci runs completed in the window, by
// pipeline and by the pipeline task that failed (the evidence row's
// statement.predicate.failed_tasks). The CI line asked on 2026-11-20 to tell
// guard tests that stopped a commit (test-api, code-health) apart from CI's
// own faults (ci-infra): a fall in ci_success_ratio was mostly the former.
// A run with two failed tasks counts under each.
func ciFailureReadings(rows []eventlog.Row, tasksOf func(repo, commit, run string) ([]string, error), w Window) []Reading {
	src := "event ledger pipeline_seen kind=ci result=Failed, completed in window; failed tasks from the evidence ledger row of the same run"
	d := def(src, "count of failed runs by pipeline and failed pipeline task")
	type key struct{ pipeline, task string }
	counts := map[key]int{}
	failed := 0
	for _, r := range rows {
		b := r.Body
		if str(b, "kind") != "ci" || str(b, "result") != "Failed" {
			continue
		}
		done, ok := instant(b, "completed_at")
		if !ok || !in(done, w) {
			continue
		}
		failed++
		p := str(b, "pipeline")
		var tasks []string
		if tasksOf != nil {
			// A failed lookup leaves the run unrecorded rather than failing the readout.
			tasks, _ = tasksOf(str(b, "repo"), str(b, "revision"), str(b, "run"))
		}
		if len(tasks) == 0 {
			tasks = []string{failedTaskUnrecorded}
		}
		for _, t := range tasks {
			counts[key{p, t}]++
		}
	}
	if failed == 0 {
		// pipeline_seen records every run: no failed run is a measured zero.
		r := Reading{Name: "ci_failures", Unit: "count", Window: w, Definition: d}
		r = finish(r, false)
		measuredZero(&r)
		return []Reading{r}
	}
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pipeline != keys[j].pipeline {
			return keys[i].pipeline < keys[j].pipeline
		}
		return keys[i].task < keys[j].task
	})
	out := make([]Reading, 0, len(keys))
	for _, k := range keys {
		v := float64(counts[k])
		out = append(out, finish(Reading{Name: "ci_failures", Labels: map[string]string{"pipeline": k.pipeline, "failed_task": k.task},
			Value: &v, Unit: "count", Samples: counts[k], Window: w, Definition: d}, false))
	}
	return out
}
