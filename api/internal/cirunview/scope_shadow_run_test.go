package cirunview

import (
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- stands in for git's object ids in a test tree
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// scopeTrees serves commits by name from files by path, as unitplan reads them.
func scopeTrees(t *testing.T, commits map[string]map[string]string) func(context.Context, string, string) (*unitplan.Tree, error) {
	t.Helper()
	blobID := func(content string) string {
		return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content)))) // #nosec G401
	}
	return func(_ context.Context, repo, commit string) (*unitplan.Tree, error) {
		files, ok := commits[commit]
		if !ok || repo != "r" {
			return nil, fmt.Errorf("gitea: no %s@%s", repo, commit)
		}
		blobs, dirs := map[string]string{}, map[string][]string{}
		var entries []unitplan.Entry
		for p, body := range files {
			blobs[blobID(body)] = body
			entries = append(entries, unitplan.Entry{Path: p, ID: blobID(body)})
			for d := p; strings.Contains(d, "/"); {
				d = d[:strings.LastIndex(d, "/")]
				dirs[d] = append(dirs[d], p+"="+body)
			}
			dirs[""] = append(dirs[""], p+"="+body)
		}
		dirID := func(d string) string { sort.Strings(dirs[d]); return blobID("tree:" + strings.Join(dirs[d], "\n")) }
		for d := range dirs {
			if d != "" {
				entries = append(entries, unitplan.Entry{Path: d, Dir: true, ID: dirID(d)})
			}
		}
		return unitplan.NewTree(dirID(""), entries, func(id string) ([]byte, error) { return []byte(blobs[id]), nil })
	}
}

// shadowFixture is a shadow over two commits of a repository "r" whose
// declaration names a module "svc" of three packages (a imports b) and a
// module "web": the head changes package c alone. plans counts the scopes
// computed; lines are the log lines written.
type shadowFixture struct {
	*ScopeShadow
	plans int
	known map[string]bool
	out   bytes.Buffer
}

func newShadowFixture(t *testing.T) *shadowFixture {
	t.Helper()
	const manifest = `version: 1
modules:
  - name: svc
    kind: go
    root: svc
    tasks: {test: {per: package}}
  - name: web
    kind: node
    root: web
    tasks: {test: {}}
`
	b, c := strings.Repeat("b", 40), strings.Repeat("c", 40)
	base := map[string]string{"bifrost.units.yaml": manifest, "svc/go.mod": "module example.com/m\n\ngo 1.25\n",
		"svc/a/a.go": "package a\n\nimport _ \"example.com/m/b\"\n", "svc/a/a_test.go": "package a\n",
		"svc/b/b.go": "package b\n", "svc/b/b_test.go": "package b\n",
		"svc/c/c.go": "package c\n", "svc/c/c_test.go": "package c\n", "web/index.ts": "1"}
	head := map[string]string{}
	for k, v := range base {
		head[k] = v
	}
	head["svc/c/c.go"] = "package c\n\nconst C = 1\n"
	trees := scopeTrees(t, map[string]map[string]string{b: base, c: head})
	f := &shadowFixture{known: map[string]bool{}}
	f.ScopeShadow = &ScopeShadow{Repo: "r", Pipeline: "ci-r",
		Scope: func(ctx context.Context, repo, commit string) (unitplan.Scope, error) {
			f.plans++
			return unitplan.ScopeOf(ctx, trees, func(context.Context, string, string) (string, string, error) { return "Succeeded", "ci-0", nil }, repo, commit, b)
		},
		Known: func(_ context.Context, _, _, uid string) (bool, error) { return f.known[uid], nil },
		Start: func(fn func()) { fn() },
		Log:   slog.New(slog.NewJSONHandler(&f.out, nil))}
	return f
}

func (f *shadowFixture) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(f.out.String()), "\n") {
		if l == "" {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(l), &doc); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		out = append(out, doc)
	}
	return out
}

