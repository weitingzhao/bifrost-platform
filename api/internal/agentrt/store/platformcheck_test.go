package store_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/platformcheck"
)

func platformFixture(t *testing.T) string {
	t.Helper()
	return platformFixtureAt(t, t.TempDir())
}
func platformFixtureAt(t *testing.T, root string) string {
	t.Helper()
	err := os.CopyFS(filepath.Join(root, "api"), os.DirFS("../platformcheck/testdata/fixture"))
	if err != nil {
		t.Fatal(err)
	}
	platformGit(t, root, "init")
	platformCommit(t, root)
	return root
}
func platformGit(t *testing.T, root string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	c.Dir = root
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v\n%s", args, e, b)
	}
}
func platformCommit(t *testing.T, root string) {
	t.Helper()
	platformGit(t, root, "add", ".")
	platformGit(t, root, "commit", "-m", "fixture")
}
func platformWrite(t *testing.T, root, path, body string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func platformRun(t *testing.T, root, mode, pg string) (int, string) {
	t.Helper()
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "AGENTRT_TEST_PG=") && !strings.HasPrefix(v, "AGENTRT_TEST_REQUIRED=") {
			env = append(env, v)
		}
	}
	env = append(env, "AGENTRT_TEST_PG="+pg, "AGENTRT_TEST_REQUIRED=1")
	var out bytes.Buffer
	code := platformcheck.Run(context.Background(), filepath.Join(root, "api"), mode, env, &out)
	return code, out.String()
}
func TestPlatformCheckRulesAndChanged(t *testing.T) {
	t.Parallel()
	root := platformFixture(t)
	platformWrite(t, root, "api/b/b.go", "package b\nconst Value = \"forbidden\"\n")
	platformCommit(t, root)
	if code, out := platformRun(t, root, "rules", ""); code == 0 || !strings.Contains(out, "TestSourceRule") {
		t.Fatalf("rules=%d\n%s", code, out)
	}
	if code, out := platformRun(t, root, "changed", ""); code != 0 || !strings.Contains(out, "changed: fixture/b") || strings.Contains(out, "fixture/a") {
		t.Fatalf("changed=%d\n%s", code, out)
	}
}
func TestPlatformCheckDatabase(t *testing.T) {
	t.Parallel()
	root := platformFixture(t)
	platformWrite(t, root, "api/c/c.go", "package c\nconst Value = 2\n")
	platformCommit(t, root)
	if code, out := platformRun(t, root, "changed", "fixture"); code == 0 || !strings.Contains(out, "bad database value") {
		t.Fatalf("database=%d\n%s", code, out)
	}
	if code, out := platformRun(t, root, "changed", ""); code != 0 || !strings.HasSuffix(out, platformcheck.NoDatabase+"\n") {
		t.Fatalf("skip=%d\n%s", code, out)
	}
}
func TestPlatformCheckOutsideModule(t *testing.T) {
	t.Parallel()
	root := platformFixture(t)
	platformWrite(t, root, "outside.txt", "x")
	platformCommit(t, root)
	if code, out := platformRun(t, root, "changed", ""); code != 0 || !strings.Contains(out, "no package changed") {
		t.Fatalf("outside=%d\n%s", code, out)
	}
}
func TestPlatformCheckMissingParent(t *testing.T) {
	t.Parallel()
	root := platformFixture(t)
	if code, out := platformRun(t, root, "changed", ""); code == 0 || !strings.Contains(out, "HEAD^") {
		t.Fatalf("parent=%d\n%s", code, out)
	}
}

// TestPlatformCheckSymlinkedRoot runs the check from a path that reaches the
// repository through a symbolic link, as t.TempDir does on macOS where /var
// links to /private/var. go list reports resolved directories, so the changed
// package is found only when both sides are resolved before comparison.
func TestPlatformCheckSymlinkedRoot(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(base, "link")
	if e := os.Symlink(real, link); e != nil {
		t.Fatal(e)
	}
	root := platformFixtureAt(t, link)
	platformWrite(t, root, "api/b/b.go", "package b\nconst Value = 2\n")
	platformCommit(t, root)
	if code, out := platformRun(t, root, "changed", ""); code != 0 || !strings.Contains(out, "changed: fixture/b") || strings.Contains(out, "fixture/a") {
		t.Fatalf("symlinked=%d\n%s", code, out)
	}
}
