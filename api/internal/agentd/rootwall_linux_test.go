//go:build linux

package agentd

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// listing is every entry under dir, with a digest of each file's content,
// but for what is under skip (an attempt's own checkout and output).
func listing(t *testing.T, dir string, skip ...string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if slices.Contains(skip, p) {
			out = append(out, p+" (the attempt's own)")
			return filepath.SkipDir
		}
		entry := p + " " + d.Type().String()
		if d.Type().IsRegular() {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			sum := sha256.Sum256(b)
			entry += " " + hex.EncodeToString(sum[:8])
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// tries is a shell script that tries each write and says, in results, which
// went through.
func tries(results string, writes map[string]string) string {
	var b strings.Builder
	names := make([]string, 0, len(writes))
	for name := range writes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b.WriteString("if (" + writes[name] + ") 2>/dev/null; then echo 'WROTE " + name + "'; else echo 'denied " + name + "'; fi >> '" + results + "'\n")
	}
	return b.String()
}

// T14 根目录只读 (v5 §5.3; the Owner 2026-10-19 「T14 A」: on the agent host
// the root is agentd's root and the agent user's home, and the wall keeps
// them): a walled attempt, its executor and the code its checks run, tries
// to write agentd's root (a new file, a new task directory, the registry, a
// link), another task's checkout and the agent user's home; every write is
// refused, and when the attempt is over the root and the home list what they
// did before, the attempt's own checkout and output apart. What it may write
// it does: its checkout, and its temporary directory.
func TestAWalledAttemptLeavesTheRootAsItWas(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v): the agent host's probe proves it", wallABI, abi, err)
	}
	home := t.TempDir()
	root := filepath.Join(home, "agent-work", "agentd")
	other := WorkspaceDir(root, "t_01M5AJ2GMV6RX45QVQ2FGNXYXV", 1)
	a := &Attempt{Claim: &Claimed{Role: "verify", Tool: Tool{Executor: "exec:deterministic"}}, Dir: WorkspaceDir(root, "t_01M57YSQN6B43ZFNESD9VQ2HA9", 1)}
	a.Out = a.Dir + ".out"
	for _, d := range []string{a.Dir, a.Out, other, filepath.Join(home, ".config")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{filepath.Join(root, "registry.json"): "{}\n", filepath.Join(other, "report.md"): "# theirs\n",
		filepath.Join(home, ".profile"): "# the agent user's\n"} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writes := func(results string) map[string]string {
		return map[string]string{
			"a file in the root":         "echo x > '" + filepath.Join(root, "planted") + "'",
			"a task directory":           "mkdir '" + filepath.Join(root, "tasks", "t_01M5AJ2GMV6RX45QVQ2FGNXYZZ") + "'",
			"the registry":               "echo x >> '" + filepath.Join(root, "registry.json") + "'",
			"a link in the root":         "ln -s /etc/passwd '" + filepath.Join(root, "link") + "'",
			"another task checkout":      "echo x > '" + filepath.Join(other, "report.md") + "'",
			"a file in the home":         "echo x > '" + filepath.Join(home, "planted") + "'",
			"the home profile":           "echo x >> '" + filepath.Join(home, ".profile") + "'",
			"the home configuration":     "echo x > '" + filepath.Join(home, ".config", "hooks.json") + "'",
			"its checkout":               "echo x > '" + filepath.Join(a.Dir, "inside") + "'",
			"its temporary directory":    "echo x > \"$TMPDIR/inside\"",
			"the parent of the checkout": "echo x > '" + filepath.Join(filepath.Dir(a.Dir), "beside") + "'",
		}
	}
	may := map[string]bool{"its checkout": true, "its temporary directory": true}
	check := func(t *testing.T, results string) {
		t.Helper()
		b, err := os.ReadFile(results)
		if err != nil {
			t.Fatalf("no results: %v", err)
		}
		got := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(got) != len(writes(results)) {
			t.Fatalf("%d results, want %d:\n%s", len(got), len(writes(results)), b)
		}
		for _, line := range got {
			verb, name, _ := strings.Cut(line, " ")
			if want := map[bool]string{true: "WROTE", false: "denied"}[may[name]]; verb != want {
				t.Errorf("%s: %s, want %s", name, verb, want)
			}
		}
	}
	rootBefore, homeBefore := listing(t, root, a.Dir, a.Out), listing(t, home, root)

	// The code a walled check runs: the real deterministic executor, inside
	// the wall, as Run starts it, its commands in their narrower wall.
	a.Walled, a.inner = true, innerWrites(a, nil)
	checks := filepath.Join(a.Dir, "check-results")
	a.Spec = TaskSpec{Text: "t", Verify: [][]string{{"sh", "-c", tries(checks, writes(checks))}}}
	sp, err := Executors(os.Args[0], os.Environ(), Secrets{})["exec:deterministic"](a)
	if err != nil {
		t.Fatal(err)
	}
	argv := append(wallArgv(os.Args[0], []string{a.Dir, a.Out}), sp.Argv...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = sp.Dir, append(sp.Env, "TESTMAIN_AGENTD_LAUNCHER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("the check exited %v:\n%s", err, out)
	}
	check(t, checks)

	// The executor's own process: inside the outer wall only, as cursor's
	// and claude's are.
	own := filepath.Join(a.Dir, "executor-results")
	argv = append(wallArgv(os.Args[0], []string{a.Dir, a.Out}), "sh", "-c", tries(own, writes(own)))
	cmd = exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = a.Dir, sp.Env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the executor's shell: %v\n%s", err, out)
	}
	check(t, own)

	if after := listing(t, root, a.Dir, a.Out); !slices.Equal(after, rootBefore) {
		t.Errorf("the root changed:\nbefore %v\nafter  %v", rootBefore, after)
	}
	if after := listing(t, home, root); !slices.Equal(after, homeBefore) {
		t.Errorf("the home changed:\nbefore %v\nafter  %v", homeBefore, after)
	}
}

