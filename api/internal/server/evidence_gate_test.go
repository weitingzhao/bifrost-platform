package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

func TestEvidenceGateOn(t *testing.T) {
	for _, tc := range []struct {
		value, env string
		want       bool
	}{{"", "stg", true}, {"", "prod", false}, {"", "", false}, {"ledger", "prod", true}, {"runs", "stg", false}, {" Ledger ", "prod", true}, {"yes", "prod", false}, {"yes", "stg", true}} {
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
