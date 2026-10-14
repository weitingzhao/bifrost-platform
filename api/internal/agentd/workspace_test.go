package agentd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitEnv(home string) []string {
	return append(os.Environ(), "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=agentd", "GIT_AUTHOR_EMAIL=agentd@h1", "GIT_COMMITTER_NAME=agentd", "GIT_COMMITTER_EMAIL=agentd@h1")
}

func run(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// remote is a bare repository with one commit on main; it returns the
// remote's path and that commit.
func remote(t *testing.T, env []string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	run(t, root, env, "git", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	seed := filepath.Join(root, "seed")
	run(t, root, env, "git", "clone", "--quiet", bare, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, seed, env, "git", "add", "README.md")
	run(t, seed, env, "git", "commit", "--quiet", "-m", "first")
	run(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, run(t, seed, env, "git", "rev-parse", "HEAD")
}

func TestWorkspace(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	ctx := context.Background()
	dir := WorkspaceDir(t.TempDir(), "t_01M4XW5Z7AC7CTA9P0YDGACGPG", 1)
	w, err := Prepare(ctx, dir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if dirty, dirtyErr := w.Dirty(ctx); dirtyErr != nil || dirty {
		t.Fatalf("a fresh checkout is dirty: %v %v", dirty, dirtyErr)
	}
	if empty, emptyErr := w.Commit(ctx, "nothing", Lineage{Task: "t_x", ChangeID: "Ix", Work: "W-31"}); emptyErr != nil || empty != "" {
		t.Fatalf("an empty commit = %q %v", empty, emptyErr)
	}
	if _, err = Prepare(ctx, dir, bare, base, env); !errors.Is(err, ErrWorkspaceExists) {
		t.Fatalf("a second run of the attempt = %v", err)
	}

	if err = os.WriteFile(filepath.Join(dir, "new.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo, changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, tooLarge, err := w.Diff(ctx, patchLimit)
	if err != nil || tooLarge || !strings.Contains(string(patch), "new.go") || !strings.Contains(string(patch), "changed") {
		t.Fatalf("checkpoint patch = %s %v %v", patch, tooLarge, err)
	}
	if _, tooLarge, err = w.Diff(ctx, 10); err != nil || !tooLarge {
		t.Fatalf("a patch over its limit = %v %v", tooLarge, err)
	}
	if staged := run(t, dir, env, "git", "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("the checkpoint left the index changed: %s", staged)
	}

	if _, err = w.Commit(ctx, "work", Lineage{Task: "t_x", ChangeID: "Ix"}); err == nil {
		t.Fatal("a commit without Work")
	}
	cid, err := NewChangeID()
	if err != nil || len(cid) != 41 || cid[0] != 'I' {
		t.Fatalf("change id = %q %v", cid, err)
	}
	commit, err := w.Commit(ctx, "agentrt: the task's work", Lineage{Task: "t_01M4XW5Z7AC7CTA9P0YDGACGPG", ChangeID: cid, Work: "W-31"})
	if err != nil || len(commit) != 40 {
		t.Fatalf("commit = %q %v", commit, err)
	}
	msg := run(t, dir, env, "git", "log", "-1", "--format=%B")
	for _, want := range []string{"Agentrt-Task: t_01M4XW5Z7AC7CTA9P0YDGACGPG", "Change-Id: " + cid, "Work: W-31"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lacks %q:\n%s", want, msg)
		}
	}
	ref := "refs/heads/agent/t_01M4XW5Z7AC7CTA9P0YDGACGPG/a1"
	if head, err := w.Push(ctx, bare, commit, ref); err != nil || head != commit {
		t.Fatalf("push = %q %v", head, err)
	}
	if _, err := w.Push(ctx, bare, commit, "refs/heads/main"); err == nil {
		t.Fatal("pushed to main")
	}
	if main := run(t, t.TempDir(), env, "git", "ls-remote", bare, "refs/heads/main"); !strings.HasPrefix(main, base) {
		t.Fatalf("main moved: %s", main)
	}
}

func TestPrepareRefusesAnUnknownBase(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, _ := remote(t, env)
	if _, err := Prepare(context.Background(), filepath.Join(t.TempDir(), "a1"), bare, strings.Repeat("0", 40), env); err == nil {
		t.Fatal("checked out a commit the remote does not have")
	}
}
