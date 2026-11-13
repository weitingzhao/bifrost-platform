package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

func TestEvidenceGateOn(t *testing.T) {
	for _, tc := range []struct {
		value, env string
		want       bool
	}{{"", "stg", true}, {"", "prod", true}, {"", "dev", false}, {"", "", false}, {"ledger", "dev", true}, {"runs", "stg", false}, {"runs", "prod", false}, {" Ledger ", "dev", true}, {"yes", "dev", false}, {"yes", "stg", true}} {
		t.Setenv(evidenceGateEnv, tc.value)
		if got := evidenceGateOn(tc.env); got != tc.want {
			t.Errorf("%s=%q on %q: %v", evidenceGateEnv, tc.value, tc.env, got)
		}
	}
}

// The ledger's answer for a commit: Succeeded with its own full pass,
// whatever a repository beside it did since; nothing for a pass of part of
// it; Failed with only a failure; nothing once the full CI invalidated the
// pass; an error with no database.
func TestLedgerCI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	h := func(c string) string { return strings.Repeat(c, 40) }
	record := func(name, commit, tree, result, scope string, audit bool) {
		t.Helper()
		reason := "Succeeded"
		if result == "Failed" {
			reason = "Failed"
		}
		p := store.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: reason, Repo: "bifrost-platform",
			Scope: scope, Audit: audit, CompletedAt: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
			Inputs: []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree}, {Repo: "bifrost-ui", Commit: h("2"), Tree: h("f")}}}
		e, is, err := store.RunEvidence(p)
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = store.RecordEvidence(ctx, pool, []store.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	trees := map[string]string{h("a"): h("c"), h("b"): h("c"), h("d"): h("9"), h("e"): h("9")}
	read := ledgerCI(func() store.DB { return pool }, func(_ context.Context, _, commit string) (string, error) {
		if t, ok := trees[commit]; ok {
			return t, nil
		}
		return "", errors.New("no such commit")
	})
	ask := func(commit, status, run string) {
		t.Helper()
		gotStatus, gotRun, err := read(ctx, "bifrost-platform", commit)
		if err != nil || gotStatus != status || gotRun != run {
			t.Fatalf("commit %s: %q %q %v, want %q %q", commit[:1], gotStatus, gotRun, err, status, run)
		}
	}
	ask(h("a"), "", "")
	record("ci-part", h("a"), h("c"), "Succeeded", "incremental", false)
	ask(h("a"), "", "") // only part of it was tested
	record("ci-red", h("a"), h("c"), "Failed", "", false)
	ask(h("a"), delivery.CIFailed, "ci-red")
	record("ci-full", h("a"), h("c"), "Succeeded", "full", false)
	ask(h("a"), delivery.CISucceeded, "ci-full") // ui's main moving on changes nothing here: the commit's own rows answer
	ask(h("b"), delivery.CISucceeded, "ci-full") // the same tree under a commit that was never run: the content passed
	ask(h("7"), "", "")                          // a commit whose tree cannot be read has its own answer: none

	record("ci-d", h("d"), h("9"), "Succeeded", "full", false)
	ask(h("d"), delivery.CISucceeded, "ci-d")
	record("ci-full-audit", h("d"), h("9"), "Failed", "", true) // the daily full CI contradicts it
	ask(h("d"), delivery.CIFailed, "ci-full-audit")
	ask(h("e"), delivery.CIFailed, "ci-full-audit") // and so for the same tree under another commit

	// The push to main also counts a pass of part of the repository, unless
	// a newer run failed.
	record("ci-console", h("3"), h("4"), "Succeeded", "console", false)
	anyScope := ledgerCIScoped(func() store.DB { return pool }, nil, true)
	for _, tc := range []struct{ commit, status, run string }{{h("3"), delivery.CISucceeded, "ci-console"}, {h("a"), delivery.CISucceeded, "ci-full"}, {h("d"), delivery.CIFailed, "ci-full-audit"}} {
		if status, run, err := anyScope(ctx, "bifrost-platform", tc.commit); err != nil || status != tc.status || run != tc.run {
			t.Fatalf("any scope, commit %s: %q %q %v", tc.commit[:1], status, run, err)
		}
	}
	ask(h("3"), "", "")
	record("ci-console-red", h("3"), h("4"), "Failed", "", false)
	if status, run, _ := anyScope(ctx, "bifrost-platform", h("3")); status != delivery.CIFailed || run != "ci-console-red" {
		t.Fatalf("a pass of part before a failure still counts: %q %q", status, run)
	}
	if _, _, err := ledgerCI(func() store.DB { return nil }, nil)(ctx, "bifrost-platform", h("a")); err == nil {
		t.Fatal("no database, and an answer")
	}
}