// W-31 2.4 「Claude 写核心代码」 on a Linux host: an implementer's claude, as
// its executor builds it and Run walls it, may write its checkout, its home
// (its configuration and its temporary root) and its temporary directory,
// and nothing else: not agentd's root, not another task's checkout, not
// cursor's data directories, not the agent user's home, not /tmp. A program
// named claude stands in for claude here: the wall holds whatever the
// process tree is, and Edit, Write and Bash write as that tree.
func TestClaudeImplementerWritesOnlyItsOwn(t *testing.T) {
	if abi, err := landlockABI(); err != nil || abi < wallABI {
		t.Skipf("no Landlock ABI %d here (%d, %v): the agent host's probe proves it", wallABI, abi, err)
	}
	home := t.TempDir()
	root := filepath.Join(home, "agent-work", "agentd")
	other := WorkspaceDir(root, "t_01M5AJ2GMV6RX45QVQ2FGNXYXV", 1)
	a := &Attempt{Claim: &Claimed{Role: "implement", EffectClass: "branch", Tool: Tool{Executor: "exec:claude", Model: "opus"}},
		Spec: TaskSpec{Text: "# implement it"}, Dir: WorkspaceDir(root, "t_01M57YSQN6B43ZFNESD9VQ2HA9", 1), Walled: true}
	a.Out, a.root = a.Dir+".out", root
	for _, d := range []string{a.Dir, a.Out, other, filepath.Join(root, "s"), filepath.Join(home, ".config")} {
		must(t, os.MkdirAll(d, 0o700))
	}
	for p, body := range map[string]string{filepath.Join(root, "registry.json"): "{}\n", filepath.Join(other, "main.go"): "package theirs\n",
		filepath.Join(home, ".profile"): "# the agent user's\n"} {
		must(t, os.WriteFile(p, []byte(body), 0o600))
	}
	results := filepath.Join(a.Dir, "claude-results")
	writes := map[string]string{
		"its checkout":            "echo x > '" + filepath.Join(a.Dir, "main.go") + "'",
		"its configuration":       "echo x > \"$CLAUDE_CONFIG_DIR/settings.json\"",
		"its temporary root":      "mkdir -p \"$CLAUDE_CODE_TMPDIR/claude-1\" && echo x > \"$CLAUDE_CODE_TMPDIR/claude-1/cwd\"",
		"its temporary directory": "echo x > \"$TMPDIR/inside\"",
		"a file in the root":      "echo x > '" + filepath.Join(root, "planted") + "'",
		"the registry":            "echo x >> '" + filepath.Join(root, "registry.json") + "'",
		"another task checkout":   "echo x >> '" + filepath.Join(other, "main.go") + "'",
		"cursor data directories": "mkdir '" + filepath.Join(root, "s", "planted") + "'",
		"beside the checkout":     "echo x > '" + filepath.Join(filepath.Dir(a.Dir), "beside") + "'",
		"the home profile":        "echo x >> '" + filepath.Join(home, ".profile") + "'",
		"the home configuration":  "echo x > '" + filepath.Join(home, ".config", "hooks.json") + "'",
		"/tmp":                    "echo x > /tmp/claude-planted-$$",
	}
	may := map[string]bool{"its checkout": true, "its configuration": true, "its temporary root": true, "its temporary directory": true}
	bin := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bin, "claude"), []byte(refusals(results, writes)), 0o755))
	rootBefore, homeBefore := listing(t, root, a.Dir, a.Out), listing(t, home, root)

	sp, err := Executors(os.Args[0], []string{"PATH=" + bin + ":/usr/bin:/bin"}, Secrets{ClaudeOAuthToken: "fixture-token"})["exec:claude"](a)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sp.Argv, claudeWriteTools) {
		t.Fatalf("not an implementer's claude: %v", sp.Argv)
	}
	argv := append(wallArgv(os.Args[0], executorWrites("exec:claude", a, nil)), sp.Argv...)
	if err = checkPins(&Wall{ABI: wallABI, Executors: map[string]bool{"exec:claude": true}}, "exec:claude", os.Args[0], a, nil,
		Spawn{Argv: argv, Env: sp.Env, Dir: sp.Dir}); err != nil {
		t.Fatalf("the spawn as Run builds it: %v", err)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = sp.Dir, sp.Env
	if out, cerr := cmd.CombinedOutput(); cerr != nil {
		t.Fatalf("the walled claude: %v\n%s", cerr, out)
	}
	b, err := os.ReadFile(results)
	if err != nil {
		t.Fatalf("no results: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(got) != len(writes) {
		t.Fatalf("%d results, want %d:\n%s", len(got), len(writes), b)
	}
	for _, line := range got {
		verb, name, _ := strings.Cut(line, " ")
		if want := map[bool]string{true: "WROTE", false: "denied"}[may[name]]; verb != want {
			t.Errorf("%s: %s, want %s", name, verb, want)
		}
	}
	if after := listing(t, root, a.Dir, a.Out); !slices.Equal(after, rootBefore) {
		t.Errorf("the root changed:\nbefore %v\nafter  %v", rootBefore, after)
	}
	if after := listing(t, home, root); !slices.Equal(after, homeBefore) {
		t.Errorf("the home changed:\nbefore %v\nafter  %v", homeBefore, after)
	}
}
