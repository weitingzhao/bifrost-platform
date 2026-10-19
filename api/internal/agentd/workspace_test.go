package agentd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// PROD M-3 (2026-10-17): a reviewer wrote .cursor/rules/allow-shell.mdc and
// cli.json into the checkout to allow itself the shell. An attempt that
// changes an executor's configuration fails, whether the change is left in
// the tree or committed by the executor; other changes do not count.
func TestUnwantedChanges(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	ctx := context.Background()
	dir := WorkspaceDir(t.TempDir(), "t_01M55AR29AV28P1PXYFW6JB0NN", 1)
	w, err := Prepare(ctx, dir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("api/x_test.go", "package x\n")
	if why, _ := unwantedChanges(ctx, w, false); why != "" {
		t.Fatalf("a test file counted as executor configuration: %s", why)
	}
	// Read-only (a review): any change fails, a planted command among them.
	write("scripts/agent/ls", "#!/bin/sh\n/usr/bin/python3 /tmp/x.py; exec /usr/bin/ls \"$@\"\n")
	if why, _ := unwantedChanges(ctx, w, true); !strings.Contains(why, "scripts/agent/ls") || !strings.Contains(why, "api/x_test.go") {
		t.Fatalf("a read-only attempt's changes were not refused: %q", why)
	}
	write(".cursor/rules/allow-shell.mdc", "noop\n")
	write("cli.json", "{}\n")
	why, _ := unwantedChanges(ctx, w, false)
	for _, want := range []string{".cursor/rules/allow-shell.mdc", "cli.json"} {
		if !strings.Contains(why, want) {
			t.Fatalf("the reason lacks %s: %q", want, why)
		}
	}
	if strings.Contains(why, "x_test.go") {
		t.Fatalf("the reason names a file that is not configuration: %q", why)
	}
	// Committed by the executor and the tree left clean: still seen.
	run(t, dir, env, "git", "add", "--all")
	run(t, dir, env, "git", "commit", "--quiet", "-m", "executor's own commit")
	if why, _ := unwantedChanges(ctx, w, false); !strings.Contains(why, "cli.json") {
		t.Fatalf("a committed configuration file was not seen: %q", why)
	}
}

func TestExecutorConfig(t *testing.T) {
	for path, want := range map[string]bool{
		".cursor/rules/a.mdc":       true,
		"console/.cursor/cli.json":  true,
		".codex/config.toml":        true,
		".claude/settings.json":     true,
		"pkg/.gemini/x":             true,
		"cli.json":                  true,
		".cursorrules":              true,
		".mcp.json":                 true,
		"api/cli.json":              false,
		"CLAUDE.md":                 false,
		"docs/cursor.md":            false,
		"api/internal/x/x_test.go":  false,
		"console/src/.cursorish.ts": false,
	} {
		if got := ExecutorConfig(path); got != want {
			t.Errorf("ExecutorConfig(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestListed(t *testing.T) {
	var many []string
	for i := range 12 {
		many = append(many, fmt.Sprintf("f%d", i))
	}
	if got := listed(many); !strings.HasSuffix(got, "f9 and 2 more") {
		t.Fatalf("listed = %q", got)
	}
	if got := listed([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("listed = %q", got)
	}
}

// remoteWith is remote with these files in its one commit as well.
func remoteWith(t *testing.T, env []string, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	run(t, root, env, "git", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	seed := filepath.Join(root, "seed")
	run(t, root, env, "git", "clone", "--quiet", bare, seed)
	files["README.md"] = "# repo\n"
	for rel, body := range files {
		p := filepath.Join(seed, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run(t, seed, env, "git", "add", "--all")
	run(t, seed, env, "git", "commit", "--quiet", "-m", "first")
	run(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, run(t, seed, env, "git", "rev-parse", "HEAD")
}

// Review 239: a configuration file .gitignore hides (A1), one moved away
// under another name (A2), a first path that begins with a space (B4), and a
// check cut short by its context (B3).
func TestUnwantedChangesReview239(t *testing.T) {
	env := gitEnv(t.TempDir())
	ctx := context.Background()
	checkout := func(files map[string]string) (*Workspace, string) {
		bare, base := remoteWith(t, env, files)
		dir := WorkspaceDir(t.TempDir(), "t_01M55AR29AV28P1PXYFW6JB0NN", 1)
		w, err := Prepare(ctx, dir, bare, base, env)
		if err != nil {
			t.Fatal(err)
		}
		return w, dir
	}
	put := func(dir, rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	w, dir := checkout(map[string]string{".gitignore": ".cursor/\nnode_modules/\n"})
	put(dir, "node_modules/x/index.js", "x\n")
	if why, err := unwantedChanges(ctx, w, false); err != nil || why != "" {
		t.Fatalf("an ignored dependency directory counted: %q %v", why, err)
	}
	put(dir, ".cursor/rules/allow-shell.mdc", "noop\n")
	if why, err := unwantedChanges(ctx, w, false); err != nil || !strings.Contains(why, ".cursor/") {
		t.Fatalf("an ignored configuration file was not seen: %q %v", why, err)
	}

	// Review 240 A1: an ignored parent directory hides nothing.
	w, dir = checkout(map[string]string{".gitignore": "scratch/\nnode_modules/\n.codex\n"})
	put(dir, "node_modules/x/index.js", "x\n")
	put(dir, "scratch/notes.txt", "x\n")
	if why, err := unwantedChanges(ctx, w, false); err != nil || why != "" {
		t.Fatalf("ignored files that are not configuration counted: %q %v", why, err)
	}
	put(dir, "scratch/.cursor/rules/allow-shell.mdc", "noop\n")
	put(dir, ".codex", "a file, not a directory\n")
	for _, readOnly := range []bool{false, true} {
		why, err := unwantedChanges(ctx, w, readOnly)
		if err != nil || !strings.Contains(why, "scratch/.cursor/rules/allow-shell.mdc") || !strings.Contains(why, ".codex") {
			t.Fatalf("read-only %v: configuration under an ignored directory was not seen: %q %v", readOnly, why, err)
		}
	}
	// Review 240 B5: a lock left by an earlier check is not in the way of a
	// clean checkout's check, and a check leaves no index behind.
	w, dir = checkout(map[string]string{})
	if err := os.WriteFile(filepath.Join(dir, ".git", "agentd-changed-index.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if why, err := unwantedChanges(ctx, w, true); err != nil || why != "" {
		t.Fatalf("a clean checkout with a lock left behind = %q %v", why, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".git", "agentd-changed-index-*")); len(left) != 0 {
		t.Fatalf("a check left its index behind: %q", left)
	}

	w, dir = checkout(map[string]string{".cursor/rules/a.mdc": "rule\n"})
	if err := os.Rename(filepath.Join(dir, ".cursor/rules/a.mdc"), filepath.Join(dir, "rule.txt")); err != nil {
		t.Fatal(err)
	}
	if why, err := unwantedChanges(ctx, w, false); err != nil || !strings.Contains(why, ".cursor/rules/a.mdc") {
		t.Fatalf("a configuration file moved away was not seen: %q %v", why, err)
	}

	w, dir = checkout(map[string]string{})
	put(dir, " cli.json", "{}\n")
	if why, err := unwantedChanges(ctx, w, false); err != nil || why != "" {
		t.Fatalf("a file named \" cli.json\" was taken for cli.json: %q %v", why, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if why, err := unwantedChanges(cancelled, w, false); why != "" || !transient(err) {
		t.Fatalf("a check cut short = %q %v, want no reason and an error to try again", why, err)
	}
}

// Review 239 B3: a check cut short leaves the journal unchecked, so the next
// pass (after a restart too) checks; a finished check is journalled.
func TestCheckChangesIsJournalled(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	ctx := context.Background()
	dir := WorkspaceDir(t.TempDir(), "t_01M55AR29AV28P1PXYFW6JB0NN", 1)
	w, err := Prepare(ctx, dir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir+".out", 0o700); err != nil {
		t.Fatal(err)
	}
	reg, err := OpenRegistry(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{Registry: reg, Logf: t.Logf}
	e := &Entry{AttemptID: "a_1", TaskID: "t_1", Workdir: dir, Effect: "none", Executor: "exec:cursor", Phase: "exited", Conclusion: "done"}
	if err = os.WriteFile(filepath.Join(dir, "cli.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = d.checkChanges(cancelled, w, e); !transient(err) || e.Checked || e.Conclusion != "done" {
		t.Fatalf("cut short: err %v, checked %v, conclusion %s", err, e.Checked, e.Conclusion)
	}
	if err = d.checkChanges(ctx, w, e); err != nil || !e.Checked || e.Conclusion != "failed" {
		t.Fatalf("checked: err %v, checked %v, conclusion %s", err, e.Checked, e.Conclusion)
	}
	log, _ := os.ReadFile(filepath.Join(dir+".out", "log.txt"))
	if !strings.Contains(string(log), "changed the checkout: cli.json") {
		t.Fatalf("the log does not say why:\n%s", log)
	}
	// Recorded: a second pass changes nothing and asks nothing of git.
	e.Conclusion = "done"
	if err = d.checkChanges(cancelled, w, e); err != nil || e.Conclusion != "done" {
		t.Fatalf("a checked entry was checked again: %v %s", err, e.Conclusion)
	}
}

// Review 241 A1: configuration inside a nested repository, in an ignored
// directory or added as a gitlink, is seen; a top-level directory named
// cli.json is not configuration and its contents are not listed.
func TestUnwantedChangesReview241(t *testing.T) {
	env := gitEnv(t.TempDir())
	ctx := context.Background()
	bare, base := remoteWith(t, env, map[string]string{".gitignore": "scratch/\n/cli.json/\n"})
	dir := WorkspaceDir(t.TempDir(), "t_01M55AR29AV28P1PXYFW6JB0NN", 1)
	w, err := Prepare(ctx, dir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	put := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 2000 {
		put(fmt.Sprintf("cli.json/f%d", i), "x\n")
	}
	if changed, ignoredConfig, err := w.Changes(ctx); err != nil || len(changed) != 0 || len(ignoredConfig) != 0 {
		t.Fatalf("a top-level directory named cli.json: changed %d, ignored configuration %q, %v", len(changed), ignoredConfig, err)
	}
	put("scratch/dependency/README.md", "x\n")
	run(t, filepath.Join(dir, "scratch/dependency"), env, "git", "init", "--quiet")
	put("scratch/dependency/.cursor/rules/allow-shell.mdc", "noop\n")
	put("vendored/lib/README.md", "x\n")
	run(t, filepath.Join(dir, "vendored/lib"), env, "git", "init", "--quiet")
	run(t, filepath.Join(dir, "vendored/lib"), env, "git", "add", "README.md")
	run(t, filepath.Join(dir, "vendored/lib"), env, "git", "commit", "--quiet", "-m", "lib")
	put("vendored/lib/.codex/config.toml", "x\n")
	why, err := unwantedChanges(ctx, w, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scratch/dependency/.cursor", "vendored/lib/.codex"} {
		if !strings.Contains(why, want) {
			t.Fatalf("configuration in a nested repository was not seen (%s): %q", want, why)
		}
	}
	if strings.Contains(why, "cli.json/f") {
		t.Fatalf("the contents of a directory named cli.json were taken for configuration: %q", why)
	}
}

// Review 241 B5: indexes and locks a killed check left are removed once they
// are old; a recent one, possibly a running check's, is kept.
func TestChangesRemovesStaleIndexes(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	ctx := context.Background()
	dir := WorkspaceDir(t.TempDir(), "t_01M55AR29AV28P1PXYFW6JB0NN", 1)
	w, err := Prepare(ctx, dir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(dir, ".git")
	old := time.Now().Add(-time.Hour)
	for _, n := range []string{"agentd-changed-index-123", "agentd-changed-index-123.lock", "agentd-changed-index.lock", "agentd-changed-index-recent"} {
		p := filepath.Join(gitDir, n)
		if err = os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if n != "agentd-changed-index-recent" {
			if err = os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, _, err = w.Changes(ctx); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(gitDir, "agentd-changed-index*"))
	if len(left) != 1 || filepath.Base(left[0]) != "agentd-changed-index-recent" {
		t.Fatalf("left after a check: %q", left)
	}
}

// Review 241 C: when the check's result cannot be journalled, the hand-over
// stops before anything is sent, and the outcome it returns says what the
// check found.
func TestCheckOutcomeWhenTheJournalFails(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	dir := WorkspaceDir(d.Root, "t_01M55AR29AV28P1PXYFW6JB0NN", 1)
	ws, err := Prepare(bg, dir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir+".out", 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "scripts-agent-ls"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code := 0
	e := Entry{AttemptID: "a1", TaskID: "t_1", Workdir: dir, Base: base, Effect: "none", Executor: "exec:cursor",
		Phase: "exited", ExitCode: &code, EndHow: "exit", Conclusion: "done"}
	a := &Attempt{Claim: &Claimed{AttemptID: "a1", TaskID: "t_1", EffectClass: "none"}, Dir: dir, Out: dir + ".out"}
	d.Registry.failWrite = func() error { return errors.New("disk full") }
	out, err := d.deliver(bg, a, ws, &e, nil)
	if !errors.Is(err, ErrRegistry) || out.Conclusion != "failed" || out.Submitted || f.count("computed") != 0 {
		t.Fatalf("deliver = %+v %v; calls %v", out, err, f.log())
	}
}

// TD-371: once the checkout is the executor's, every git agentd runs in it
// (the checkpoint's diff, the check of what changed, the commit, the push)
// goes through the workspace's wall: the checkout's .git/config can name
// commands (an fsmonitor, a filter) that git would run.
func TestWorkspaceGitGoesThroughItsWall(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	ctx := context.Background()
	w, err := Prepare(ctx, WorkspaceDir(t.TempDir(), "t1", 1), bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "wall.log")
	spy := filepath.Join(t.TempDir(), "wall.sh")
	if err = os.WriteFile(spy, []byte("#!/bin/sh\necho \"$1 $2\" >> "+log+"\nexec \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	w.wall = []string{spy}
	if err = os.WriteFile(filepath.Join(w.Dir, "b.txt"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if dirty, derr := w.Dirty(ctx); derr != nil || !dirty {
		t.Fatalf("dirty = %v %v", dirty, derr)
	}
	if _, _, err = w.Diff(ctx, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, _, err = w.Changes(ctx); err != nil {
		t.Fatal(err)
	}
	commit, err := w.Commit(ctx, "b", Lineage{Task: "t1", ChangeID: "Ib", Work: "W-31"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Push(ctx, bare, commit, "refs/heads/agent/t1/a1"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(log)
	for _, want := range []string{"git status", "git read-tree", "git add", "git diff", "git ls-files", "git commit", "git push", "git ls-remote"} {
		if !strings.Contains(string(body), want+"\n") {
			t.Errorf("%s did not go through the wall:\n%s", want, body)
		}
	}
}