func shadowRun(name, result string, tests ...string) seenfacts.PipelineRun {
	p := seenfacts.PipelineRun{Run: name, UID: "uid-" + name, Kind: "ci", Pipeline: "ci-r", Repo: "r", Result: result, Reason: result, FailedTests: tests,
		Inputs: []seenfacts.RepoAt{{Repo: "other", Commit: strings.Repeat("9", 40)}, {Repo: "r", Commit: strings.Repeat("c", 40)}}}
	return p
}

// One line a finished run: what a run by unit would have run, and the units a
// failed test belongs to that it would have left out.
// Origin: main coordinator 2026-11-14: backend skip by package goes shadow
// first; a failing package the scope would skip is the miss this looks for.
func TestUnitScopeShadow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		run           seenfacts.PipelineRun
		missed        string
		unnamed       bool
		unmatched     float64
		verdict, base string
	}{
		{"a passed run", shadowRun("ci-1", "Succeeded"), "", false, 0, "Succeeded", strings.Repeat("b", 40)},
		// The changed package fails: the scope would have run it.
		{"a failed run whose failing package runs", shadowRun("ci-2", "Failed", "example.com/m/c.TestC/sub/a.b"), "", false, 0, "Failed", strings.Repeat("b", 40)},
		// The positive control: package a's keys are the base's and its test failed.
		{"a failed run whose failing package is left out", shadowRun("ci-3", "Failed", "example.com/m/a.TestA", "example.com/m/a.TestB", "example.com/m/c.TestC"), "svc/a", false, 0, "Failed", strings.Repeat("b", 40)},
		{"a failed run that names no test", shadowRun("ci-4", "Failed"), "", true, 0, "Failed", strings.Repeat("b", 40)},
		{"a failed test of no unit", shadowRun("ci-5", "Failed", "example.com/elsewhere/z.TestZ", "not a test name"), "", false, 2, "Failed", strings.Repeat("b", 40)},
	} {
		f := newShadowFixture(t)
		f.Observe(context.Background(), tc.run)
		lines := f.lines(t)
		if len(lines) != 1 || f.plans != 1 {
			t.Errorf("%s: %d lines from %d plans: %s", tc.name, len(lines), f.plans, f.out.String())
			continue
		}
		l := lines[0]
		var missed []string
		for _, m := range l["missed"].([]any) {
			missed = append(missed, m.(string))
		}
		if l["msg"] != "unit scope shadow" || l["level"] != "INFO" || l["run"] != tc.run.Run || l["revision"] != strings.Repeat("c", 40) || l["base"] != tc.base ||
			l["verdict"] != tc.verdict || l["would_run"] != float64(1) || l["total"] != float64(4) || l["by_module"] != "svc=1/3 web=0/1" || l["reason"] != "" ||
			strings.Join(missed, " ") != tc.missed || l["failed_unnamed"] != tc.unnamed || l["unmatched"] != tc.unmatched {
			t.Errorf("%s: %s", tc.name, f.out.String())
		}
	}
}

// A run is shadowed once: told twice by one process it is computed once, and
// a run the evidence ledger already has (a process before this one wrote it)
// is not computed at all. A run of another repository or pipeline is not the
// shadow's. Origin: main coordinator 2026-11-14: backend skip by package goes
// shadow first; a failing package the scope would skip is the miss this looks
// for. At most one plan pair a run: on 2026-11-14 a new loop listed every
// PipelineRun each pass and the STG workers were OOMKilled at 512Mi.
func TestUnitScopeShadowOncePerRun(t *testing.T) {
	t.Parallel()
	f := newShadowFixture(t)
	f.Observe(context.Background(), shadowRun("ci-1", "Succeeded"))
	f.Observe(context.Background(), shadowRun("ci-1", "Succeeded"))
	f.known["uid-ci-old"] = true
	f.Observe(context.Background(), shadowRun("ci-old", "Succeeded"))
	other := shadowRun("ci-other", "Succeeded")
	other.Pipeline = "deliver-r"
	f.Observe(context.Background(), other)
	elsewhere := shadowRun("ci-elsewhere", "Succeeded")
	elsewhere.Repo = "s"
	f.Observe(context.Background(), elsewhere)
	if lines := f.lines(t); f.plans != 1 || len(lines) != 1 || lines[0]["run"] != "ci-1" {
		t.Fatalf("%d plans: %s", f.plans, f.out.String())
	}
}

