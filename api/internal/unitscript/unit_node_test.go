package unitscript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The node script with npm and vitest stood in for: what it writes for each
// way a task ends. The stand-ins are a fake npm (FAKE_NPM_FAIL names what
// fails: ci, type-check, lint, build) and a fake npx that writes the vitest
// report FAKE_VITEST_REPORT and exits FAKE_VITEST_EXIT. The script itself
// and its node program are the real ones, so node is needed: the Go image CI
// tests in has none and the test is skipped there (it runs on a developer's
// machine; the real runs are the unit tasks themselves).
func runNode(t *testing.T, task string, env ...string) outcome {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir, bin, result := t.TempDir(), t.TempDir(), t.TempDir()
	write := func(p, body string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "pkg", "package.json"), `{"name":"p","dependencies":{"beside":"file:../beside","left-pad":"1.0.0"}}`, 0o644)
	write(filepath.Join(dir, "beside", "package.json"), `{"name":"beside"}`, 0o644)
	write(filepath.Join(dir, "pkg", "src", "a.test.ts"), "", 0o644)
	write(filepath.Join(dir, "pkg", "src", "b.test.tsx"), "", 0o644)
	write(filepath.Join(dir, "pkg", "node_modules", "x", "c.test.ts"), "", 0o644)
	write(filepath.Join(bin, "npm"), `#!/bin/sh
echo "npm $* in $(pwd)" >> "$FAKE_LOG"
case " $FAKE_NPM_FAIL " in *" $1 "*|*" ${2:-} "*) exit 1 ;; esac
exit 0
`, 0o755)
	write(filepath.Join(bin, "npx"), `#!/bin/sh
echo "npx $*" >> "$FAKE_LOG"
for a in "$@"; do case "$a" in --outputFile.json=*) [ -n "${FAKE_VITEST_REPORT:-}" ] && printf '%s' "$FAKE_VITEST_REPORT" > "${a#--outputFile.json=}" ;; esac; done
exit "${FAKE_VITEST_EXIT:-0}"
`, 0o755)
	log := filepath.Join(result, "calls.log")
	cmd := exec.Command(script(t, "unit-console-"+task))
	cmd.Dir = filepath.Join(dir, "pkg")
	cmd.Env = append(os.Environ(), append([]string{"UNIT_RESULT_DIR=" + result, "PATH=" + bin + ":" + os.Getenv("PATH"), "FAKE_LOG=" + log, "UNIT_CACHE="}, env...)...)
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
	calls, _ := os.ReadFile(log)
	// The package beside is installed before this one, each in its directory.
	if lines := strings.Split(strings.TrimSpace(string(calls)), "\n"); len(lines) >= 2 && !strings.Contains(string(out), "installing") {
		if !strings.Contains(lines[0], "npm ci --prefer-offline in") || !strings.HasSuffix(lines[0], "/beside") || !strings.HasSuffix(lines[1], "/pkg") {
			t.Errorf("install order: %q then %q", lines[0], lines[1])
		}
	}
	if !strings.Contains(string(out), "unit-node: install ") && o.exit == 0 {
		t.Errorf("the output does not say how long the install took:\n%s", out)
	}
	return o
}

func TestUnitNodeResults(t *testing.T) {
	const pass = `{"numTotalTests":3,"numPassedTests":2,"numFailedTests":0,"numPendingTests":1,"testResults":[{"name":"src/a.test.ts","status":"passed","assertionResults":[{"status":"passed","fullName":"a one"}]}]}`
	const red = `{"numTotalTests":3,"numPassedTests":1,"numFailedTests":2,"testResults":[{"name":"src/a.test.ts","status":"failed","assertionResults":[{"status":"failed","fullName":"a two, with a comma","failureMessages":["expected 1 to be 2"]},{"status":"failed","fullName":"a slow one","failureMessages":["Error: Test timed out in 5000ms."]}]}]}`
	const slow = `{"numTotalTests":2,"numPassedTests":1,"numFailedTests":1,"testResults":[{"name":"src/a.test.ts","status":"failed","assertionResults":[{"status":"failed","fullName":"a slow one","failureMessages":["Error: Test timed out in 5000ms."]}]}]}`
	const broken = `{"numTotalTests":0,"numPassedTests":0,"numFailedTests":0,"testResults":[{"name":"src/a.test.ts","status":"failed","assertionResults":[]}]}`
	for name, tc := range map[string]struct {
		task  string
		env   []string
		exit  int
		class string
		tests string // what the tests result starts and ends with
		fails string
	}{
		"tests pass":                     {"test", []string{"FAKE_VITEST_REPORT=" + pass}, 0, "", "3 |2 1", ""},
		"a red test":                     {"test", []string{"FAKE_VITEST_REPORT=" + red, "FAKE_VITEST_EXIT=1"}, 1, "test", "3 |3 0", "src/a.test.ts > a slow one,src/a.test.ts > a two; with a comma"},
		"every failure is a timeout":     {"test", []string{"FAKE_VITEST_REPORT=" + slow, "FAKE_VITEST_EXIT=1"}, 1, "", "2 |2 0", "src/a.test.ts > a slow one"},
		"a test file that does not load": {"test", []string{"FAKE_VITEST_REPORT=" + broken, "FAKE_VITEST_EXIT=1"}, 1, "build", "0 |0 0", ""},
		"vitest dies with no report":     {"test", []string{"FAKE_VITEST_EXIT=137"}, 137, "", "", ""},
		"the install fails":              {"test", []string{"FAKE_NPM_FAIL=ci"}, 1, "", "", ""},
		"typecheck passes":               {"typecheck", nil, 0, "", "", ""},
		"typecheck fails":                {"typecheck", []string{"FAKE_NPM_FAIL=type-check"}, 1, "build", "", ""},
		"lint fails":                     {"lint", []string{"FAKE_NPM_FAIL=lint"}, 1, "test", "", ""},
		"build fails":                    {"build", []string{"FAKE_NPM_FAIL=build"}, 1, "build", "", ""},
	} {
		o := runNode(t, tc.task, tc.env...)
		start, end, _ := strings.Cut(tc.tests, "|")
		if o.exit != tc.exit || o.failClass != tc.class || o.failedTests != tc.fails || !strings.HasPrefix(o.tests, start) || !strings.HasSuffix(o.tests, end) {
			t.Errorf("%s: exit %d, fail-class %q, tests %q, failed-tests %q", name, o.exit, o.failClass, o.tests, o.failedTests)
		}
		// The list the parts are cut from is the package's test files, not its dependencies'.
		if tc.tests != "" && (len(strings.Fields(o.tests)) != 4 || len(strings.Fields(o.tests)[1]) != 64) {
			t.Errorf("%s: the list hash is %q", name, strings.Fields(o.tests)[1])
		}
	}
	a := runNode(t, "test", "FAKE_VITEST_REPORT="+pass, "UNIT_SHARD_INDEX=1", "UNIT_SHARD_TOTAL=4")
	b := runNode(t, "test", "FAKE_VITEST_REPORT="+pass)
	if strings.Fields(a.tests)[1] != strings.Fields(b.tests)[1] {
		t.Error("two parts of one package report different lists")
	}
}
