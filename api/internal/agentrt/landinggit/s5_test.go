package landinggit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestS5TrustedPatchReaderLocalGit(t *testing.T) {
	ctx := context.Background()
	w := &Workspace{git: &Git{repo: "bifrost-platform"}, dir: t.TempDir()}
	run := func(args ...string) string {
		t.Helper()
		s, e := w.local(ctx, args...)
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(s)
	}
	run("init", "--template=", ".")
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(w.dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w.dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		run("add", "--", name)
	}
	task := "t_00000000000000000000000001"
	msg := "S5 source\n\nAgentrt-Task: " + task + "\nChange-Id: I" + strings.Repeat("a", 40) + "\n"
	commit := func(message string) string {
		t.Helper()
		_, e := w.git.run(ctx, w.dir, false, []byte(message), []string{"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid"}, "commit", "--no-verify", "-F", "-")
		if e != nil {
			t.Fatal(e)
		}
		return run("rev-parse", "HEAD")
	}
	path := "console/src/lib/patrol/cronSchedule.ts"
	write(path, "export const value = 1\n")
	base := commit("base\n")
	write(path, "export const value = 2\n")
	a := commit(msg)
	source, err := w.S5Patch(ctx, task, base, a, "", "")
	if err != nil || source.ParentA != base || source.SourcePatchSHA256 == "" || source.ReplayMatch {
		t.Fatal(source, err)
	}
	run("checkout", "--detach", base)
	write("README.md", "unrelated main drift\n")
	m0 := commit("main drift\n")
	at := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	r, err := w.Replay(ctx, ReplayInput{A: a, Base: base, M0: m0, Task: task, Attempt: "a_00000000000000000000000001", Mission: "M-1", Operation: "land:" + task + ":1", RuleVersion: 3, AcceptedAt: at, CommitterDate: at.Add(time.Minute), Env: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := w.S5Patch(ctx, task, base, a, m0, r)
	if err != nil || !p.ReplayMatch || p.ParentR != m0 || p.SourceStablePatchID != p.ReplayStablePatchID || p.BaselineTree == "" || p.ResultTree == "" {
		t.Fatal(p, err)
	}
	if _, err := w.S5Patch(ctx, task, base, a, base, r); err == nil {
		t.Fatal("wrong R parent accepted")
	}
	// A valid lineage does not excuse an extra path in the replay.
	write("extra.ts", "export const injected = true\n")
	extra := commit(msg)
	if _, err := w.S5Patch(ctx, task, base, a, r, extra); err == nil {
		t.Fatal("extra replay path accepted")
	}
}
