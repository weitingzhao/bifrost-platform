package gaterun

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A gate passes only when every named test ran and passed: a test that did
// not run, skipped (or whose subtest skipped), failed or did not end, a
// package that failed or never said, all fail it (TD-397, review td397-1 A8).
func TestCheck(t *testing.T) {
	t.Parallel()
	ev := func(action, test string) string {
		if test == "" {
			return `{"Action":"` + action + `","Package":"p"}`
		}
		return `{"Action":"` + action + `","Package":"p","Test":"` + test + `"}`
	}
	for name, c := range map[string]struct {
		lines []string
		tests []string
		pass  bool
		why   string
	}{
		"both pass":   {[]string{ev("run", "TestA"), ev("pass", "TestA"), ev("run", "TestB"), ev("pass", "TestB"), ev("pass", "")}, []string{"TestA", "TestB"}, true, ""},
		"one missing": {[]string{ev("run", "TestA"), ev("pass", "TestA"), ev("pass", "")}, []string{"TestA", "TestB"}, false, "TestB: it did not run"},
		"a skip":      {[]string{ev("run", "TestA"), ev("skip", "TestA"), ev("pass", "")}, []string{"TestA"}, false, "TestA: it skipped"},
		"a subtest skip": {[]string{ev("run", "TestA"), ev("run", "TestA/x"), ev("skip", "TestA/x"), ev("pass", "TestA"), ev("pass", "")}, []string{"TestA"}, false,
			"TestA: its subtest TestA/x skipped"},
		"a fail":          {[]string{ev("run", "TestA"), ev("fail", "TestA"), ev("fail", "")}, []string{"TestA"}, false, "TestA: it failed"},
		"no end":          {[]string{ev("run", "TestA"), ev("pass", "")}, []string{"TestA"}, false, "TestA: it did not end"},
		"package fails":   {[]string{ev("run", "TestA"), ev("pass", "TestA"), ev("fail", "")}, []string{"TestA"}, false, ""},
		"package silent":  {[]string{ev("run", "TestA"), ev("pass", "TestA")}, []string{"TestA"}, false, ""},
		"no test files":   {[]string{ev("skip", "")}, []string{"TestA"}, false, "TestA: it did not run"},
		"not events":      {[]string{"ok  p 0.1s", "PASS"}, []string{"TestA"}, false, "TestA: it did not run"},
		"another passing": {[]string{ev("run", "TestZ"), ev("pass", "TestZ"), ev("pass", "")}, []string{"TestA"}, false, "TestA: it did not run"},
		"a cut line": {[]string{ev("run", "TestA"), ev("pass", "TestA"), ev("pass", ""), `{"Action":`}, []string{"TestA"}, false,
			"1 lines of go test's output are not test events"},
		"a subtest fails": {[]string{ev("run", "TestA"), ev("run", "TestA/x"), ev("fail", "TestA/x"), ev("pass", "TestA"), ev("pass", "")}, []string{"TestA"}, false,
			"TestA: its subtest TestA/x failed"},
	} {
		rep, err := Check(strings.NewReader(strings.Join(c.lines, "\n")), c.tests)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Pass() != c.pass {
			t.Errorf("%s: pass %v; want %v (%+v)", name, rep.Pass(), c.pass, rep)
		}
		summary := strings.Join(append(rep.Summary(5), rep.Verdict("p", nil)), "\n")
		if c.why != "" && !strings.Contains(summary, c.why) {
			t.Errorf("%s: the summary does not say %q:\n%s", name, c.why, summary)
		}
	}
}

