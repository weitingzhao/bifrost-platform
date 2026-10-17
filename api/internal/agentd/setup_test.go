package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Siblings and setup in a spec (CUT-agentd-console-checks-2026-10-16): read
// when well formed, refused when a name could leave the task's directory, a
// ref could be read as an option, or a command is empty.
func TestParseSpecSiblingsAndSetup(t *testing.T) {
	s, err := ParseSpec([]byte(`{"text":"t","verify":[["go","vet"]],"setup":[["npm","ci"]],"siblings":[{"repo":"bifrost-ui","ref":"main"}]}`))
	if err != nil || len(s.Setup) != 1 || s.Setup[0][0] != "npm" || len(s.Siblings) != 1 || s.Siblings[0] != (Sibling{Repo: "bifrost-ui", Ref: "main"}) {
		t.Fatalf("spec = %+v %v", s, err)
	}
	if s, err = ParseSpec([]byte(`{"text":"t","siblings":[{"repo":"ui","ref":"release/1.2"}]}`)); err != nil || s.Siblings[0].Ref != "release/1.2" {
		t.Fatalf("a branch with a slash = %+v %v", s, err)
	}
	for name, body := range map[string]string{
		"empty setup command":          `{"text":"t","setup":[[]]}`,
		"blank setup argv[0]":          `{"text":"t","setup":[[""]]}`,
		"a path for a repo":            `{"text":"t","siblings":[{"repo":"../x","ref":"main"}]}`,
		"a slash in a repo":            `{"text":"t","siblings":[{"repo":"a/b","ref":"main"}]}`,
		"an attempt's dir":             `{"text":"t","siblings":[{"repo":"a1","ref":"main"}]}`,
		"an attempt's dir, upper case": `{"text":"t","siblings":[{"repo":"A1","ref":"main"}]}`,
		"a dot repo":                   `{"text":"t","siblings":[{"repo":".","ref":"main"}]}`,
		"an option for a ref":          `{"text":"t","siblings":[{"repo":"ui","ref":"--upload-pack=x"}]}`,
		"a ref with ..":                `{"text":"t","siblings":[{"repo":"ui","ref":"a..b"}]}`,
		"a ref ending in /":            `{"text":"t","siblings":[{"repo":"ui","ref":"main/"}]}`,
		"a .lock ref":                  `{"text":"t","siblings":[{"repo":"ui","ref":"main.lock"}]}`,
		"no ref":                       `{"text":"t","siblings":[{"repo":"ui"}]}`,
		"the same repo twice":          `{"text":"t","siblings":[{"repo":"ui","ref":"main"},{"repo":"ui","ref":"dev"}]}`,
		"a field it lacks":             `{"text":"t","siblings":[{"repo":"ui","ref":"main","path":"/"}]}`,
		"more than four":               `{"text":"t","siblings":[{"repo":"a","ref":"m"},{"repo":"b","ref":"m"},{"repo":"c","ref":"m"},{"repo":"d","ref":"m"},{"repo":"e","ref":"m"}]}`,
	} {
		if _, err := ParseSpec([]byte(body)); err == nil {
			t.Errorf("%s: accepted %s", name, body)
		}
	}
}

