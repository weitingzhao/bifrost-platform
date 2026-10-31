package s5check

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tempGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=S5 test", "GIT_AUTHOR_EMAIL=s5@example.invalid", "GIT_COMMITTER_NAME=S5 test", "GIT_COMMITTER_EMAIL=s5@example.invalid"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitReaderUsesObjectsNotWorktreeAndDoesNotRunHooks(t *testing.T) {
	dir := t.TempDir()
	tempGit(t, dir, "init", "--template=", ".")
	file := filepath.Join(dir, "source.ts")
	if err := os.WriteFile(file, []byte("export const n = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tempGit(t, dir, "add", "source.ts")
	tempGit(t, dir, "commit", "-m", "base")
	base := tempGit(t, dir, "rev-parse", "HEAD")
	if !fullSHA.MatchString(base) {
		t.Fatalf("unexpected git SHA %q", base)
	}
	if err := os.WriteFile(file, []byte("export const n = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tempGit(t, dir, "add", "source.ts")
	tempGit(t, dir, "commit", "-m", "result")
	result := tempGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(file, []byte("throw new Error('uncommitted candidate must not execute')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := GitTreeReader{Directory: dir}
	b, err := r.ReadTree(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	a, err := r.ReadTree(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	if b.Files["source.ts"].Content != "export const n = 1\n" || a.Files["source.ts"].Content != "export const n = 2\n" || a.Parents[0] != base {
		t.Fatal("reader consulted working tree")
	}
	patch, err := r.ReadPatch(context.Background(), base, result)
	if err != nil || len(patch.Bytes) == 0 || !fullSHA.MatchString(patch.StableID) {
		t.Fatalf("patch: %+v %v", patch, err)
	}
	for _, bad := range []string{"HEAD", result + "^", "--help", "../source.ts"} {
		if _, err := r.ReadTree(context.Background(), bad); err == nil {
			t.Fatalf("accepted non-immutable %q", bad)
		}
	}
	if got := tempGit(t, dir, "status", "--porcelain"); got != "M source.ts" {
		t.Fatalf("reader changed checkout: %q", got)
	}
}

func TestGitReaderRetainsBinaryAndSymlinkModes(t *testing.T) {
	dir := t.TempDir()
	tempGit(t, dir, "init", "--template=", ".")
	if err := os.WriteFile(filepath.Join(dir, "binary.ts"), []byte{'a', 0, 'b'}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("binary.ts", filepath.Join(dir, "link.ts")); err != nil {
		t.Fatal(err)
	}
	tempGit(t, dir, "add", "binary.ts", "link.ts")
	tempGit(t, dir, "commit", "-m", "binary and link")
	sha := tempGit(t, dir, "rev-parse", "HEAD")
	tree, err := (GitTreeReader{Directory: dir}).ReadTree(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	if !tree.Files["binary.ts"].Binary || tree.Files["link.ts"].Mode != "120000" {
		t.Fatalf("unsupported endpoints disappeared: %+v", tree.Files)
	}
}
