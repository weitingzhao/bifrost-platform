package agentd_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// infra's check, run as agentd runs it, with real pytest: a failing test
// fails it whatever the change adds at the checkout's root (pytest.ini,
// tox.ini, setup.cfg or pyproject.toml asking to collect only, a conftest.py
// that clears the session's failures) and whatever a directory above the
// checkout holds (review 330 A1). pytest is the host's python3's: the test is
// skipped where it has none.
func TestInfraCheckTakesNoPlantedConfiguration(t *testing.T) {
	if exec.Command("python3", "-I", "-c", "import pytest").Run() != nil {
		t.Skip("no python3 with pytest on this host")
	}
	check := store.Repos["bifrost-trade-infra"].Verify
	_, paths, ok := strings.Cut(check[0][2], "-p no:cacheprovider ")
	if !ok || len(check) != 1 {
		t.Fatalf("infra's check: %q", check)
	}
	write := func(path, body string) {
		t.Helper()
		must(t, os.MkdirAll(filepath.Dir(path), 0o755))
		must(t, os.WriteFile(path, []byte(body), 0o644))
	}
	// tree is a checkout with a passing test at every path the check names,
	// and a failing one when asked, inside a directory of its own.
	tree := func(failing bool) string {
		dir := filepath.Join(t.TempDir(), "a1")
		for i, p := range strings.Fields(paths) {
			file := filepath.Join(dir, p)
			if !strings.HasSuffix(p, ".py") {
				file = filepath.Join(file, fmt.Sprintf("test_ok_%d.py", i))
			}
			write(file, "def test_ok():\n    pass\n")
		}
		if failing {
			write(filepath.Join(dir, "scripts", "agentrt", "test_broken.py"), "def test_broken():\n    assert False\n")
		}
		return dir
	}
	run := func(dir string) agentd.Verification { return agentd.Verify(ctx, dir, os.Environ(), check, time.Minute) }
	if v := run(tree(false)); !v.Pass {
		t.Fatalf("a passing tree failed: %+v", v)
	}
	if v := run(tree(true)); v.Pass {
		t.Fatalf("a failing test passed: %+v", v)
	}
	const collectOnly = "addopts = --collect-only\n"
	const swallow = "def pytest_sessionfinish(session, exitstatus):\n    session.exitstatus = 0\n"
	for name, files := range map[string]map[string]string{
		"pytest.ini":     {"pytest.ini": "[pytest]\n" + collectOnly},
		"tox.ini":        {"tox.ini": "[pytest]\n" + collectOnly},
		"setup.cfg":      {"setup.cfg": "[tool:pytest]\n" + collectOnly},
		"pyproject.toml": {"pyproject.toml": "[tool.pytest.ini_options]\naddopts = \"--collect-only\"\n"},
		"conftest.py":    {"conftest.py": swallow},
		"above":          {"../pytest.ini": "[pytest]\n" + collectOnly, "../conftest.py": swallow},
	} {
		dir := tree(true)
		for path, body := range files {
			write(filepath.Join(dir, path), body)
		}
		if v := run(dir); v.Pass {
			t.Errorf("%s: a failing test passed the check:\n%s", name, v.Commands[0].Tail)
		}
	}
}
