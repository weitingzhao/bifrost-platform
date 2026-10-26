package landinggit

import (
	"context"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testTask = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Tester", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Tester", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git fixture: %v: %s", e, b)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (*Git, ReplayInput, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "remote.git")
	gitTest(t, root, "init", "--bare", repo)
	gitTest(t, repo, "config", "http.receivepack", "true")
	src := filepath.Join(root, "source")
	gitTest(t, root, "init", "-b", "main", src)
	put := func(name, content string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(src, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	put("display.txt", "old\n")
	gitTest(t, src, "add", "display.txt")
	gitTest(t, src, "commit", "-m", "base")
	base := gitTest(t, src, "rev-parse", "HEAD")
	put("display.txt", "new\n")
	gitTest(t, src, "add", "display.txt")
	gitTest(t, src, "commit", "-m", "copy\n\nWork: W-570\nAgentrt-Task: "+testTask+"\nChange-Id: I"+strings.Repeat("a", 40))
	result := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "branch", "accepted")
	gitTest(t, src, "reset", "--hard", base)
	put("unrelated.txt", "main advanced\n")
	gitTest(t, src, "add", "unrelated.txt")
	gitTest(t, src, "commit", "-m", "main moves")
	main := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", repo, "main", "accepted")
	execPath := gitTest(t, src, "--exec-path")
	server := httptest.NewServer(&cgi.Handler{Path: filepath.Join(execPath, "git-http-backend"), Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}})
	t.Cleanup(server.Close)
	u, p := filepath.Join(root, "username"), filepath.Join(root, "password")
	if e := os.WriteFile(u, []byte("drill-fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte("fixture-only-password"), 0600); e != nil {
		t.Fatal(e)
	}
	g := &Git{remote: server.URL + "/remote.git", usernameFile: u, passwordFile: p}
	in := ReplayInput{A: result, Base: base, M0: main, Task: testTask, Attempt: "a_fixture", Mission: "M-1", Reviewers: "review-attempt", Operation: "land:" + testTask + ":1", RuleVersion: 1, AcceptedAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), CommitterDate: time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)}
	return g, in, src
}
func TestReplayRecoverPushedBranchAndConditionalDelete(t *testing.T) {
	g, in, _ := fixture(t)
	ctx := context.Background()
	w, e := g.Open(ctx, in.A)
	if e != nil {
		t.Fatal(e)
	}
	r, e := w.Replay(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := os.ReadFile(filepath.Join(w.dir, ".git", "config"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(cfg), "fixture-only-password") || strings.Contains(string(cfg), "extraHeader") {
		t.Fatal("credential persisted")
	}
	ref := "refs/heads/land/stg/" + testTask + "/1"
	if e = w.Push(ctx, ref, r); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	} // crash: no branch_pushed note and no work dir
	recovered, e := g.Open(ctx, in.A)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	again, e := recovered.Replay(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if r != again {
		t.Fatalf("SHA changed after recovery: %s %s", r, again)
	}
	observed, e := g.Ref(ctx, ref)
	if e != nil || observed != r {
		t.Fatal(observed, e)
	}
	// Recovery observes R and needs no second push.
	if e = g.Delete(ctx, ref, in.A); e == nil {
		t.Fatal("delete ignored SHA fence")
	}
	if observed, e = g.Ref(ctx, ref); e != nil || observed != r {
		t.Fatal("foreign SHA deleted", observed, e)
	}
	if e = g.Delete(ctx, ref, r); e != nil {
		t.Fatal(e)
	}
	if observed, e = g.Ref(ctx, ref); e != nil || observed != "" {
		t.Fatal(observed, e)
	}
	if e = g.Delete(ctx, ref, r); e != nil {
		t.Fatal("delete recovery is not idempotent", e)
	}
	if e = recovered.Push(ctx, "refs/heads/main", r); e == nil {
		t.Fatal("main writable")
	}
	if e = recovered.Push(ctx, "refs/tags/v1", r); e == nil {
		t.Fatal("tag writable")
	}
	if e = g.Delete(ctx, "refs/heads/main", r); e == nil {
		t.Fatal("main deletable")
	}
}
func TestReplayRejectsOverlap(t *testing.T) {
	g, in, src := fixture(t)
	if e := os.WriteFile(filepath.Join(src, "display.txt"), []byte("new main copy\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitTest(t, src, "add", "display.txt")
	gitTest(t, src, "commit", "-m", "overlapping main")
	// Fixture pushes only into the throwaway bare repository.
	repo := filepath.Join(filepath.Dir(src), "remote.git")
	gitTest(t, src, "push", repo, "main")
	in.M0 = gitTest(t, src, "rev-parse", "HEAD")
	w, e := g.Open(context.Background(), in.A)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if _, e = w.Replay(context.Background(), in); !errors.Is(e, ErrOverlap) {
		t.Fatal("overlap not refused", e)
	}
}
func TestGitEnvironmentAndCredentialScope(t *testing.T) {
	g, in, _ := fixture(t)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.sshCommand")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	t.Setenv("GIT_TRACE", "1")
	t.Setenv("GIT_CONFIG_SYSTEM", "/untrusted")
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", "/untrusted")
	for _, v := range isolatedEnv() {
		if strings.Contains(v, "/untrusted") || strings.HasPrefix(v, "GIT_TRACE=") || strings.HasPrefix(v, "GIT_CONFIG_COUNT=") {
			t.Fatal("inherited environment", v)
		}
	}
	w, e := g.Open(context.Background(), in.A)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	// Installing a hook in even this trusted local config cannot enable it.
	sentinel := filepath.Join(t.TempDir(), "hook-ran")
	hook := "#!/bin/sh\ntouch '" + sentinel + "'\n"
	if e = os.WriteFile(filepath.Join(w.dir, ".git", "hooks", "post-checkout"), []byte(hook), 0700); !os.IsNotExist(e) && e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(w.dir, ".git", "hooks"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(w.dir, ".git", "hooks", "post-checkout"), []byte(hook), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = w.Replay(context.Background(), in); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatal("hook executed")
	}
	t.Setenv(UsernameFileEnv, g.usernameFile)
	t.Setenv(PasswordFileEnv, g.passwordFile)
	if _, e = FromEnv("prod"); e == nil {
		t.Fatal("production configured drill")
	}
	configured, e := FromEnv("stg")
	if e != nil || configured.remote != remoteURL {
		t.Fatal(configured, e)
	}
}

func TestTrustedDiffAndFullHistory(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	w, e := g.Open(ctx, in.A)
	if e != nil {
		t.Fatal(e)
	}
	single, changes, e := w.Result(ctx, in.Task, in.Base, in.A)
	if e != nil || !single || len(changes) != 1 || changes[0].Added != 1 || changes[0].Deleted != 1 || changes[0].AddedText != "new\n" {
		t.Fatal(single, changes, e)
	}
	h, e := w.History(ctx, in.Task, in.A, "")
	if e != nil || h.State != landing.Never {
		t.Fatal(h, e)
	}
	w.Close()
	// Land into the local fixture, then revert. Both operations are fixture
	// setup, never an external repository or a deployed controller.
	gitTest(t, src, "cherry-pick", in.A)
	landed := gitTest(t, src, "rev-parse", "HEAD")
	repo := filepath.Join(filepath.Dir(src), "remote.git")
	gitTest(t, src, "push", repo, "main")
	w, e = g.Open(ctx, in.A)
	if e != nil {
		t.Fatal(e)
	}
	h, e = w.History(ctx, in.Task, in.A, "")
	if e != nil || h.State != landing.Effective || h.Commit != landed {
		t.Fatal(h, e)
	}
	w.Close()
	gitTest(t, src, "revert", "--no-edit", landed)
	revert := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", repo, "main")
	w, e = g.Open(ctx, in.A)
	if e != nil {
		t.Fatal(e)
	}
	h, e = w.History(ctx, in.Task, in.A, "")
	if e != nil || h.State != landing.Reverted {
		t.Fatal(h, e)
	}
	w.Close()
	gitTest(t, src, "revert", "--no-edit", revert)
	gitTest(t, src, "push", repo, "main")
	w, e = g.Open(ctx, in.A)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	h, e = w.History(ctx, in.Task, in.A, "")
	if e != nil || h.State != landing.Effective {
		t.Fatal(h, e)
	}
	if _, e = w.History(ctx, in.Task, strings.Repeat("f", 40), ""); e == nil {
		t.Fatal("unknown history became never")
	}
}
