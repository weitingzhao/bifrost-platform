package unitscript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unit-check on a throwaway repository: what it writes when its checks pass,
// when one fails, and when the repository has none of them.
func runCheck(t *testing.T, files map[string]string) outcome {
	t.Helper()
	root, result := t.TempDir(), t.TempDir()
	repo := filepath.Join(root, "some-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(script(t, "unit-check"))
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "UNIT_RESULT_DIR="+result)
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

func TestUnitCheckResults(t *testing.T) {
	const spine = "some-repo/scripts/ci/check_spine_catalog.sh"
	// The ratchet is called with the workspace root and the repository's name.
	const scan = "bifrost-trade-infra/agent-config/scripts/code-health/scan.sh"
	scanOK := "#!/bin/bash\n[ \"$1\" = --root ] && [ -d \"$2/some-repo\" ] && [ \"$3\" = --repo ] && [ \"$4\" = some-repo ]\n"
	for name, tc := range map[string]struct {
		files                map[string]string
		exit                 int
		class, failed, tests string // tests: what it starts and ends with
	}{
		"both checks pass":            {map[string]string{spine: "#!/bin/bash\nexit 0\n", scan: scanOK}, 0, "", "", "2 |2 0"},
		"the spine check fails":       {map[string]string{spine: "#!/bin/bash\nexit 1\n", scan: scanOK}, 1, "test", "spine-catalog", "2 |2 0"},
		"both fail":                   {map[string]string{spine: "#!/bin/bash\nexit 1\n", scan: "#!/bin/bash\nexit 3\n"}, 1, "test", "spine-catalog,code-health", "2 |2 0"},
		"infra is not beside it":      {map[string]string{spine: "#!/bin/bash\nexit 0\n"}, 0, "", "", "2 |1 1"},
		"a repository with no checks": {map[string]string{}, 0, "", "", "2 |0 2"},
	} {
		o := runCheck(t, tc.files)
		start, end, _ := strings.Cut(tc.tests, "|")
		if o.exit != tc.exit || o.failClass != tc.class || o.failedTests != tc.failed || !strings.HasPrefix(o.tests, start) || !strings.HasSuffix(o.tests, end) || len(strings.Fields(o.tests)) != 4 {
			t.Errorf("%s: exit %d, fail-class %q, failed-tests %q, tests %q", name, o.exit, o.failClass, o.failedTests, o.tests)
		}
	}
}
