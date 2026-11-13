// Package unitscript holds the tests of the scripts a unit's task runs
// (scripts/ci/unit-*): what they write is what the platform reads when it
// collects a task, so the contract is tested where the platform is.
package unitscript

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func script(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(file), "..", "..", "..", "scripts", "ci", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the script is not where the manifest says it is: %v", err)
	}
	return p
}

// module writes a throwaway Go module with one package of the given test file.
func module(t *testing.T, test string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.com/um\n\ngo 1.25\n", "a_test.go": test} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type outcome struct {
	exit                          int
	tests, failedTests, failClass string
}

func run(t *testing.T, dir string, env ...string) outcome {
	t.Helper()
	result := t.TempDir()
	cmd := exec.Command(script(t, "unit-go-test"))
	cmd.Dir = dir
	// The module is offline and its own: nothing of the caller's Go settings.
	cmd.Env = append(os.Environ(), append([]string{"UNIT_RESULT_DIR=" + result, "GOFLAGS=", "GOPROXY=off", "GOWORK=off"}, env...)...)
	out, err := cmd.CombinedOutput()
	o := outcome{}
	if exit, ok := err.(*exec.ExitError); ok {
		o.exit = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("the script did not run: %v\n%s", err, out)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(result, name))
		if err != nil {
			t.Fatalf("no result %s: %v\n%s", name, err, out)
		}
		return string(b)
	}
	o.tests, o.failedTests, o.failClass = read("tests"), read("failed-tests"), read("fail-class")
	return o
}

const mixed = `package red

import "testing"

func TestGood(t *testing.T) {}
func TestBad(t *testing.T)  { t.Run("sub", func(t *testing.T) { t.Fatal("no") }) }
func TestSkip(t *testing.T) { t.Skip("later") }
func BenchmarkX(b *testing.B) {}
`

// What a unit's test task reports: the four numbers, the failed tests and the
// class, for a pass, a failed test, a package that does not build and a test
// program that dies; and the parts of a split are every test once.
func TestUnitGoTestResults(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test in a throwaway module")
	}
	t.Parallel()
	fields := func(o outcome) []string { return strings.Fields(o.tests) }

	pass := run(t, module(t, "package ok\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\nfunc TestB(t *testing.T) {}\n"))
	if f := fields(pass); pass.exit != 0 || len(f) != 4 || f[0] != "2" || len(f[1]) != 64 || f[2] != "2" || f[3] != "0" || pass.failedTests != "" || pass.failClass != "" {
		t.Fatalf("a pass: %+v", pass)
	}

	dir := module(t, mixed)
	red := run(t, dir)
	if f := fields(red); red.exit == 0 || len(f) != 4 || f[0] != "3" || f[2] != "2" || f[3] != "1" || red.failClass != "test" ||
		red.failedTests != "example.com/um.TestBad,example.com/um.TestBad/sub" {
		t.Fatalf("a failed test: %+v", red)
	}
	// Split in two: the same list hash, the expected counts add up to the
	// whole, and only the part with the failing test fails.
	total, failedParts := 0, 0
	for i := 0; i < 2; i++ {
		part := run(t, dir, fmt.Sprintf("UNIT_SHARD_INDEX=%d", i), "UNIT_SHARD_TOTAL=2")
		f := fields(part)
		if len(f) != 4 || f[1] != fields(red)[1] {
			t.Fatalf("part %d: %+v", i, part)
		}
		var n int
		fmt.Sscan(f[0], &n)
		total += n
		if part.exit != 0 {
			failedParts++
		}
	}
	if total != 3 || failedParts != 1 {
		t.Fatalf("the parts: %d tests in all, %d parts failed", total, failedParts)
	}
	if empty := run(t, dir, "UNIT_SHARD_INDEX=7", "UNIT_SHARD_TOTAL=8"); empty.exit != 0 || !strings.HasPrefix(empty.tests, "0 ") || empty.failClass != "" {
		t.Fatalf("a part with no test: %+v", empty)
	}

	build := run(t, module(t, "package b\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { undefined() }\n"))
	if build.exit == 0 || build.failClass != "build" || build.failedTests != "" || build.tests != "" {
		t.Fatalf("a package that does not build: %+v", build)
	}

	died := run(t, module(t, "package d\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestA(t *testing.T) {}\nfunc TestDie(t *testing.T) { os.Exit(3) }\n"))
	if f := fields(died); died.exit == 0 || died.failClass != "build" || len(f) != 4 || f[0] != "2" || f[2] == "2" {
		t.Fatalf("a test program that dies: %+v", died)
	}

	var many strings.Builder
	many.WriteString("package many\n\nimport \"testing\"\n\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&many, "func TestCase%03dWithAVeryLongNameToFillTheResultOfFailedTests(t *testing.T) { t.Fatal(%d) }\n", i, i)
	}
	long := run(t, module(t, many.String()))
	if len(long.failedTests) > 1500 || !strings.HasSuffix(long.failedTests, ",…") || long.failClass != "test" || !strings.HasPrefix(long.tests, "120 ") {
		t.Fatalf("120 failed tests: %d bytes, class %q, tests %q", len(long.failedTests), long.failClass, long.tests)
	}
}
