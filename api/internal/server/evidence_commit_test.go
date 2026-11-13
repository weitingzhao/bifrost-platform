package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The answer for a commit follows the rows of the key it has now: none with
// an empty ledger; green after a full pass on its tree and the main trees
// beside it; the same for another commit with the same tree; none (with the
// commit's own rows) once a repository beside it moved, or when only an
// incremental run passed; red after a failure with no full pass.
func TestCommitEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	h := func(c string) string { return strings.Repeat(c, 40) }
	// Commits a, b share tree c; d has tree 9. Main of ui is commit 2.
	trees := map[string]string{
		"bifrost-platform@" + h("a"): h("c"), "bifrost-platform@" + h("b"): h("c"), "bifrost-platform@" + h("d"): h("9"),
		"bifrost-platform@" + h("7"): h("8"),
		"bifrost-ui@" + h("2"):       h("f"), "bifrost-ui@" + h("3"): h("0"),
	}
	mains := map[string]string{"bifrost-ui": h("2")}
	tree := func(_ context.Context, repo, commit string) (string, error) {
		if v, ok := trees[repo+"@"+commit]; ok {
			return v, nil
		}
		return "", errors.New("no such commit")
	}
	main := func(_ context.Context, repo string) (string, error) { return mains[repo], nil }
	ask := func(commit, verdict string, taskRows, commitRows int) evidenceAnswer {
		t.Helper()
		out, err := commitEvidence(ctx, pool, tree, main, "bifrost-platform", commit)
		if err != nil {
			t.Fatal(err)
		}
		rows := 0
		for _, task := range out.Tasks {
			rows += len(task.Rows)
		}
		if out.Verdict != verdict || rows != taskRows || len(out.CommitRows) != commitRows || out.Reason == "" {
			t.Fatalf("commit %s: verdict %s (want %s), %d key rows (want %d), %d commit rows (want %d): %s",
				commit[:1], out.Verdict, verdict, rows, taskRows, len(out.CommitRows), commitRows, out.Reason)
		}
		return out
	}
	record := func(name, commit, result, reason, scope string) {
		t.Helper()
		at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
		p := store.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: reason, Repo: "bifrost-platform",
			Scope: scope, CompletedAt: at, Inputs: []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: trees["bifrost-platform@"+commit]},
				{Repo: "bifrost-ui", Commit: mains["bifrost-ui"], Tree: trees["bifrost-ui@"+mains["bifrost-ui"]]}}}
		e, is, err := store.RunEvidence(p)
		if err != nil || !is {
			t.Fatalf("run %s is no evidence: %v", name, err)
		}
		if n, err := store.RecordEvidence(ctx, pool, []store.EvidenceRow{e}); err != nil || n != 1 {
			t.Fatalf("record %s: %d %v", name, n, err)
		}
	}
	ask(h("a"), store.EvidenceNone, 0, 0) // an empty ledger

	record("ci-1", h("d"), "Failed", "Failed", "")
	ask(h("d"), store.EvidenceRed, 1, 1)
	ask(h("a"), store.EvidenceNone, 0, 0)

	record("ci-2", h("a"), "Succeeded", "Succeeded", "incremental")
	ask(h("a"), store.EvidenceNone, 1, 1) // only part of it was tested

	record("ci-3", h("a"), "Succeeded", "Succeeded", "full")
	out := ask(h("a"), store.EvidenceGreen, 2, 2)
	if len(out.Tasks) != 1 || out.Tasks[0].Task != "bifrost-ci-platform" || len(out.Tasks[0].Beside) != 1 || out.Tasks[0].Beside[0].Commit != h("2") || out.Tree != h("c") {
		t.Fatalf("answer: %+v", out)
	}
	ask(h("b"), store.EvidenceGreen, 2, 0) // the same tree under another commit

	record("ci-4", h("d"), "Succeeded", "Succeeded", "full")
	ask(h("d"), store.EvidenceGreen, 2, 2) // a full pass after the failure

	mains["bifrost-ui"] = h("3") // ui's main moved: the key changed, the commit's rows remain
	ask(h("a"), store.EvidenceNone, 0, 2)

	if _, err := commitEvidence(ctx, pool, tree, main, "bifrost-platform", h("5")); err == nil {
		t.Fatal("a commit Gitea does not have was answered")
	}
}