// The gate's last line names its first failure, or why the run failed, and
// is never longer than VerdictMax bytes, so a verify.json's tail keeps it
// (review td397-3 B-c).
func TestVerdict(t *testing.T) {
	t.Parallel()
	long := "Test" + strings.Repeat("X", 400)
	rep := Report{Package: "fail", Outcomes: []Outcome{{Test: "TestA", Result: "pass"}, {Test: long, Result: "fail", Why: "it failed"}}}
	if v := rep.Verdict("./internal/x/", nil); !strings.HasPrefix(v, VerdictPrefix+"FAIL ./internal/x/: TestXXX") || len(v) != VerdictMax {
		t.Fatalf("%q (%d bytes)", v, len(v))
	}
	ok := Report{Package: "pass", Outcomes: []Outcome{{Test: "TestA", Result: "pass"}}}
	if v := ok.Verdict("p", nil); v != VerdictPrefix+"PASS p: 1 tests ran and passed" {
		t.Fatalf("%q", v)
	}
	if v := ok.Verdict("p", errors.New("exit status 1")); v != VerdictPrefix+"FAIL p: go test: exit status 1" {
		t.Fatalf("%q", v)
	}
}

// A gate's spec is the runner on named tests of one package and nothing
// else; Spec writes it and ParseSpec reads it back, and refuses any other
// form (review td397-1 A8: a command that runs no test, a shell, an env
// override, setup, siblings, check fields).
func TestSpec(t *testing.T) {
	t.Parallel()
	g := Gate{ID: "T40", Text: "a gate", Commands: []Command{{Pkg: "./internal/agentrt/plan/", Tests: []string{"TestA", "TestB"}},
		{Pkg: "./internal/agentrt/rules/", Tests: []string{"TestC"}}}}
	body, sum, err := g.Spec()
	if err != nil || len(sum) != 64 {
		t.Fatalf("%s %s %v", body, sum, err)
	}
	cmds, err := ParseSpec(body)
	if err != nil || len(cmds) != 2 || !slices.Equal(cmds[0].Tests, []string{"TestA", "TestB"}) || cmds[1].Pkg != "./internal/agentrt/rules/" {
		t.Fatalf("read back %+v %v", cmds, err)
	}
	runner := `"go","-C","api","run","./cmd/agentrt-gate"`
	for name, bad := range map[string]string{
		"true":          `{"text":"x","timeout_s":600,"verify":[["env","AGENTRT_TEST_REQUIRED=1","true"]]}`,
		"go build":      `{"text":"x","timeout_s":600,"verify":[["go","-C","api","build","./..."]]}`,
		"shell":         `{"text":"x","timeout_s":600,"verify":[["sh","-c","go run ./cmd/agentrt-gate -pkg ./internal/x/ -test TestA"]]}`,
		"env override":  `{"text":"x","timeout_s":600,"verify":[["env","AGENTRT_TEST_REQUIRED=0",` + runner[1:] + `,"-pkg","./internal/x/","-test","TestA"]]}`,
		"extra flag":    `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA","-exec","x"]]}`,
		"two packages":  `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/x/","-pkg","./internal/y/","-test","TestA"]]}`,
		"no test":       `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/x/"]]}`,
		"a pattern":     `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/x/","-test","Test.*"]]}`,
		"all packages":  `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./...","-test","TestA"]]}`,
		"up and out":    `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/../x/","-test","TestA"]]}`,
		"setup":         `{"text":"x","timeout_s":600,"setup":[["true"]],"verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA"]]}`,
		"siblings":      `{"text":"x","timeout_s":600,"siblings":[],"verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA"]]}`,
		"check":         `{"text":"x","timeout_s":600,"check":"verify","verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA"]]}`,
		"no timeout":    `{"text":"x","verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA"]]}`,
		"no commands":   `{"text":"x","timeout_s":600,"verify":[]}`,
		"two values":    `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA"]]} {}`,
		"a test twice":  `{"text":"x","timeout_s":600,"verify":[[` + runner + `,"-pkg","./internal/x/","-test","TestA","-test","TestA"]]}`,
		"markdown spec": "# just text\n",
	} {
		if _, err := ParseSpec([]byte(bad)); err == nil {
			t.Errorf("%s: the spec was taken", name)
		}
	}
}

