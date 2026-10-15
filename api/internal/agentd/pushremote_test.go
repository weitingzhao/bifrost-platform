package agentd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Agent branches go to a repository of their own when the one work is
// cloned from is a mirror that takes no push (the Gitea pull mirror of
// GitHub): pushes go there, and a check whose base is a pushed commit finds
// it there.

// agentsRepo is a bare repository that holds what an attempt of task pushed:
// one commit on top of base, on refs/heads/agent/<task>/a1. It returns the
// repository and the commit.
func agentsRepo(t *testing.T, env []string, mirror, task string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "agents.git")
	run(t, root, env, "git", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	work := filepath.Join(root, "work")
	run(t, root, env, "git", "clone", "--quiet", mirror, work)
	if err := os.WriteFile(filepath.Join(work, "fix.txt"), []byte("fix\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, work, env, "git", "add", "fix.txt")
	run(t, work, env, "git", "commit", "--quiet", "-m", "fix")
	commit := run(t, work, env, "git", "rev-parse", "HEAD")
	run(t, work, env, "git", "push", "--quiet", bare, "HEAD:refs/heads/agent/"+task+"/a1")
	return bare, commit
}

func TestPrepareFetchingFindsAPushedCommit(t *testing.T) {
	env := gitEnv(t.TempDir())
	mirror, _ := remote(t, env)
	agents, pushed := agentsRepo(t, env, mirror, "t_A")
	ctx := context.Background()
	if _, err := Prepare(ctx, filepath.Join(t.TempDir(), "a1"), mirror, pushed, env); err == nil {
		t.Fatal("the mirror has the pushed commit: the test proves nothing")
	}
	w, err := PrepareFetching(ctx, filepath.Join(t.TempDir(), "a1"), mirror, pushed, env,
		Fetch{Remote: agents, Refspec: "+refs/heads/agent/t_A/*:refs/remotes/agents/t_A/*"})
	if err != nil {
		t.Fatal(err)
	}
	if head := run(t, w.Dir, env, "git", "rev-parse", "HEAD"); head != pushed {
		t.Fatalf("HEAD = %s, want %s", head, pushed)
	}
	// Only the branches the refspec names: another task's are not fetched.
	if _, err = PrepareFetching(ctx, filepath.Join(t.TempDir(), "a1"), mirror, pushed, env,
		Fetch{Remote: agents, Refspec: "+refs/heads/agent/t_B/*:refs/remotes/agents/t_B/*"}); err == nil {
		t.Fatal("a commit on t_A's branch was found through t_B's refspec")
	}
}

func TestPrepareFetchingLeavesTheOtherRemoteWhenTheBaseIsThere(t *testing.T) {
	env := gitEnv(t.TempDir())
	mirror, base := remote(t, env)
	w, err := PrepareFetching(context.Background(), filepath.Join(t.TempDir(), "a1"), mirror, base, env,
		Fetch{Remote: filepath.Join(t.TempDir(), "no-such.git"), Refspec: "+refs/heads/agent/t_A/*:refs/remotes/agents/t_A/*"})
	if err != nil {
		t.Fatalf("a base the clone has needs no fetch: %v", err)
	}
	if head := run(t, w.Dir, env, "git", "rev-parse", "HEAD"); head != base {
		t.Fatalf("HEAD = %s", head)
	}
}

func TestCheckoutOfACheckFetchesTheCheckedTask(t *testing.T) {
	env := gitEnv(t.TempDir())
	mirror, base := remote(t, env)
	agents, pushed := agentsRepo(t, env, mirror, "t_A")
	d := &Daemon{Remote: func(string) string { return mirror }, PushRemote: func(string) string { return agents }, GitEnv: env}
	check := func(task string) *Attempt {
		return &Attempt{Dir: filepath.Join(t.TempDir(), "a1"), Spec: TaskSpec{Check: "verify", SubjectTask: task}, Subject: &TaskSpec{}}
	}
	ws, err := d.checkout(context.Background(), check("t_A"), "repo", pushed)
	if err != nil {
		t.Fatal(err)
	}
	if head := run(t, ws.Dir, env, "git", "rev-parse", "HEAD"); head != pushed {
		t.Fatalf("HEAD = %s", head)
	}
	// Only the checked task's branches: a check of t_B does not reach t_A's.
	if _, err = d.checkout(context.Background(), check("t_B"), "repo", pushed); err == nil {
		t.Fatal("a check of t_B found a commit on t_A's branch")
	}
	for _, bad := range []string{"", "t_A/*:refs/heads/main", "-t", "t A", "t_A:x"} {
		if _, err = d.checkout(context.Background(), check(bad), "repo", pushed); err == nil || !strings.Contains(err.Error(), "not a task id") {
			t.Fatalf("checked task %q: %v", bad, err)
		}
	}
	// Work that is not a check is cloned as before, from the mirror alone.
	work := &Attempt{Dir: filepath.Join(t.TempDir(), "a1")}
	if _, err = d.checkout(context.Background(), work, "repo", pushed); err == nil {
		t.Fatal("work that is not a check fetched from where branches go")
	}
	if _, err = d.checkout(context.Background(), &Attempt{Dir: filepath.Join(t.TempDir(), "a1")}, "repo", base); err != nil {
		t.Fatal(err)
	}
}

func TestPushGoesWhereBranchesGo(t *testing.T) {
	env := gitEnv(t.TempDir())
	mirror, base := remote(t, env)
	agents := filepath.Join(t.TempDir(), "agents.git")
	run(t, filepath.Dir(agents), env, "git", "init", "--quiet", "--bare", "--initial-branch=main", agents)
	ctx := context.Background()
	ws, err := Prepare(ctx, filepath.Join(t.TempDir(), "a1"), mirror, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(ws.Dir, "fix.txt"), []byte("fix\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit, err := ws.Commit(ctx, "fix", Lineage{Task: "t_A", ChangeID: "Ia", Work: "W-31"})
	if err != nil || commit == "" {
		t.Fatalf("commit = %q %v", commit, err)
	}
	ref := "refs/heads/agent/t_A/a1"
	d := &Daemon{Remote: func(string) string { return mirror }, PushRemote: func(string) string { return agents }, GitEnv: env, Retry: 1}
	if err = d.pushPrepared(ctx, ws, &Entry{Repo: "repo", PushedCommit: commit, PushedRef: ref}); err != nil {
		t.Fatal(err)
	}
	if got := run(t, ws.Dir, env, "git", "ls-remote", agents, ref); !strings.HasPrefix(got, commit) {
		t.Fatalf("agents has %q, want %s", got, commit)
	}
	if got := run(t, ws.Dir, env, "git", "ls-remote", mirror, ref); got != "" {
		t.Fatalf("the mirror got the branch: %q", got)
	}
	// Without a push remote, branches go to the repository work came from.
	d.PushRemote = nil
	ref2 := "refs/heads/agent/t_A/a2"
	if err = d.pushPrepared(ctx, ws, &Entry{Repo: "repo", PushedCommit: commit, PushedRef: ref2}); err != nil {
		t.Fatal(err)
	}
	if got := run(t, ws.Dir, env, "git", "ls-remote", mirror, ref2); !strings.HasPrefix(got, commit) {
		t.Fatalf("mirror has %q", got)
	}
}
