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

// parallelFloor is the -parallel TestMain sets, and whether it sets one: none
// when the command names it or GOMAXPROCS is already at least minParallel.
func parallelFloor(explicit bool, procs int) (value int, set bool) {
	if explicit || procs >= minParallel {
		return 0, false
	}
	return minParallel, true
}

// parallelAsked is what TestMain found before it set anything: whether the
// command named -parallel, and the value it left (review 322 B1: flag.Set
// makes a flag look set to flag.Visit, so a test cannot tell afterwards).
var parallelAsked struct {
	explicit bool
	want     string
}

func TestMain(m *testing.M) {
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "test.parallel" })
	parallelAsked.explicit = explicit
	parallelAsked.want = flag.Lookup("test.parallel").Value.String()
	if n, set := parallelFloor(explicit, runtime.GOMAXPROCS(0)); set {
		if err := flag.Set("test.parallel", fmt.Sprint(n)); err != nil {
			fmt.Fprintln(os.Stderr, "store tests: -parallel:", err)
			os.Exit(2)
		}
		parallelAsked.want = fmt.Sprint(n)
	}
	os.Exit(m.Run())
}

// TestMain's floor holds: without -parallel on the command line, at least
// minParallel tests run at once; with it, the command's number.
func TestParallelFloor(t *testing.T) {
	t.Parallel()
	got := flag.Lookup("test.parallel").Value.String()
	if got != parallelAsked.want {
		t.Fatalf("-parallel is %s; TestMain meant %s", got, parallelAsked.want)
	}
	if !parallelAsked.explicit && got != fmt.Sprint(max(runtime.GOMAXPROCS(0), minParallel)) {
		t.Fatalf("-parallel is %s at GOMAXPROCS %d without -parallel; want at least %d", got, runtime.GOMAXPROCS(0), minParallel)
	}
	for _, c := range []struct {
		explicit bool
		procs    int
		value    int
		set      bool
	}{{false, 1, minParallel, true}, {false, 2, minParallel, true}, {false, minParallel, 0, false}, {false, 10, 0, false}, {true, 2, 0, false}} {
		if v, set := parallelFloor(c.explicit, c.procs); v != c.value || set != c.set {
			t.Errorf("parallelFloor(%v, %d) = %d, %v; want %d, %v", c.explicit, c.procs, v, set, c.value, c.set)
		}
	}
}

// TD-374's ratchet: the package's tests run in parallel, so the package stays
// well under CI's per-binary timeout as it grows (each test has a database of
// its own). A test calls t.Parallel() first, or says on a line of its comment
// starting "Serial:" why it may not (a package-level switch, or timing a
// parallel neighbour could break: a window of a few seconds, a lock let go on
// a timer, a time budget). A test that reaches a package-level switch
// (serialOnly), from its own body or through a function or variable declared
// in the package's test files, must be serial: a parallel test would run
// under another test's switch, and t.Setenv panics in one.
//
// What the scan sees is names: it does not see a switch behind another
// package's function or method. A new package-level switch (a hook, a
// variable a test sets, a limit a test lowers), or a helper outside these
// files that reaches one, goes into serialOnly, with a case in
// TestParallelScanFindsWhatItShould.
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

