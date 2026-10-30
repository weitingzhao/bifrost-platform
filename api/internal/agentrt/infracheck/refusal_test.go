package infracheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, text string, mode os.FileMode) {
	t.Helper()
	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func quickFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, entry := range infraTests {
		if strings.HasSuffix(entry, ".py") {
			writeFixture(t, dir, entry, "", 0600)
		} else if err := os.MkdirAll(filepath.Join(dir, entry), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, dir, ".git/venv/bin/python3", "#!/bin/sh\nprintf '%s\\n' \"$PYTEST_DISABLE_PLUGIN_AUTOLOAD\" \"$@\" > pytest-ran\n", 0700)
	return dir
}

func refusedQuick(t *testing.T, dir, name, message string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", Check())
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 || !strings.Contains(string(out), name) || !strings.Contains(string(out), message) {
		t.Fatalf("expected exit 3 naming %s (%s), got %v: %s", name, message, err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pytest-ran")); !os.IsNotExist(err) {
		t.Fatal("pytest executed despite refusal", err)
	}
}

func TestQuickMissingEntries(t *testing.T) {
	for _, entry := range infraTests {
		t.Run(entry, func(t *testing.T) {
			dir := quickFixture(t)
			if err := os.Remove(filepath.Join(dir, entry)); err != nil {
				t.Fatal(err)
			}
			refusedQuick(t, dir, entry, "quick list entry missing")
		})
	}
}

func TestQuickRefusesPackageInitializers(t *testing.T) {
	for _, file := range []string{
		"__init__.py", "agent-config/__init__.py", "agent-config/scripts/__init__.py", "k8s/__init__.py", "k8s/data/__init__.py", "scripts/__init__.py",
		"scripts/agentrt/__init__.py", "scripts/agentrt/nested/__init__.py", "scripts/landing/__init__.py", "scripts/landing/nested/__init__.py",
	} {
		for _, dangling := range []bool{false, true} {
			t.Run(file+map[bool]string{true: "/dangling", false: "/file"}[dangling], func(t *testing.T) {
				dir := quickFixture(t)
				writeFixture(t, dir, file, "raise RuntimeError('must not import')\n", 0600)
				if dangling {
					if err := os.Remove(filepath.Join(dir, file)); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("absent", filepath.Join(dir, file)); err != nil {
						t.Fatal(err)
					}
				}
				refusedQuick(t, dir, file, "Python package")
			})
		}
	}
}

func TestQuickFollowsDirectorySymlinks(t *testing.T) {
	dir := quickFixture(t)
	writeFixture(t, dir, "outside/nested/__init__.py", "", 0600)
	if err := os.Symlink(filepath.Join(dir, "outside"), filepath.Join(dir, "scripts/agentrt/linked")); err != nil {
		t.Fatal(err)
	}
	refusedQuick(t, dir, "scripts/agentrt/linked/nested/__init__.py", "Python package")
}

func TestQuickValidTreeRunsIsolatedPytest(t *testing.T) {
	dir := quickFixture(t)
	cmd := exec.Command("sh", "-c", Check())
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pytest-ran"))
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n-I\n-m\npytest\n-c\n/dev/null\n--rootdir=" + dir + "\n--noconftest\n-q\n-p\nno:cacheprovider\n"
	if string(raw) != want+strings.Join(infraTests, "\n")+"\n" {
		t.Fatalf("pytest lost isolation or its test list: %s", raw)
	}
}