// A unit task's row is not the repository's CI (review 2026-11-13, A1). The
// rows unit tasks write are scope full too: a commit with one unit task
// passed was answered Succeeded, whatever its whole CI did; and the rows of
// the unit named "repo" (the repository's own checks) carry a tree, so the
// same held for any commit of that tree. Only the rows a CI pipeline's run
// wrote say whether the commit's CI passed.
func TestLedgerCIIsNotAUnitTasksWord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	h := func(c string) string { return strings.Repeat(c, 40) }
	unitRow := func(run, unit, task, commit string, inputs map[string]any) {
		t.Helper()
		raw, err := json.Marshal(inputs)
		if err != nil {
			t.Fatal(err)
		}
		row, err := store.UnitEvidenceRow(store.UnitEvidence{Repo: "bifrost-platform", Unit: unit, Task: task, InputKey: unitplan.Hash(inputs), Inputs: raw,
			Passed: true, Commit: commit, Tree: h("c"), RunName: run, RunUID: "uid-" + run, CompletedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if n, err := store.RecordEvidence(ctx, pool, []store.EvidenceRow{row}); err != nil || n != 1 {
			t.Fatalf("unit row %s: %d %v", run, n, err)
		}
	}
	ci := func(name, commit, tree, result string) {
		t.Helper()
		p := store.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: result, Repo: "bifrost-platform",
			Scope: "full", CompletedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
			Inputs: []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree}}}
		e, is, err := store.RunEvidence(p)
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = store.RecordEvidence(ctx, pool, []store.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	trees := map[string]string{h("a"): h("c"), h("b"): h("c"), h("d"): h("8"), h("e"): h("7")}
	tree := func(_ context.Context, _, commit string) (string, error) { return trees[commit], nil }
	for _, anyScope := range []bool{false, true} {
		read := ledgerCIScoped(func() store.DB { return pool }, tree, anyScope)
		ask := func(when, commit, status, run string) {
			t.Helper()
			gotStatus, gotRun, err := read(ctx, "bifrost-platform", commit)
			if err != nil || gotStatus != status || gotRun != run {
				t.Fatalf("any scope %v, %s, commit %s: %q %q %v, want %q %q", anyScope, when, commit[:1], gotStatus, gotRun, err, status, run)
			}
		}
		if !anyScope {
			// A package's test task passed for commit a; its whole CI never ran.
			unitRow("unit-1", "api/internal/x", "test", h("a"), map[string]any{"v": 2, "repo": "bifrost-platform", "unit": "api/internal/x", "task": "test"})
			// The repository's own checks passed for commit a: unit "repo", a tree in its inputs.
			unitRow("unit-2", "repo", "check", h("a"), map[string]any{"v": 2, "repo": "bifrost-platform", "unit": "repo", "task": "check", "tree": h("c")})
			// Commit d: its whole CI failed, a unit task of it passed afterwards.
			ci("ci-d-red", h("d"), h("8"), "Failed")
			unitRow("unit-3", "api/internal/x", "test", h("d"), map[string]any{"v": 2, "repo": "bifrost-platform", "unit": "api/internal/x", "task": "test", "n": 3})
			// Commit e: its whole CI passed, then many unit rows came after it.
			ci("ci-e", h("e"), h("7"), "Succeeded")
			for i := 0; i < 60; i++ {
				unitRow(fmt.Sprintf("unit-e-%d", i), "api/internal/x", "test", h("e"), map[string]any{"v": 2, "repo": "bifrost-platform", "unit": "api/internal/x", "task": "test", "n": 100 + i})
			}
		}
		ask("only unit tasks passed", h("a"), "", "")
		ask("the same tree, only unit tasks passed", h("b"), "", "")
		ask("the whole CI failed and a unit task passed", h("d"), delivery.CIFailed, "ci-d-red")
		ask("the whole CI passed and sixty unit rows followed", h("e"), delivery.CISucceeded, "ci-e")
	}
}

// Three ways the ledger's own bookkeeping turned a true verdict (review
// 2026-11-13: B1, B2, A2). Each is one commit with one key.
func TestLedgerCIBookkeeping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	h := func(c string) string { return strings.Repeat(c, 40) }
	run := func(name, commit, tree, result string, audit bool, at time.Time) store.EvidenceRow {
		t.Helper()
		p := store.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: result, Repo: "bifrost-platform",
			Scope: "full", Audit: audit, CompletedAt: at, Inputs: []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree}}}
		e, is, err := store.RunEvidence(p)
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		return e
	}
	write := func(rows ...store.EvidenceRow) {
		t.Helper()
		if _, err := store.RecordEvidence(ctx, pool, rows); err != nil {
			t.Fatal(err)
		}
	}
	read := ledgerCI(func() store.DB { return pool }, nil)
	ask := func(when, commit, status string) {
		t.Helper()
		if got, run, err := read(ctx, "bifrost-platform", commit); err != nil || got != status {
			t.Errorf("%s: %q (%s) %v, want %q", when, got, run, err, status)
		}
	}
	t1 := time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
	t2, t3 := t1.Add(time.Hour), t1.Add(2*time.Hour)

	// B1: the daily full CI failed, a later full run passed, and the failed
	// run is seen again (a restart forgets what was recorded). The pass holds.
	f := run("b1-audit", h("1"), h("a"), "Failed", true, t1)
	write(f)
	write(run("b1-pass", h("1"), h("a"), "Succeeded", false, t2))
	ask("B1, before the failure is seen again", h("1"), delivery.CISucceeded)
	write(f)
	ask("B1, the old failure seen again", h("1"), delivery.CISucceeded)

	// B2: a full pass, then fifty ordinary failures of the same commit. An
	// ordinary failure does not invalidate a full pass, however many follow.
	write(run("b2-pass", h("2"), h("b"), "Succeeded", false, t1))
	for i := 0; i < 50; i++ {
		write(run(fmt.Sprintf("b2-red-%d", i), h("2"), h("b"), "Failed", false, t2))
	}
	ask("B2, a full pass under fifty failures", h("2"), delivery.CISucceeded)

	// A2: a full pass at t1 and the daily full CI's failure at t3, both seen
	// for the first time in one batch, the failure listed first. The later
	// failure invalidates the pass whatever the order of the batch.
	write(run("a2-audit", h("3"), h("c"), "Failed", true, t3), run("a2-pass", h("3"), h("c"), "Succeeded", false, t1))
	ask("A2, the failure listed before the pass it follows", h("3"), delivery.CIFailed)
	write(run("a2b-pass", h("4"), h("d"), "Succeeded", false, t1), run("a2b-audit", h("4"), h("d"), "Failed", true, t3))
	ask("A2, the same in order", h("4"), delivery.CIFailed)
}