// The catalog: test ids unique and the rule grammar's, rows unique, every
// spec reads back as written, and every named test is a test function of
// its package's source (a test renamed or removed fails here, not on a host
// at night).
func TestCatalog(t *testing.T) {
	t.Parallel()
	ids, labels := map[string]bool{}, map[string]bool{}
	for _, r := range Catalog {
		if labels[r.Label] || len(r.Gates) == 0 {
			t.Errorf("row %s twice or with no gate", r.Label)
		}
		labels[r.Label] = true
		if _, err := r.Rule("prod"); err != nil {
			t.Errorf("row %s: %v", r.Label, err)
		}
		for _, g := range r.Gates {
			if ids[g.ID] {
				t.Errorf("gate %s twice", g.ID)
			}
			ids[g.ID] = true
			body, _, err := g.Spec()
			if err != nil {
				t.Fatalf("gate %s: %v", g.ID, err)
			}
			cmds, err := ParseSpec(body)
			if err != nil || len(cmds) != len(g.Commands) {
				t.Fatalf("gate %s reads back %v %v", g.ID, cmds, err)
			}
			for _, c := range g.Commands {
				have := testFuncs(t, filepath.Join("..", "..", "..", strings.TrimPrefix(c.Pkg, "./")))
				for _, name := range c.Tests {
					if !have[name] {
						t.Errorf("gate %s: %s has no test %s", g.ID, c.Pkg, name)
					}
				}
			}
		}
	}
}

// testFuncs is the top-level Test functions of a package's test files.
func testFuncs(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test files in %s: %v", dir, err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// The runner on a module of its own: a passing test passes the gate; a
// skipping one, a failing one, one that is not there, and one that needs the
// test database (as agentrttest does, under AGENTRT_TEST_REQUIRED=1, which
// the runner sets whatever the environment says) fail it.
//
// Serial: it sets the environment the runner reads.
func TestRunOnAModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("go is not on PATH")
	}
	dir := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gatefixture\n\ngo 1.22\n")
	write("internal/x/x_test.go", `package x

import (
	"os"
	"testing"
)

func TestPasses(t *testing.T) {}
func TestSkips(t *testing.T)  { t.Skip("not here") }
func TestFails(t *testing.T)  { t.Fatal("no") }
func TestNeedsTheDatabase(t *testing.T) {
	if os.Getenv("AGENTRT_TEST_PG") == "" {
		if os.Getenv("AGENTRT_TEST_REQUIRED") == "1" {
			t.Fatal("AGENTRT_TEST_PG is not set but AGENTRT_TEST_REQUIRED=1")
		}
		t.Skip("no test database")
	}
}
`)
	// A TestMain that exits 0 before running anything, and one that exits 1
	// after every test passed.
	write("internal/early/early_test.go", "package early\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n"+
		"func TestMain(m *testing.M) { os.Exit(0) }\nfunc TestPasses(t *testing.T) {}\n")
	write("internal/late/late_test.go", "package late\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n"+
		"func TestMain(m *testing.M) { m.Run(); os.Exit(1) }\nfunc TestPasses(t *testing.T) {}\n")
	for _, pkg := range []string{"./internal/early/", "./internal/late/"} {
		var out strings.Builder
		if code := Run(context.Background(), dir, Command{Pkg: pkg, Tests: []string{"TestPasses"}}, &out); code != 1 {
			t.Errorf("%s: exit %d; want 1\n%s", pkg, code, out.String())
		}
	}
	t.Setenv("AGENTRT_TEST_REQUIRED", "0")
	t.Setenv("AGENTRT_TEST_PG", "")
	t.Setenv("GOFLAGS", "-mod=mod")
	for tests, want := range map[string]int{"TestPasses": 0, "TestSkips": 1, "TestFails": 1, "TestNotThere": 1, "TestNeedsTheDatabase": 1,
		"TestPasses,TestSkips": 1} {
		var out strings.Builder
		if code := Run(context.Background(), dir, Command{Pkg: "./internal/x/", Tests: strings.Split(tests, ",")}, &out); code != want {
			t.Errorf("%s: exit %d; want %d\n%s", tests, code, want, out.String())
		}
	}
}
