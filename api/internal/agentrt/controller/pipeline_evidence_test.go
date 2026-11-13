package controller_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func sha(c string) string { return strings.Repeat(c, 40) }

// ciRun is a completed bifrost-ci-platform run as the cluster reports one:
// its own commit and tree, and the commits of the two repositories beside it.
func ciRun(name, commit, tree string) store.PipelineRun {
	p := completedRun(name)
	p.Pipeline, p.Kind, p.Revision, p.Tree, p.Reason = "bifrost-ci-platform", "ci", commit, tree, "Succeeded"
	p.Inputs = []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree},
		{Repo: "bifrost-trade-infra", Commit: sha("1")}, {Repo: "bifrost-ui", Commit: sha("2")}}
	return p
}

type evidenceRow struct{ run, key, result, scope, commit string }

func evidenceRows(t *testing.T, db store.DB) []evidenceRow {
	t.Helper()
	var out []evidenceRow
	must(t, store.InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		rows, err := tx.Query(ctx, `SELECT run_name, input_key, result, scope, commit_sha FROM agentrt.evidence ORDER BY evidence_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r evidenceRow
			if err = rows.Scan(&r.run, &r.key, &r.result, &r.scope, &r.commit); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}))
	return out
}

// A completed CI run gives one row of evidence, keyed by the trees it tested
// (read for the commits it reports without one); the same content under
// another commit has the same key; a run seen again writes nothing; a reused,
// a cancelled and a non-CI run are no evidence; a failed run is failed
// evidence; an incremental run says so.
func TestPipelineRecordsWritesEvidence(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	trees := map[string]string{"bifrost-trade-infra@" + sha("1"): sha("e"), "bifrost-ui@" + sha("2"): sha("f")}
	reads := 0
	green := ciRun("ci-platform-green", sha("a"), sha("c"))
	same := ciRun("ci-platform-same-tree", sha("b"), sha("c"))
	same.Scope = "incremental"
	red := ciRun("ci-platform-red", sha("d"), sha("9"))
	red.Result, red.Reason = "Failed", "Failed"
	reused := ciRun("ci-platform-reused", sha("a"), sha("c"))
	reused.TestedBy = "ci-platform-green"
	cancelled := ciRun("ci-platform-cancelled", sha("a"), sha("c"))
	cancelled.Result, cancelled.Reason = "Failed", "Cancelled"
	runs := []store.PipelineRun{green, same, red, reused, cancelled, completedRun("deliver-1")}
	c := controller.Controller{DB: pool, PipelineFullEvery: time.Nanosecond, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]store.PipelineRun, error) { return runs, nil },
		CommitTree: func(_ context.Context, repo, commit string) (string, error) {
			reads++
			return trees[repo+"@"+commit], nil
		}}}
	for pass := 0; pass < 3; pass++ { // every pass is a full one: each run is compared again
		if _, err := c.PipelineRecords(ctx); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	_, key, err := store.EvidenceInputs("bifrost-ci-platform", "bifrost-platform", sha("c"),
		[]store.RepoAt{{Repo: "bifrost-ui", Tree: sha("f")}, {Repo: "bifrost-trade-infra", Tree: sha("e")}})
	must(t, err)
	_, redKey, err := store.EvidenceInputs("bifrost-ci-platform", "bifrost-platform", sha("9"),
		[]store.RepoAt{{Repo: "bifrost-trade-infra", Tree: sha("e")}, {Repo: "bifrost-ui", Tree: sha("f")}})
	must(t, err)
	want := map[string]evidenceRow{
		"ci-platform-green":     {"ci-platform-green", key, "passed", "full", sha("a")},
		"ci-platform-same-tree": {"ci-platform-same-tree", key, "passed", "incremental", sha("b")},
		"ci-platform-red":       {"ci-platform-red", redKey, "failed", "full", sha("d")},
	}
	got := evidenceRows(t, pool)
	if len(got) != len(want) {
		t.Fatalf("rows: %+v", got)
	}
	for _, r := range got {
		if want[r.run] != r {
			t.Errorf("row %+v, want %+v", r, want[r.run])
		}
	}
	if key == redKey || reads != 2 {
		t.Fatalf("keys equal %v; %d tree reads, want 2 (one a commit)", key == redKey, reads)
	}
}

// A tree that cannot be read holds back the evidence and not the record: the
// run is recorded as before, and its evidence is written on a later full pass.
func TestPipelineRecordsEvidenceWaitsForATree(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	away := errors.New("gitea is away")
	fail := true
	c := controller.Controller{DB: pool, PipelineFullEvery: time.Hour, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]store.PipelineRun, error) {
			return []store.PipelineRun{ciRun("ci-platform-1", sha("a"), sha("c"))}, nil
		},
		CommitTree: func(context.Context, string, string) (string, error) {
			if fail {
				return "", away
			}
			return sha("e"), nil
		}}}
	n, err := c.PipelineRecords(ctx)
	if n != 1 || !errors.Is(err, away) || pipelineSeenCount(t, pool, "ci-platform-1") != 1 || len(evidenceRows(t, pool)) != 0 {
		t.Fatalf("first pass: %d %v", n, err)
	}
	fail = false
	if n, err = c.PipelineRecords(ctx); n != 0 || err != nil || len(evidenceRows(t, pool)) != 0 {
		t.Fatalf("a pass that compared nothing wrote evidence: %d %v", n, err)
	}
	c.PipelineFullEvery = time.Nanosecond
	if n, err = c.PipelineRecords(ctx); n != 0 || err != nil || len(evidenceRows(t, pool)) != 1 {
		t.Fatalf("full pass: %d %v, rows %+v", n, err, evidenceRows(t, pool))
	}
}

// The daily full CI failing on inputs the ledger holds a full pass for
// invalidates that pass; an ordinary failed run on the same inputs does not,
// and neither does the full CI failing on inputs with no pass.
func TestPipelineRecordsFullCIFailureInvalidatesAPass(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	tree := func(context.Context, string, string) (string, error) { return sha("e"), nil }
	fail := func(name, commit, treeID string, audit bool) store.PipelineRun {
		p := ciRun(name, commit, treeID)
		p.Result, p.Reason, p.Audit = "Failed", "Failed", audit
		return p
	}
	runs := []store.PipelineRun{ciRun("ci-platform-green", sha("a"), sha("c"))}
	c := controller.Controller{DB: pool, PipelineFullEvery: time.Nanosecond, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]store.PipelineRun, error) { return runs, nil }, CommitTree: tree}}
	results := func() string {
		t.Helper()
		if _, err := c.PipelineRecords(ctx); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range evidenceRows(t, pool) {
			out = append(out, r.run+":"+r.result)
		}
		return strings.Join(out, " ")
	}
	if got := results(); got != "ci-platform-green:passed" {
		t.Fatal(got)
	}
	runs = append(runs, fail("ci-platform-flake", sha("a"), sha("c"), false), fail("ci-platform-full-other", sha("d"), sha("9"), true))
	if got := results(); strings.Contains(got, "invalidated") {
		t.Fatalf("invalidated without the full CI contradicting a pass: %s", got)
	}
	runs = append(runs, fail("ci-platform-full-1", sha("a"), sha("c"), true))
	for pass := 0; pass < 2; pass++ { // seen again: nothing more
		if got := results(); strings.Count(got, "ci-platform-full-1:invalidated") != 1 || strings.Count(got, "ci-platform-full-1:failed") != 1 || strings.Count(got, "invalidated") != 1 {
			t.Fatalf("pass %d: %s", pass, got)
		}
	}
}

// A failed CI run's row says what it failed on, as its tasks said: the tests
// and the class in the statement, outside the key; a passed run and a run
// read without that source say nothing of it; a read that fails holds the row
// back until a later full pass.
func TestPipelineRecordsFailedRunSaysWhatFailed(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	red := ciRun("ci-platform-red", sha("d"), sha("9"))
	red.Result, red.Reason = "Failed", "Failed"
	build := ciRun("ci-platform-build", sha("b"), sha("8"))
	build.Result, build.Reason = "Failed", "Failed"
	away := errors.New("the cluster is away")
	fail := true
	asked := map[string]int{}
	c := controller.Controller{DB: pool, PipelineFullEvery: time.Nanosecond, Sources: controller.Sources{
		PipelineRuns: func(context.Context) ([]store.PipelineRun, error) {
			return []store.PipelineRun{ciRun("ci-platform-green", sha("a"), sha("c")), red, build}, nil
		},
		CommitTree: func(context.Context, string, string) (string, error) { return sha("e"), nil },
		RunFailure: func(_ context.Context, run string) ([]string, []string, string, error) {
			asked[run]++
			switch {
			case fail:
				return nil, nil, "", away
			case run == "ci-platform-red":
				return []string{"test-api"}, []string{"pkg/a.TestOne", "pkg/b.TestTwo"}, store.FailClassTest, nil
			}
			return []string{"test-api"}, nil, store.FailClassBuild, nil
		}}}
	if _, err := c.PipelineRecords(ctx); !errors.Is(err, away) {
		t.Fatalf("first pass: %v", err)
	}
	fail = false
	if _, err := c.PipelineRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if asked["ci-platform-green"] != 0 {
		t.Fatal("a passed run was asked what it failed on")
	}
	got := map[string]string{}
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT run_name, COALESCE(statement->'predicate'->>'fail_class','') || ' ' ||
		  COALESCE(statement->'predicate'->'failedTests','[]')::text || ' ' || COALESCE(statement->'predicate'->'failed_tasks','[]')::text FROM agentrt.evidence`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run, v string
			if err = rows.Scan(&run, &v); err != nil {
				return err
			}
			got[run] = v
		}
		return rows.Err()
	}))
	want := map[string]string{
		"ci-platform-green": ` [] []`,
		"ci-platform-red":   `test ["pkg/a.TestOne", "pkg/b.TestTwo"] ["test-api"]`,
		"ci-platform-build": `build [] ["test-api"]`,
	}
	for run, v := range want {
		if got[run] != v {
			t.Errorf("%s: %q, want %q", run, got[run], v)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("rows: %v", got)
	}
}