// A sibling is a fresh clone at the branch's head on every attempt: nothing an
// earlier attempt left in it (a hook that would read the Gitea header, another
// origin, installed files) survives into the fetch with the credentials
// (review 227 A1); a symbolic link for it or for the task directory is refused;
// a ref that is an option, an attempt's own directory in any case, and a branch
// the remote has not are refused.
func TestPrepareSibling(t *testing.T) {
	home := t.TempDir()
	env := gitEnv(home)
	creds := append(append([]string{}, env...), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: token fixture-not-a-secret")
	bare, first := remote(t, env)
	task := filepath.Join(t.TempDir(), "tasks", "t_1")
	dir := filepath.Join(task, "bifrost-ui")
	head, err := PrepareSibling(context.Background(), dir, bare, "main", creds)
	if err != nil || head != first {
		t.Fatalf("first = %q %v, want %s", head, err, first)
	}
	if _, err = os.Stat(filepath.Join(dir, ".git", "hooks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the clone has template hooks: %v", err)
	}
	// What an unsandboxed setup could leave: a hook that reads the header, an
	// origin elsewhere, installed files.
	leaked := filepath.Join(t.TempDir(), "leaked")
	other, _ := remote(t, env)
	hooks := filepath.Join(dir, ".git", "hooks")
	if err = os.MkdirAll(hooks, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"post-checkout", "reference-transaction", "post-merge"} {
		if err = os.WriteFile(filepath.Join(hooks, h), []byte("#!/bin/sh\nenv > "+leaked+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, env, "git", "remote", "set-url", "origin", other)
	run(t, dir, env, "sh", "-c", "mkdir -p node_modules && touch node_modules/left")
	seed := filepath.Join(t.TempDir(), "seed")
	run(t, filepath.Dir(seed), env, "git", "clone", "--quiet", bare, seed)
	run(t, seed, env, "git", "commit", "--quiet", "--allow-empty", "-m", "second")
	run(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	second := run(t, seed, env, "git", "rev-parse", "HEAD")
	// The host user's global configuration, which the same setup could write.
	globalHooks := filepath.Join(t.TempDir(), "global-hooks")
	if err = os.MkdirAll(globalHooks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(globalHooks, "post-checkout"), []byte("#!/bin/sh\nenv > "+leaked+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[core]\n\thooksPath = "+globalHooks+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if head, err = PrepareSibling(context.Background(), dir, bare, "main", creds); err != nil || head != second {
		t.Fatalf("again = %q %v, want %s from the remote it was given", head, err, second)
	}
	if b, rerr := os.ReadFile(leaked); rerr == nil {
		t.Fatalf("a hook left by an earlier attempt or in the global configuration ran with the credentials:\n%s", b)
	}
	if err = os.Remove(filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "node_modules", "left")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an earlier attempt's files survived: %v", err)
	}
	if got := run(t, dir, env, "git", "remote", "get-url", "origin"); got != bare {
		t.Fatalf("origin = %s, want %s", got, bare)
	}

	// Symbolic links: the sibling, and the task directory.
	outside := t.TempDir()
	if err = os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareSibling(context.Background(), dir, bare, "main", creds); err == nil {
		t.Fatal("a sibling that is a symbolic link was prepared")
	}
	if _, err = os.Stat(outside); err != nil {
		t.Fatalf("the link's target went: %v", err)
	}
	linked := filepath.Join(t.TempDir(), "t_2")
	if err = os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareSibling(context.Background(), filepath.Join(linked, "bifrost-ui"), bare, "main", creds); err == nil {
		t.Fatal("a sibling in a linked task directory was prepared")
	}

	if _, err = PrepareSibling(context.Background(), filepath.Join(t.TempDir(), "x"), bare, "--upload-pack=touch /tmp/x", env); err == nil {
		t.Fatal("an option for a ref was fetched")
	}
	for _, name := range []string{"a1", "A1", "A12"} {
		if _, err = PrepareSibling(context.Background(), filepath.Join(t.TempDir(), name), bare, "main", env); err == nil {
			t.Fatalf("an attempt's directory (%s) was taken for a sibling", name)
		}
	}
	if _, err = PrepareSibling(context.Background(), filepath.Join(t.TempDir(), "x"), bare, "no-such-branch", env); err == nil {
		t.Fatal("a branch the remote has not was checked out")
	}
}

// The setup sees the allow-listed environment only; the executor it becomes
// gets its own environment back, its vendor key included (review 227 A2).
func TestSetupDoesNotSeeTheExecutorsKeys(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	env := append(launcherEnv(), "CURSOR_API_KEY=fixture-cursor", "CLAUDE_CODE_OAUTH_TOKEN=fixture-claude")
	sp, err := withSetup(os.Args[0], out, [][]string{{"sh", "-c", "env > setup-env.txt"}},
		Spawn{Argv: []string{"sh", "-c", "env > exec-env.txt"}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sp.Argv[0], sp.Argv[1:]...)
	cmd.Dir, cmd.Env = dir, sp.Env
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	setupEnv, _ := os.ReadFile(filepath.Join(dir, "setup-env.txt"))
	execEnv, _ := os.ReadFile(filepath.Join(dir, "exec-env.txt"))
	for _, key := range []string{"CURSOR_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if strings.Contains(string(setupEnv), key) {
			t.Errorf("the setup saw %s", key)
		}
		if !strings.Contains(string(execEnv), key) {
			t.Errorf("the executor lost %s", key)
		}
	}
	if !strings.Contains(string(setupEnv), "PATH=") {
		t.Errorf("the setup has no PATH:\n%s", setupEnv)
	}
}

// agentd setup runs the commands, records them, and only then becomes the
// executor; a failing command ends it with 3 and the executor never starts.
func TestRunSetup(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	marker := filepath.Join(out, "executor-ran")
	setup := func(cmds [][]string) []string {
		sp, err := withSetup(os.Args[0], out, cmds, Spawn{Argv: []string{"sh", "-c", "touch " + marker}})
		if err != nil {
			t.Fatal(err)
		}
		return sp.Argv
	}
	read := func() Verification {
		var v Verification
		body, err := os.ReadFile(filepath.Join(out, "setup.json"))
		if err != nil || json.Unmarshal(body, &v) != nil {
			t.Fatalf("setup.json: %v %s", err, body)
		}
		return v
	}

	argv := setup([][]string{{"sh", "-c", "echo installed > installed.txt"}})
	if argv[0] != os.Args[0] || argv[1] != SetupArg || !strings.Contains(strings.Join(argv, " "), "-- sh -c touch "+marker) {
		t.Fatalf("argv = %q", argv)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dir, launcherEnv()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup that passes: %v\n%s", err, b)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the executor did not run after the setup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "installed.txt")); err != nil {
		t.Fatalf("the setup did not run in the checkout: %v", err)
	}
	if v := read(); !v.Pass || len(v.Commands) != 1 {
		t.Fatalf("record = %+v", v)
	}

	_ = os.Remove(marker)
	argv = setup([][]string{{"sh", "-c", "echo registry down >&2; exit 7"}, {"sh", "-c", "touch never"}})
	cmd = exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dir, launcherEnv()
	b, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != setupFailed || !strings.Contains(string(b), "the executor did not start") || !strings.Contains(string(b), "registry down") {
		t.Fatalf("setup that fails: %v\n%s", err, b)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the executor ran after a failed setup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "never")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a command after the failing one ran")
	}
	if v := read(); v.Pass || len(v.Commands) != 1 || v.Commands[0].ExitCode != 7 {
		t.Fatalf("record = %+v", v)
	}
}

// A check prepares what its subject needs: the subject spec's siblings and
// setup, not its own (it has none).
func TestACheckPreparesItsSubject(t *testing.T) {
	own := TaskSpec{Text: "check", Check: "verify"}
	subject := TaskSpec{Text: "t", Setup: [][]string{{"npm", "ci"}}, Siblings: []Sibling{{Repo: "bifrost-ui", Ref: "main"}}}
	if p := (&Attempt{Spec: own, Subject: &subject}).prepare(); len(p.Setup) != 1 || len(p.Siblings) != 1 {
		t.Fatalf("a check prepares %+v", p)
	}
	if p := (&Attempt{Spec: subject}).prepare(); len(p.Setup) != 1 {
		t.Fatalf("a task prepares %+v", p)
	}
}
