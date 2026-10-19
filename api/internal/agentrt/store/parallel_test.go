package store_test

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

// minParallel is how many tests of the package run at once when the command
// does not say (-parallel): at least this many, whatever GOMAXPROCS is. CI
// gives the test step 2 CPUs, so -parallel defaults to 2 there; the tests
// mostly wait on PostgreSQL (a database each, locks, sleeps), not on the CPU,
// and 4 at a time took the package from 58 s to 41 s at GOMAXPROCS=2 here
// (TD-374).
const minParallel = 4

func TestMain(m *testing.M) {
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "test.parallel" })
	if !explicit && runtime.GOMAXPROCS(0) < minParallel {
		if err := flag.Set("test.parallel", fmt.Sprint(minParallel)); err != nil {
			fmt.Fprintln(os.Stderr, "store tests: -parallel:", err)
			os.Exit(2)
		}
	}
	os.Exit(m.Run())
}

// TD-374's ratchet: the package's tests run in parallel, so the package stays
// well under CI's per-binary timeout as it grows (each test has a database of
// its own). A test calls t.Parallel() first, or says on a line of its comment
// starting "Serial:" why it may not. A test that reaches a package-level
// switch (serialOnly: a hook, the event clock, a lowered limit, the process's
// environment), from its own body or through a helper of this package, must
// be serial: a parallel test would run under another test's switch, and
// t.Setenv panics in one.
func TestTestsRunInParallel(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("the test files: %v %v", files, err)
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, file)
	}
	tests, problems := parallelProblems(parsed)
	if tests < 100 {
		t.Fatalf("read %d tests; the package has more (is the scan reading the files?)", tests)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// TestMain's floor holds: without -parallel on the command line, at least
// minParallel tests run at once.
func TestParallelFloor(t *testing.T) {
	t.Parallel()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "test.parallel" })
	got := flag.Lookup("test.parallel").Value.String()
	if !explicit && got != fmt.Sprint(max(runtime.GOMAXPROCS(0), minParallel)) {
		t.Fatalf("-parallel is %s at GOMAXPROCS %d; want at least %d", got, runtime.GOMAXPROCS(0), minParallel)
	}
}

