package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
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

// The comparison must not excuse what the gate refuses (review 2026-11-13,
// A5). as_tested was "the commit has some passed, full row": a pass the daily
// full CI later invalidated still counted, and so did a unit task's row, and
// either was filed under beside_moved, the class that means "the gate takes
// it". The gate's own reading decides the class now.
func TestCompareEvidenceFollowsTheGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	h := func(c string) string { return strings.Repeat(c, 40) }
	trees := map[string]string{"bifrost-platform@" + h("e"): h("1"), "bifrost-platform@" + h("9"): h("2"), "bifrost-ui@" + h("3"): h("0")}
	tree := func(_ context.Context, repo, commit string) (string, error) {
		if v, ok := trees[repo+"@"+commit]; ok {
			return v, nil
		}
		return "", errors.New("no such commit")
	}
	main := func(_ context.Context, repo string) (string, error) { return h("3"), nil }
	ci := func(name, commit, result string, audit bool) {
		t.Helper()
		p := store.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: result,
			Repo: "bifrost-platform", Scope: "full", Audit: audit, CompletedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
			Inputs: []store.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: trees["bifrost-platform@"+commit]}, {Repo: "bifrost-ui", Commit: h("3"), Tree: h("0")}}}
		e, is, err := store.RunEvidence(p)
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = store.RecordEvidence(ctx, pool, []store.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	// e: passed in full, then the daily full CI failed on the same inputs.
	ci("ci-e", h("e"), "Succeeded", false)
	ci("ci-e-audit", h("e"), "Failed", true)
	// 9: its whole CI never ran; one unit task of it passed.
	inputs := map[string]any{"v": 2, "repo": "bifrost-platform", "unit": "api/internal/x", "task": "test"}
	raw, _ := json.Marshal(inputs)
	row, err := store.UnitEvidenceRow(store.UnitEvidence{Repo: "bifrost-platform", Unit: "api/internal/x", Task: "test", InputKey: unitplan.Hash(inputs), Inputs: raw,
		Passed: true, Commit: h("9"), Tree: h("2"), RunName: "unit-1", RunUID: "uid-unit-1", CompletedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RecordEvidence(ctx, pool, []store.EvidenceRow{row}); err != nil {
		t.Fatal(err)
	}
	index := map[string]delivery.CIOutcome{
		"bifrost-platform@" + h("e"): {Status: delivery.CISucceeded, Run: "ci-e"},
		"bifrost-platform@" + h("9"): {Status: delivery.CISucceeded, Run: "ci-9"},
	}
	out := compareEvidence(ctx, pool, index, tree, main)
	got := map[string]evidenceComparison{}
	for _, c := range out.Commits {
		got[c.Commit[:1]] = c
	}
	if c := got["e"]; c.Class != "ledger_red" || c.AsTested || c.Gate != delivery.CIFailed {
		t.Errorf("a pass the full CI invalidated: %+v", c)
	}
	if c := got["9"]; c.Class != "not_in_ledger" || c.AsTested || c.Gate != "" {
		t.Errorf("a commit with a unit task's pass alone: %+v", c)
	}
}