// The scan itself, on a package made up for it (a positive control: a scan
// that finds nothing proves nothing until it is shown to find something).
func TestParallelScanFindsWhatItShould(t *testing.T) {
	t.Parallel()
	const src = `package store_test

func helper() { store.SetHook(nil) }
func (w *world) frozen() { defer store.FreezeEventClock(time.Now())() }
func indirect() { helper() }

// Two helpers that call each other: each reaches the hook, whichever a
// test calls first (review 322 B3).
func ping(n int) { if n > 0 { pong(n - 1) }; store.SetHook(nil) }
func pong(n int) { if n > 0 { ping(n - 1) } }

var install = store.SetHook
var freeze = func() { store.FreezeEventClock(time.Now()) }

func TestGood(t *testing.T) { t.Parallel() }

// Serial: SetHook.
func TestSerialOK(t *testing.T) { indirect() }

/* Serial: a block comment
   says so too. */
func TestSerialInABlock(t *testing.T) { indirect() }

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

func TestPing(t *testing.T) {
	t.Parallel()
	ping(1)
}

func TestPong(t *testing.T) {
	t.Parallel()
	pong(1)
}

func TestAliased(t *testing.T) {
	t.Parallel()
	install(nil)
}

func TestStoredClosure(t *testing.T) {
	t.Parallel()
	freeze()
}

func TestHelperAsValue(t *testing.T) {
	t.Parallel()
	f := helper
	f()
}

func TestNotItsT(t *testing.T) {
	other.Parallel()
}
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
		"TestEmptyWhy", "TestPing", "TestPong", "TestAliased", "TestStoredClosure", "TestHelperAsValue", "TestNotItsT", "TestInsideClock"}
	if tests != 17 || len(got) != len(want) {
		t.Fatalf("%d tests, problems %v; want 17 tests and %v", tests, problems, want)
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
	// The names each function and package-level variable of the test files
	// uses (calls them, takes them as values, selects them: a method by its
	// own name, so a helper of that name anywhere counts, which errs on the
	// side of serial).
	uses := map[string][]string{}
	var tests []*ast.FuncDecl
	for _, f := range files {
		if f.Name.Name != "store_test" && f.Name.Name != "store" {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				uses[d.Name.Name] = append(uses[d.Name.Name], namesIn(d.Body)...)
				if strings.HasPrefix(d.Name.Name, "Test") && d.Name.Name != "TestMain" && d.Recv == nil {
					tests = append(tests, d)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					v, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					var names []string
					for _, e := range v.Values {
						names = append(names, namesIn(e)...)
					}
					for _, n := range v.Names {
						uses[n.Name] = append(uses[n.Name], names...)
					}
				}
			}
		}
	}
	// reach is every switch a name leads to, each test on its own walk: no
	// result is kept between tests, so a walk cut short at a cycle never
	// stands for another test's (review 322 B3).
	reach := func(root string) []string {
		var out []string
		seen := map[string]bool{}
		var walk func(name string)
		walk = func(name string) {
			if seen[name] {
				return
			}
			seen[name] = true
			for _, u := range uses[name] {
				if slices.Contains(serialOnly, u) {
					out = append(out, u)
				} else if _, ours := uses[u]; ours {
					walk(u)
				}
			}
		}
		walk(root)
		sort.Strings(out)
		return slices.Compact(out)
	}
	var problems []string
	for _, fn := range tests {
		name := fn.Name.Name
		switches := reach(name)
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

// namesIn is every identifier a node holds: what it calls, takes as a value
// or selects (store.SetHook, w.legacy, t.Setenv by the selected name).
func namesIn(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// parallelFirst says whether the test's first statement is t.Parallel() on
// its own *testing.T, and whether it calls that later instead.
func parallelFirst(fn *ast.FuncDecl) (first, late bool) {
	param := ""
	if ps := fn.Type.Params.List; len(ps) == 1 && len(ps[0].Names) == 1 {
		param = ps[0].Names[0].Name
	}
	for i, s := range fn.Body.List {
		e, ok := s.(*ast.ExprStmt)
		if !ok {
			continue
		}
		c, ok := e.X.(*ast.CallExpr)
		if !ok || len(c.Args) != 0 {
			continue
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Parallel" {
			continue
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != param {
			continue
		}
		if i == 0 {
			return true, false
		}
		return false, true
	}
	return false, false
}

// serialWhy is the reason a test's comment gives for running serially, ""
// when it gives none; a line of a // or a /* */ comment counts.
func serialWhy(fn *ast.FuncDecl) string {
	if fn.Doc == nil {
		return ""
	}
	for _, c := range fn.Doc.List {
		text := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"), "*/")
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
			if why, ok := strings.CutPrefix(line, "Serial:"); ok && strings.TrimSpace(why) != "" {
				return strings.TrimSpace(why)
			}
		}
	}
	return ""
}