// Nothing the shadow meets fails its caller: a scope that cannot be computed
// and a ledger that cannot answer are one WARN line each, and when everything
// runs the line says why. Origin: main coordinator 2026-11-14: backend skip by
// package goes shadow first; a failing package the scope would skip is the
// miss this looks for.
func TestUnitScopeShadowSwallowsErrors(t *testing.T) {
	t.Parallel()
	f := newShadowFixture(t)
	f.Scope = func(context.Context, string, string) (unitplan.Scope, error) {
		f.plans++
		return unitplan.Scope{}, errors.New("gitea: HTTP 502")
	}
	f.Observe(context.Background(), shadowRun("ci-1", "Succeeded"))
	f.known = nil
	f.ScopeShadow.Known = func(context.Context, string, string, string) (bool, error) { return false, errors.New("no ledger") }
	f.Observe(context.Background(), shadowRun("ci-2", "Succeeded"))
	lines := f.lines(t)
	if f.plans != 1 || len(lines) != 2 || lines[0]["level"] != "WARN" || lines[0]["error"] != "gitea: HTTP 502" || lines[1]["level"] != "WARN" || lines[1]["run"] != "ci-2" {
		t.Fatalf("%d plans: %s", f.plans, f.out.String())
	}
	whole := newShadowFixture(t)
	whole.Scope = func(context.Context, string, string) (unitplan.Scope, error) {
		return unitplan.Scope{Repo: "r", Commit: strings.Repeat("c", 40), Declared: true, Note: "no base to compare with",
			Modules: []unitplan.ModuleScope{{Module: "svc", Tasks: 1, Changed: 1, Run: true, Why: []string{"no base to compare with"}, RunUnits: []string{"svc/a"}, SameUnits: []string{}}}}, nil
	}
	// With nothing to compare, the failing package runs: no miss.
	whole.Observe(context.Background(), shadowRun("ci-3", "Failed", "example.com/m/a.TestA"))
	if l := whole.lines(t); len(l) != 1 || l[0]["reason"] != "no base to compare with" || l[0]["would_run"] != float64(1) || l[0]["total"] != float64(1) || len(l[0]["missed"].([]any)) != 0 {
		t.Fatalf("whole: %s", whole.out.String())
	}
}

// A failed test is of the unit its package path ends with, by the most path
// elements; a subtest's name is not read as a package.
func TestShadowTestOfUnit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		test, pkg string
	}{
		{"example.com/m/internal/a.TestX", "example.com/m/internal/a"},
		{"example.com/m/a.TestX/sub/b.TestY", "example.com/m/a"},
		{"example.com/m/a.ExampleX", "example.com/m/a"},
		{"build failed", ""},
	} {
		if got := TestPackage(tc.test); got != tc.pkg {
			t.Errorf("TestPackage(%q) = %q, want %q", tc.test, got, tc.pkg)
		}
	}
	for _, tc := range []struct {
		pkg, unit string
		want      int
	}{
		{"example.com/m/root/internal/a", "root/internal/a", 3},
		{"example.com/m/internal/a", "root/internal/a", 2},
		{"example.com/m/internal/data", "root/internal/a", 0},
		{"a", "root/a", 1},
		{"", "root/a", 0},
	} {
		if got := UnitMatch(tc.pkg, tc.unit); got != tc.want {
			t.Errorf("UnitMatch(%q, %q) = %d, want %d", tc.pkg, tc.unit, got, tc.want)
		}
	}
}