// The scan itself, on a package made up for it (a positive control: a scan
// that finds nothing proves nothing until it is shown to find something).
func TestParallelScanFindsWhatItShould(t *testing.T) {
	t.Parallel()
	const src = `package store_test

func helper() { store.SetHook(nil) }
func (w *world) frozen() { defer store.FreezeEventClock(time.Now())() }
func indirect() { helper() }

func TestGood(t *testing.T) { t.Parallel() }

// Serial: SetHook.
func TestSerialOK(t *testing.T) { indirect() }

func TestForgot(t *testing.T) { ok(t, nil) }

func TestParallelButHooked(t *testing.T) {
	t.Parallel()
	indirect()
}

func TestParallelButFrozen(t *testing.T) {
	t.Parallel()
	w.frozen()
}

func TestParallelButEnv(t *testing.T) {
	t.Parallel()
	t.Setenv("X", "1")
}

func TestParallelLate(t *testing.T) {
	ok(t, nil)
	t.Parallel()
}

// Serial: a hook.
func TestBoth(t *testing.T) { t.Parallel() }

// Serial:
func TestEmptyWhy(t *testing.T) { ok(t, nil) }
`
	const inside = `package store

func TestInsideClock(t *testing.T) {
	t.Parallel()
	eventClock = func() time.Time { return time.Time{} }
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "made_up_test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	in, err := parser.ParseFile(fset, "made_up_internal_test.go", inside, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	tests, problems := parallelProblems([]*ast.File{file, in})
	got := map[string]bool{}
	for _, p := range problems {
		got[strings.Fields(p)[0]] = true
	}
	want := []string{"TestForgot", "TestParallelButHooked", "TestParallelButFrozen", "TestParallelButEnv", "TestParallelLate", "TestBoth",
		"TestEmptyWhy", "TestInsideClock"}
	if tests != 10 || len(got) != len(want) {
		t.Fatalf("%d tests, problems %v; want 10 tests and %v", tests, problems, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("the scan missed %s: %v", name, problems)
		}
	}
}

// serialOnly are the switches a parallel test must not reach: the package's
// test hook, its event clock and the limits a test lowers (export_test.go,
// or the variables themselves from a test inside the package), and the
// process's environment and working directory.
var serialOnly = []string{"SetHook", "FreezeEventClock", "LimitPendingChangesForTest", "DecidePageForTest",
	"testHook", "eventClock", "pendingChangeLimit", "decidePageSize", "Setenv", "Unsetenv", "Chdir"}

// parallelProblems reads the test files of the package (store_test, and
// store's own): how many Test functions there are, and what each one breaks
// of the rule.
func parallelProblems(files []*ast.File) (int, []string) {
	// What each function of the test files calls or names, by name (a
	// method by its own name: a helper of that name anywhere counts, which
	// errs on the side of serial).
	calls := map[string][]string{}
	var tests []*ast.FuncDecl
	for _, f := range files {
		if f.Name.Name != "store_test" && f.Name.Name != "store" {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			calls[fn.Name.Name] = append(calls[fn.Name.Name], calledNames(fn.Body)...)
			if strings.HasPrefix(fn.Name.Name, "Test") && fn.Name.Name != "TestMain" && fn.Recv == nil {
				tests = append(tests, fn)
			}
		}
	}
	reached := map[string][]string{} // function name -> the switches it reaches
	var reach func(name string, seen map[string]bool) []string
	reach = func(name string, seen map[string]bool) []string {
		if r, ok := reached[name]; ok {
			return r
		}
		if seen[name] {
			return nil
		}
		seen[name] = true
		var out []string
		for _, c := range calls[name] {
			if slices.Contains(serialOnly, c) {
				out = append(out, c)
			} else if _, ours := calls[c]; ours && c != name {
				out = append(out, reach(c, seen)...)
			}
		}
		sort.Strings(out)
		out = slices.Compact(out)
		reached[name] = out
		return out
	}
	var problems []string
	for _, fn := range tests {
		name := fn.Name.Name
		switches := reach(name, map[string]bool{})
		parallel, late := parallelFirst(fn)
		why := serialWhy(fn)
		switch {
		case late:
			problems = append(problems, fmt.Sprintf("%s calls t.Parallel() after its first statement; call it first", name))
		case parallel && len(switches) > 0:
			problems = append(problems, fmt.Sprintf("%s runs in parallel but reaches %s; make it serial with a line \"Serial: %s\"",
				name, strings.Join(switches, ", "), strings.Join(switches, ", ")))
		case parallel && why != "":
			problems = append(problems, fmt.Sprintf("%s runs in parallel and says \"Serial: %s\"; one or the other", name, why))
		case !parallel && why == "" && len(switches) > 0:
			problems = append(problems, fmt.Sprintf("%s is serial (it reaches %s) and must say so on a line \"Serial: %s\"",
				name, strings.Join(switches, ", "), strings.Join(switches, ", ")))
		case !parallel && why == "":
			problems = append(problems, fmt.Sprintf("%s neither calls t.Parallel() first nor says why not (a line \"Serial: <why>\" in its comment)", name))
		}
	}
	return len(tests), problems
}

// calledNames are the names of the functions a body calls (a plain call by
// its name, a selector such as store.SetHook, w.legacy or t.Setenv by the
// selected name), and of the package-level switches it names.
func calledNames(body *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			switch f := x.Fun.(type) {
			case *ast.Ident:
				out = append(out, f.Name)
			case *ast.SelectorExpr:
				out = append(out, f.Sel.Name)
			}
		case *ast.Ident:
			if slices.Contains(serialOnly, x.Name) {
				out = append(out, x.Name)
			}
		}
		return true
	})
	return out
}

// parallelFirst says whether the test's first statement is t.Parallel(),
// and whether it calls t.Parallel() later instead.
func parallelFirst(fn *ast.FuncDecl) (first, late bool) {
	for i, s := range fn.Body.List {
		e, ok := s.(*ast.ExprStmt)
		if !ok {
			continue
		}
		c, ok := e.X.(*ast.CallExpr)
		if !ok || len(c.Args) != 0 {
			continue
		}
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
			if i == 0 {
				return true, false
			}
			return false, true
		}
	}
	return false, false
}

// serialWhy is the reason a test's comment gives for running serially, ""
// when it gives none.
func serialWhy(fn *ast.FuncDecl) string {
	if fn.Doc == nil {
		return ""
	}
	for _, c := range fn.Doc.List {
		line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if why, ok := strings.CutPrefix(line, "Serial:"); ok && strings.TrimSpace(why) != "" {
			return strings.TrimSpace(why)
		}
	}
	return ""
}
