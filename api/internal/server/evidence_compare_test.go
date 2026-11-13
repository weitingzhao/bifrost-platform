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

// Each commit of the old reading is put in one class: agree, beside_moved (the
// full pass is in the ledger but a repository beside it moved), not_in_ledger
// (old green, no full pass recorded), ledger_only (the same tree passed under
// another commit); a running one and one Gitea cannot read are skipped.
func TestCompareEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	h := func(c string) string { return strings.Repeat(c, 40) }
	trees := map[string]string{
		"bifrost-platform@" + h("a"): h("c"), "bifrost-platform@" + h("b"): h("c"), "bifrost-platform@" + h("d"): h("9"),
		"bifrost-platform@" + h("6"): h("7"), "bifrost-platform@" + h("4"): h("5"),
		"bifrost-ui@" + h("2"): h("f"), "bifrost-ui@" + h("3"): h("0"),
	}
	mains := map[string]string{"bifrost-ui": h("3")}
	reads := 0
	tree := func(_ context.Context, repo, commit string) (string, error) {
		reads++
		if v, ok := trees[repo+"@"+commit]; ok {
			return v, nil
		}
		return "", errors.New("no such commit")
	}
	main := func(_ context.Context, repo string) (string, error) { return mains[repo], nil }
	record := func(name, commit, ui, scope string) {
		t.Helper()
		p := store.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: "Succeeded", Reason: "Succeeded",
			Repo: "bifrost-platform", Scope: scope, CompletedAt: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
			Inputs: []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: trees["bifrost-platform@"+commit]}, {Repo: "bifrost-ui", Commit: ui, Tree: trees["bifrost-ui@"+ui]}}}
		e, is, err := store.RunEvidence(p)
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = store.RecordEvidence(ctx, pool, []store.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	record("ci-a", h("a"), h("3"), "full")        // a: full pass with ui's main as it is now
	record("ci-d", h("d"), h("2"), "full")        // d: full pass with ui's older main
	record("ci-6", h("6"), h("3"), "incremental") // 6: only part tested
	index := map[string]delivery.CIOutcome{
		"bifrost-platform@" + h("a"): {Status: delivery.CISucceeded, Run: "ci-a"},
		"bifrost-platform@" + h("b"): {Status: delivery.CIFailed, Run: "ci-b"}, // same tree as a
		"bifrost-platform@" + h("d"): {Status: delivery.CISucceeded, Run: "ci-d"},
		"bifrost-platform@" + h("6"): {Status: delivery.CISucceeded, Run: "ci-6"},
		"bifrost-platform@" + h("4"): {Status: delivery.CIFailed, Run: "ci-4"},
		"bifrost-platform@" + h("1"): {Status: delivery.CISucceeded, Run: "ci-1"}, // Gitea has no such commit
		"bifrost-platform@" + h("8"): {Status: delivery.CIRunning, Run: "ci-8"},
	}
	out := compareEvidence(ctx, pool, index, tree, main)
	got := map[string]string{}
	for _, c := range out.Commits {
		got[c.Commit[:1]] = c.Class
	}
	want := map[string]string{"a": "agree", "b": "ledger_only", "d": "beside_moved", "6": "not_in_ledger", "4": "agree"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("commit %s: %q, want %q", k, got[k], v)
		}
	}
	if len(out.Commits) != len(want) || len(out.Skipped) != 2 || out.Counts["agree"] != 2 || out.Counts["not_in_ledger"] != 1 {
		t.Fatalf("answer: %+v", out)
	}
	if reads != 7 { // six commits and ui's main, each once
		t.Fatalf("%d tree reads", reads)
	}
}
