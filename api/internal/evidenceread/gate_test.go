package evidenceread

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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

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
		e, is, err := evidence.RunEvidence(runOf(p))
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = evidence.RecordEvidence(ctx, pool, []evidence.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	trees := map[string]string{h("a"): h("c"), h("b"): h("c"), h("d"): h("9"), h("e"): h("9")}
	read := LedgerCI(func() eventlog.DB { return pool }, func(_ context.Context, _, commit string) (string, error) {
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
	ask(h("a"), CIFailed, "ci-red")
	record("ci-full", h("a"), h("c"), "Succeeded", "full", false)
	ask(h("a"), CISucceeded, "ci-full") // ui's main moving on changes nothing here: the commit's own rows answer
	ask(h("b"), CISucceeded, "ci-full") // the same tree under a commit that was never run: the content passed
	ask(h("7"), "", "")                 // a commit whose tree cannot be read has its own answer: none

	record("ci-d", h("d"), h("9"), "Succeeded", "full", false)
	ask(h("d"), CISucceeded, "ci-d")
	record("ci-full-audit", h("d"), h("9"), "Failed", "", true) // the daily full CI contradicts it
	ask(h("d"), CIFailed, "ci-full-audit")
	ask(h("e"), CIFailed, "ci-full-audit") // and so for the same tree under another commit

	// The push to main also counts a pass of part of the repository, unless
	// a newer run failed.
	record("ci-console", h("3"), h("4"), "Succeeded", "console", false)
	anyScope := LedgerCIScoped(func() eventlog.DB { return pool }, nil, true)
	for _, tc := range []struct{ commit, status, run string }{{h("3"), CISucceeded, "ci-console"}, {h("a"), CISucceeded, "ci-full"}, {h("d"), CIFailed, "ci-full-audit"}} {
		if status, run, err := anyScope(ctx, "bifrost-platform", tc.commit); err != nil || status != tc.status || run != tc.run {
			t.Fatalf("any scope, commit %s: %q %q %v", tc.commit[:1], status, run, err)
		}
	}
	ask(h("3"), "", "")
	record("ci-console-red", h("3"), h("4"), "Failed", "", false)
	if status, run, _ := anyScope(ctx, "bifrost-platform", h("3")); status != CIFailed || run != "ci-console-red" {
		t.Fatalf("a pass of part before a failure still counts: %q %q", status, run)
	}
	if _, _, err := LedgerCI(func() eventlog.DB { return nil }, nil)(ctx, "bifrost-platform", h("a")); err == nil {
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
		e, is, err := evidence.RunEvidence(runOf(p))
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
		read := LedgerCIScoped(func() eventlog.DB { return pool }, tree, anyScope)
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
		ask("the whole CI failed and a unit task passed", h("d"), CIFailed, "ci-d-red")
		ask("the whole CI passed and sixty unit rows followed", h("e"), CISucceeded, "ci-e")
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
		e, is, err := evidence.RunEvidence(runOf(p))
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
	read := LedgerCI(func() eventlog.DB { return pool }, nil)
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
	ask("B1, before the failure is seen again", h("1"), CISucceeded)
	write(f)
	ask("B1, the old failure seen again", h("1"), CISucceeded)

	// B2: a full pass, then fifty ordinary failures of the same commit. An
	// ordinary failure does not invalidate a full pass, however many follow.
	write(run("b2-pass", h("2"), h("b"), "Succeeded", false, t1))
	for i := 0; i < 50; i++ {
		write(run(fmt.Sprintf("b2-red-%d", i), h("2"), h("b"), "Failed", false, t2))
	}
	ask("B2, a full pass under fifty failures", h("2"), CISucceeded)

	// A2: a full pass at t1 and the daily full CI's failure at t3, both seen
	// for the first time in one batch, the failure listed first. The later
	// failure invalidates the pass whatever the order of the batch.
	write(run("a2-audit", h("3"), h("c"), "Failed", true, t3), run("a2-pass", h("3"), h("c"), "Succeeded", false, t1))
	ask("A2, the failure listed before the pass it follows", h("3"), CIFailed)
	write(run("a2b-pass", h("4"), h("d"), "Succeeded", false, t1), run("a2b-audit", h("4"), h("d"), "Failed", true, t3))
	ask("A2, the same in order", h("4"), CIFailed)
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
		e, is, err := evidence.RunEvidence(runOf(p))
		if err != nil || !is {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err = evidence.RecordEvidence(ctx, pool, []evidence.EvidenceRow{e}); err != nil {
			t.Fatal(err)
		}
	}
	read := LedgerCI(func() eventlog.DB { return pool }, func(_ context.Context, _, c string) (string, error) {
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
	ask("the base", a, CISucceeded, "full-a")
	ask("one step", b, CISucceeded, "incr-b")
	ask("two steps", c, CISucceeded, "incr-c")

	// What PROD is judged on does not follow the chain: b and c have no
	// verdict there until a full run of theirs passes, and then they have.
	strict := LedgerCIStrict(func() eventlog.DB { return pool }, nil)
	for what, sha := range map[string]string{"one step, strict": b, "two steps, strict": c} {
		if st, run, err := strict(ctx, "bifrost-platform", sha); err != nil || st != "" || run != "" {
			t.Fatalf("%s: %q %q %v, want no verdict", what, st, run, err)
		}
	}
	if st, _, _ := strict(ctx, "bifrost-platform", a); st != CISucceeded {
		t.Fatalf("the full pass, strict: %q", st)
	}
	record("full-b", b, "Succeeded", "full", "")
	if st, run, _ := strict(ctx, "bifrost-platform", b); st != CISucceeded || run != "full-b" {
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
	ask("the failed base itself", red, CIFailed, "full-red")
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
	ask("a failure recorded after the incremental pass", later, CIFailed, "full-later-red")
	// The chain is bounded: maxVerdictChain steps reach the full pass, one more does not.
	prev, name := a, ""
	for i := 1; i <= maxVerdictChain+1; i++ {
		next := commit()
		name = fmt.Sprintf("incr-chain-%d", i)
		record(name, next, "Succeeded", "incremental", prev)
		if i == maxVerdictChain {
			ask("a chain of the bound", next, CISucceeded, name)
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
	route := CIRoute{RepoOK: gitread.Name.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (CISources, bool) { return CISources{}, false }}
	ask := func(scope string) int {
		rec := httptest.NewRecorder()
		q := "/api/v1/evidence/ci?repo=bifrost-platform&commit=" + strings.Repeat("a", 40)
		if scope != "" {
			q += "&scope=" + scope
		}
		route.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, q, nil))
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

// The row of ci-platform-pgdsv for bifrost-platform c53ea00d as the PROD
// ledger holds it (read 2026-11-13). inputs.beside holds the siblings' TREES
// (c6b1e24e…, 27bcc703…); the commits a delivery takes as uiRevision and
// infraRevision are in the statement's subjects. Taking beside from inputs
// would hand the pipeline a tree id as a revision.
const pgdsvStatement = `{"_type": "https://in-toto.io/Statement/v1", "subject": [
 {"name": "bifrost-platform", "digest": {"gitTree": "062e22be961b9c20d31eec3d3783a6f0ddf51ea8", "gitCommit": "c53ea00db051530cd38b44ad20a07ee72d5da231"}},
 {"name": "bifrost-trade-infra", "digest": {"gitTree": "27bcc703c429c6c1cefe2049246bac464abf96a6", "gitCommit": "1d5a18421e64982ebe69618371531ab5d2233d47"}},
 {"name": "bifrost-ui", "digest": {"gitTree": "c6b1e24ea050585db898fc8780731ce50a32f10f", "gitCommit": "c83f40b40e65e52f8592ec6b73c5f6987c116e93"}}]}`

func TestBesideIsTheCommitsOfTheRowTheVerdictRestsOn(t *testing.T) {
	rows := []evidence.EvidenceRow{
		// The other passing row of the same commit ran beside another infra
		// commit (ci-platform-5d6bb, cf70db50): it is not the verdict's row.
		{RunName: "ci-platform-5d6bb", Statement: json.RawMessage(`{"subject": [{"name": "bifrost-trade-infra", "digest": {"gitCommit": "cf70db500403b0b03a34842062ef5813b1153cff"}}]}`)},
		{RunName: "ci-platform-pgdsv", Statement: json.RawMessage(pgdsvStatement)},
	}
	got, found := BesideOf(rows, "bifrost-platform", "ci-platform-pgdsv")
	if !found || len(got) != 2 || got["bifrost-ui"] != "c83f40b40e65e52f8592ec6b73c5f6987c116e93" || got["bifrost-trade-infra"] != "1d5a18421e64982ebe69618371531ab5d2233d47" {
		t.Fatalf("beside %v found %v", got, found)
	}
	if got, found := BesideOf(rows, "bifrost-platform", "ci-platform-zzzzz"); found || got != nil {
		t.Fatalf("a run with no row: %v %v", got, found)
	}
}

// runOf is the record of a pipeline run as the evidence ledger reads it (the
// conversion the runtime makes: controller.EvidenceRun).
func runOf(p seenfacts.PipelineRun) evidence.Run {
	var inputs []evidence.RepoAt
	for _, in := range p.Inputs {
		inputs = append(inputs, evidence.RepoAt(in))
	}
	return evidence.Run{Kind: p.Kind, Repo: p.Repo, Run: p.Run, UID: p.UID, Pipeline: p.Pipeline,
		Result: p.Result, Reason: p.Reason, Scope: p.Scope, Base: p.Base, TestedBy: p.TestedBy, CompletedAt: p.CompletedAt,
		Inputs: inputs, Audit: p.Audit, FailedTasks: p.FailedTasks, FailedTests: p.FailedTests, FailClass: p.FailClass}
}