// Origin: 2026-11-15, the shadow line lived only in the workers' log, and the
// workers are replaced at every release of the environment: three lines were
// read, then none. The line is also kept as an audit record, with the status
// that says whether a failing package was among the skipped ones.
func TestShadowRecordKeepsTheLineAndNamesAMiss(t *testing.T) {
	status, detail := ShadowRecord([]any{"run", "ci-1", "would_run", 3, "total", 9, "missed", []string{}, "failed_unnamed", false})
	if status != "ok" || !strings.Contains(detail, `"run":"ci-1"`) || !strings.Contains(detail, `"would_run":3`) {
		t.Fatalf("status=%q detail=%s", status, detail)
	}
	// The positive control: a failing package the scope would have skipped.
	status, detail = ShadowRecord([]any{"run", "ci-2", "missed", []string{"svc/d"}})
	if status != "missed" || !strings.Contains(detail, `"missed":["svc/d"]`) {
		t.Fatalf("status=%q detail=%s", status, detail)
	}
}

// Origin: Owner 2026-11-16, reopening the incremental landing: the run that
// lets a commit in is incremental, and the full run of the push to main is
// the check on it. Every such pair is one reconciliation; a full run that
// fails where the incremental one passed is a miss, said so by name. A full
// run of a commit that had no incremental run is not a reconciliation (the
// control), nor is an incremental run itself.
func TestAFullRunIsReconciledWithTheIncrementalRunBeforeIt(t *testing.T) {
	t.Parallel()
	type rec struct{ run, status, detail string }
	var told []string
	observe := func(p seenfacts.PipelineRun, incremental string) []rec {
		f := newShadowFixture(t)
		var got []rec
		f.Drift = func(_ context.Context, repo, commit, passed, contradicts string) error {
			told = append(told, repo+" "+commit[:1]+" "+passed+" "+contradicts)
			return nil
		}
		f.Incremental = func(context.Context, string, string) (string, error) { return incremental, nil }
		f.Reconcile = func(run, status, detail string) { got = append(got, rec{run, status, detail}) }
		f.Observe(context.Background(), p)
		return got
	}
	full := func(name, result string, tests ...string) seenfacts.PipelineRun {
		p := shadowRun(name, result, tests...)
		p.Scope = "full"
		p.StartedAt = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
		p.CompletedAt = p.StartedAt.Add(150 * time.Second)
		return p
	}
	got := observe(full("ci-main-1", "Succeeded"), "ci-ready-1")
	if len(got) != 1 || got[0].run != "ci-main-1" || got[0].status != "ok" {
		t.Fatalf("a full pass after an incremental pass: %+v", got)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(got[0].detail), &d); err != nil || d["incremental_run"] != "ci-ready-1" || d["full_seconds"] != float64(150) ||
		d["by_module"] != "svc=1/3 web=0/1" || d["revision"] != strings.Repeat("c", 40) || d["repo"] != "r" {
		t.Fatalf("detail %s (%v)", got[0].detail, err)
	}
	got = observe(full("ci-main-2", "Failed", "example.com/m/a.TestA"), "ci-ready-2")
	if len(got) != 1 || got[0].status != "missed" || !strings.Contains(got[0].detail, "example.com/m/a.TestA") {
		t.Fatalf("a full failure after an incremental pass: %+v", got)
	}
	// The bus is told of the miss and of nothing else (the pass above, the two controls below).
	if strings.Join(told, "|") != "r c ci-ready-2 ci-main-2" {
		t.Fatalf("told the bus: %v", told)
	}
	if got = observe(full("ci-main-3", "Failed", "example.com/m/a.TestA"), ""); len(got) != 0 {
		t.Fatalf("control, no incremental run before it: %+v", got)
	}
	inc := shadowRun("ci-ready-4", "Succeeded")
	inc.Scope = "incremental"
	if got = observe(inc, "ci-ready-4"); len(got) != 0 {
		t.Fatalf("an incremental run is not reconciled with itself: %+v", got)
	}
	if strings.Join(told, "|") != "r c ci-ready-2 ci-main-2" {
		t.Fatalf("told the bus after the controls: %v", told)
	}
}
