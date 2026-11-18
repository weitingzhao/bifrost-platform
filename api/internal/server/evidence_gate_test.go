//bifrost:owner verification

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// 2026-11-16: the runs reading was kept as the way back from the evidence
// ledger; after incremental landing it could not stand in for it, so it is
// retired; a CI question with no ledger answers no verdict, never a pass.
//
// Wiring sets the ledger as the delivery service's reader in every
// environment, EVIDENCE_GATE=runs or not: with no ledger connected the
// policy's CI question and the runtime's CI source have no verdict. A server
// with no delivery service wires nothing.
func TestWireEvidenceGateIsTheLedgerEverywhere(t *testing.T) {
	t.Setenv("EVIDENCE_GATE", "runs") // no longer read
	ctx := context.Background()
	commit := strings.Repeat("a", 40)
	for _, env := range []string{"", "dev", "stg", "prod"} {
		s := &Server{delivery: delivery.NewHandlerForTest(delivery.NewService(&config.ClusterEntry{ID: "test"}), nil)}
		s.agentRuntime = &agentRuntime{env: env}
		reader := s.wireEvidenceGate()
		if reader == nil {
			t.Fatalf("env %q: no reader wired", env)
		}
		s.agentRuntime.evidenceCI = reader
		facts := s.delivery.PolicyFacts()
		status, run, err := facts.CIRun(ctx, "bifrost-platform", commit)
		if err == nil || !strings.Contains(err.Error(), "the evidence ledger is not connected") || status != "" || run != "" {
			t.Errorf("env %q: CIRun with no ledger = %q %q %v", env, status, run, err)
		}
		if ok, err := facts.Succeeded(ctx, "bifrost-platform", commit); ok || err == nil {
			t.Errorf("env %q: Succeeded with no ledger = %v %v", env, ok, err)
		}
		if status, run, ok := s.agentRuntimeSources().CI(ctx, "bifrost-platform", commit); ok || status != "" || run != "" {
			t.Errorf("env %q: the runtime read CI with no ledger: %q %q", env, status, run)
		}
	}
	if (&Server{}).wireEvidenceGate() != nil {
		t.Error("a server with no delivery service wired a reader")
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
		p := seenfacts.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: reason, Repo: "bifrost-platform",
			Scope: scope, Audit: audit, CompletedAt: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
			Inputs: []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree}, {Repo: "bifrost-ui", Commit: h("2"), Tree: h("f")}}}
		e, is, err := evidence.RunEvidence(controller.EvidenceRun(p))
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = evidence.RecordEvidence(ctx, pool, []evidence.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	trees := map[string]string{h("a"): h("c"), h("b"): h("c"), h("d"): h("9"), h("e"): h("9")}
	read := ledgerCI(func() eventlog.DB { return pool }, func(_ context.Context, _, commit string) (string, error) {
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
	anyScope := ledgerCIScoped(func() eventlog.DB { return pool }, nil, true)
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
	if _, _, err := ledgerCI(func() eventlog.DB { return nil }, nil)(ctx, "bifrost-platform", h("a")); err == nil {
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
		row, err := evidence.UnitEvidenceRow(evidence.UnitEvidence{Repo: "bifrost-platform", Unit: unit, Task: task, InputKey: unitplan.Hash(inputs), Inputs: raw,
			Passed: true, Commit: commit, Tree: h("c"), RunName: run, RunUID: "uid-" + run, CompletedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		if n, err := evidence.RecordEvidence(ctx, pool, []evidence.EvidenceRow{row}); err != nil || n != 1 {
			t.Fatalf("unit row %s: %d %v", run, n, err)
		}
	}
	ci := func(name, commit, tree, result string) {
		t.Helper()
		p := seenfacts.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: result, Repo: "bifrost-platform",
			Scope: "full", CompletedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
			Inputs: []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree}}}
		e, is, err := evidence.RunEvidence(controller.EvidenceRun(p))
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = evidence.RecordEvidence(ctx, pool, []evidence.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	trees := map[string]string{h("a"): h("c"), h("b"): h("c"), h("d"): h("8"), h("e"): h("7")}
	tree := func(_ context.Context, _, commit string) (string, error) { return trees[commit], nil }
	for _, anyScope := range []bool{false, true} {
		read := ledgerCIScoped(func() eventlog.DB { return pool }, tree, anyScope)
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
	run := func(name, commit, tree, result string, audit bool, at time.Time) evidence.EvidenceRow {
		t.Helper()
		p := seenfacts.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: result, Repo: "bifrost-platform",
			Scope: "full", Audit: audit, CompletedAt: at, Inputs: []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: commit, Tree: tree}}}
		e, is, err := evidence.RunEvidence(controller.EvidenceRun(p))
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		return e
	}
	write := func(rows ...evidence.EvidenceRow) {
		t.Helper()
		if _, err := evidence.RecordEvidence(ctx, pool, rows); err != nil {
			t.Fatal(err)
		}
	}
	read := ledgerCI(func() eventlog.DB { return pool }, nil)
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

// Origin: 2026-11-15. The landing gate was made to accept an incremental CI
// run judged against main's head (infra 002bb598, applied 13:31 Chicago). The
// release side took only a full pass as a commit's verdict, so after two such
// landings (platform 1999f61b, 3c40a33f) main's head had none: head-release
// waited, STG was not delivered from 12:44 to 14:30, and the console skip was
// lost with it (units/scope: "no passing full CI of the base"). It was turned
// off again. An incremental pass now is a verdict together with its base's.
func TestLedgerCIFollowsAnIncrementalPassToItsBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	seq := 0
	commit := func() string { seq++; return fmt.Sprintf("%040x", 0xc0000+seq) }
	treeOf := map[string]string{}
	record := func(name, c, result, scope, base string) {
		t.Helper()
		if treeOf[c] == "" {
			treeOf[c] = fmt.Sprintf("%040x", 0x7000000+len(treeOf))
		}
		reason := "Succeeded"
		if result == "Failed" {
			reason = "Failed"
		}
		p := seenfacts.PipelineRun{Run: name, UID: "uid-" + name, Pipeline: "bifrost-ci-platform", Kind: "ci", Result: result, Reason: reason, Repo: "bifrost-platform",
			Scope: scope, Base: base, CompletedAt: time.Date(2026, 11, 16, 0, 0, seq, 0, time.UTC),
			Inputs: []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: c, Tree: treeOf[c]}, {Repo: "bifrost-ui", Commit: strings.Repeat("2", 40), Tree: strings.Repeat("f", 40)}}}
		e, is, err := evidence.RunEvidence(controller.EvidenceRun(p))
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = evidence.RecordEvidence(ctx, pool, []evidence.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	read := ledgerCI(func() eventlog.DB { return pool }, func(_ context.Context, _, c string) (string, error) {
		if tr, ok := treeOf[c]; ok {
			return tr, nil
		}
		return "", errors.New("no such commit")
	})
	ask := func(what, c, status, run string) {
		t.Helper()
		gotStatus, gotRun, err := read(ctx, "bifrost-platform", c)
		if err != nil || gotStatus != status || gotRun != run {
			t.Fatalf("%s: %q %q %v, want %q %q", what, gotStatus, gotRun, err, status, run)
		}
	}

	// A full pass, then two incremental landings on top of it.
	a, b, c := commit(), commit(), commit()
	record("full-a", a, "Succeeded", "full", "")
	record("incr-b", b, "Succeeded", "incremental", a)
	record("incr-c", c, "Succeeded", "incremental", b)
	ask("the base", a, delivery.CISucceeded, "full-a")
	ask("one step", b, delivery.CISucceeded, "incr-b")
	ask("two steps", c, delivery.CISucceeded, "incr-c")

	// What PROD is judged on does not follow the chain: b and c have no
	// verdict there until a full run of theirs passes, and then they have.
	strict := ledgerCIStrict(func() eventlog.DB { return pool }, nil)
	for what, sha := range map[string]string{"one step, strict": b, "two steps, strict": c} {
		if st, run, err := strict(ctx, "bifrost-platform", sha); err != nil || st != "" || run != "" {
			t.Fatalf("%s: %q %q %v, want no verdict", what, st, run, err)
		}
	}
	if st, _, _ := strict(ctx, "bifrost-platform", a); st != delivery.CISucceeded {
		t.Fatalf("the full pass, strict: %q", st)
	}
	record("full-b", b, "Succeeded", "full", "")
	if st, run, _ := strict(ctx, "bifrost-platform", b); st != delivery.CISucceeded || run != "full-b" {
		t.Fatalf("after the full run of the push to main, strict: %q %q", st, run)
	}

	// Controls: each is an incremental pass that must not be a verdict.
	// Its base has no verdict at all.
	orphanBase, orphan := commit(), commit()
	record("incr-orphan", orphan, "Succeeded", "incremental", orphanBase)
	ask("a base nothing was recorded for", orphan, "", "")
	// Its base failed: the commit has no verdict (and is not called failed).
	red, onRed := commit(), commit()
	record("full-red", red, "Failed", "full", "")
	record("incr-on-red", onRed, "Succeeded", "incremental", red)
	ask("the failed base itself", red, delivery.CIFailed, "full-red")
	ask("a base that failed", onRed, "", "")
	// It names no base (a run from before the result existed, or the word none).
	noBase := commit()
	record("incr-no-base", noBase, "Succeeded", "incremental", "")
	ask("an incremental pass that names no base", noBase, "", "")
	// A console-scope pass is a pass of part with no base: as before.
	consoleOnly := commit()
	record("console-part", consoleOnly, "Succeeded", "console", a)
	ask("a console-scope pass", consoleOnly, "", "")
	// A later failure of the commit itself outranks the chain.
	later := commit()
	record("incr-later", later, "Succeeded", "incremental", a)
	record("full-later-red", later, "Failed", "full", "")
	ask("a failure recorded after the incremental pass", later, delivery.CIFailed, "full-later-red")
	// The chain is bounded: maxVerdictChain steps reach the full pass, one more does not.
	prev, name := a, ""
	for i := 1; i <= maxVerdictChain+1; i++ {
		next := commit()
		name = fmt.Sprintf("incr-chain-%d", i)
		record(name, next, "Succeeded", "incremental", prev)
		if i == maxVerdictChain {
			ask("a chain of the bound", next, delivery.CISucceeded, name)
		}
		prev = next
	}
	ask("a chain one longer than the bound", prev, "", "")
	// A base that names itself goes nowhere.
	self := commit()
	record("incr-self", self, "Succeeded", "incremental", self)
	ask("a row whose base is the commit", self, "", "")
}

func TestIncrementalBaseReadsOnlyAPassedIncrementalRow(t *testing.T) {
	base := strings.Repeat("b", 40)
	row := func(result, scope, b string) evidence.EvidenceRow {
		st, _ := json.Marshal(map[string]any{"predicate": map[string]any{"base_commit": b}})
		return evidence.EvidenceRow{Result: result, Scope: scope, Statement: st}
	}
	if got := evidence.IncrementalBase(row(evidence.EvidencePassed, evidence.EvidenceScopeIncremental, base)); got != base {
		t.Fatalf("got %q", got)
	}
	for name, r := range map[string]evidence.EvidenceRow{
		"failed":       row(evidence.EvidenceFailed, evidence.EvidenceScopeIncremental, base),
		"full":         row(evidence.EvidencePassed, evidence.EvidenceScopeFull, base),
		"console":      row(evidence.EvidencePassed, "console", base),
		"not a commit": row(evidence.EvidencePassed, evidence.EvidenceScopeIncremental, "none"),
		"no statement": {Result: evidence.EvidencePassed, Scope: evidence.EvidenceScopeIncremental},
	} {
		if got := evidence.IncrementalBase(r); got != "" {
			t.Errorf("%s: %q, want none", name, got)
		}
	}
}

// Origin: 2026-11-16. The chain answer (what STG is delivered on) was added to
// GET /api/v1/evidence/ci as scope=chain, and the handler's own check of the
// scope still refused it: read on PROD right after the release, it answered
// 400. A scope it does not know is still refused (the control).
func TestEvidenceCIAcceptsTheScopesItAnswers(t *testing.T) {
	s := &Server{}
	ask := func(scope string) int {
		rec := httptest.NewRecorder()
		q := "/api/v1/evidence/ci?repo=bifrost-platform&commit=" + strings.Repeat("a", 40)
		if scope != "" {
			q += "&scope=" + scope
		}
		s.handleEvidenceCI(rec, httptest.NewRequest(http.MethodGet, q, nil))
		return rec.Code
	}
	for _, scope := range []string{"", "full", "any", "chain"} {
		// No ledger on this server: past the check, it is 503.
		if code := ask(scope); code != http.StatusServiceUnavailable {
			t.Errorf("scope %q: %d, want 503 (accepted, no ledger here)", scope, code)
		}
	}
	if code := ask("everything"); code != http.StatusBadRequest {
		t.Errorf("control: an unknown scope answered %d, want 400", code)
	}
}
