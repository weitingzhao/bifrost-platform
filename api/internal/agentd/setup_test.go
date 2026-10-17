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
		"empty setup command": `{"text":"t","setup":[[]]}`,
		"blank setup argv[0]": `{"text":"t","setup":[[""]]}`,
		"a path for a repo":   `{"text":"t","siblings":[{"repo":"../x","ref":"main"}]}`,
		"a slash in a repo":   `{"text":"t","siblings":[{"repo":"a/b","ref":"main"}]}`,
		"an attempt's dir":    `{"text":"t","siblings":[{"repo":"a1","ref":"main"}]}`,
		"a dot repo":          `{"text":"t","siblings":[{"repo":".","ref":"main"}]}`,
		"an option for a ref": `{"text":"t","siblings":[{"repo":"ui","ref":"--upload-pack=x"}]}`,
		"a ref with ..":       `{"text":"t","siblings":[{"repo":"ui","ref":"a..b"}]}`,
		"a ref ending in /":   `{"text":"t","siblings":[{"repo":"ui","ref":"main/"}]}`,
		"a .lock ref":         `{"text":"t","siblings":[{"repo":"ui","ref":"main.lock"}]}`,
		"no ref":              `{"text":"t","siblings":[{"repo":"ui"}]}`,
		"the same repo twice": `{"text":"t","siblings":[{"repo":"ui","ref":"main"},{"repo":"ui","ref":"dev"}]}`,
		"a field it lacks":    `{"text":"t","siblings":[{"repo":"ui","ref":"main","path":"/"}]}`,
		"more than four":      `{"text":"t","siblings":[{"repo":"a","ref":"m"},{"repo":"b","ref":"m"},{"repo":"c","ref":"m"},{"repo":"d","ref":"m"},{"repo":"e","ref":"m"}]}`,
	} {
		if _, err := ParseSpec([]byte(body)); err == nil {
			t.Errorf("%s: accepted %s", name, body)
		}
	}
}

// A sibling is cloned the first time, brought to the branch's new head on a
// later attempt, keeps what was installed in it, and is never fetched with a
// ref or into a directory that is not a plain name.
func TestPrepareSibling(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, first := remote(t, env)
	dir := filepath.Join(t.TempDir(), "tasks", "t_1", "bifrost-ui")
	head, err := PrepareSibling(context.Background(), dir, bare, "main", env)
	if err != nil || head != first {
		t.Fatalf("first = %q %v, want %s", head, err, first)
	}
	// Installed, ignored files stay; the branch moves on.
	run(t, dir, env, "sh", "-c", "mkdir -p node_modules && touch node_modules/kept")
	seed := filepath.Join(t.TempDir(), "seed")
	run(t, filepath.Dir(seed), env, "git", "clone", "--quiet", bare, seed)
	if err = os.WriteFile(filepath.Join(seed, ".gitignore"), []byte("node_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, seed, env, "git", "add", ".gitignore")
	run(t, seed, env, "git", "commit", "--quiet", "-m", "second")
	run(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	second := run(t, seed, env, "git", "rev-parse", "HEAD")
	if head, err = PrepareSibling(context.Background(), dir, bare, "main", env); err != nil || head != second {
		t.Fatalf("again = %q %v, want %s", head, err, second)
	}
	if _, err = os.Stat(filepath.Join(dir, "node_modules", "kept")); err != nil {
		t.Fatalf("what the setup installed went: %v", err)
	}
	if _, err = PrepareSibling(context.Background(), dir, bare, "--upload-pack=touch /tmp/x", env); err == nil {
		t.Fatal("an option for a ref was fetched")
	}
	if _, err = PrepareSibling(context.Background(), filepath.Join(t.TempDir(), "a1"), bare, "main", env); err == nil {
		t.Fatal("an attempt's directory was taken for a sibling")
	}
	if _, err = PrepareSibling(context.Background(), filepath.Join(t.TempDir(), "x"), bare, "no-such-branch", env); err == nil {
		t.Fatal("a branch the remote has not was checked out")
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
