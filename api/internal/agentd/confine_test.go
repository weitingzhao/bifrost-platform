package agentd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// pinnedExecutors are the executors whose builders pin them (executors.go).
var pinnedExecutors = []string{"exec:deterministic", "exec:codex", "exec:cursor", "exec:claude"}

// hostEnv is an agentd environment whose home, codex home, temporary
// directory and Go caches are the host user's: none may reach an executor.
var hostEnv = []string{"PATH=/bin", "HOME=/host/home", "CODEX_HOME=/host/codex", "TMPDIR=/host/tmp", "GOPATH=/host/go",
	"GOMODCACHE=/host/go/pkg/mod", "GOCACHE=/host/cache"}

// TD-371 ratchet: each executor's environment and argv carry the pins, with
// the wall and without.
func TestExecutorPins(t *testing.T) {
	for _, walled := range []bool{false, true} {
		for _, name := range pinnedExecutors {
			t.Run(name+"/walled="+strconv.FormatBool(walled), func(t *testing.T) {
				out := t.TempDir()
				a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: t.TempDir(), Out: out, Walled: walled}
				sp, err := Executors("/opt/agentd", hostEnv, Secrets{})[name](a)
				if err != nil {
					t.Fatal(err)
				}
				home := filepath.Join(out, "home")
				want := map[string]string{"HOME": home, "TMPDIR": filepath.Join(out, "tmp"), "GOPATH": filepath.Join(home, "go"),
					"GOMODCACHE": filepath.Join(home, "go", "pkg", "mod"), "GOCACHE": filepath.Join(home, ".cache", "go-build")}
				switch name {
				case "exec:codex":
					want["CODEX_HOME"] = filepath.Join(home, ".codex")
				case "exec:claude":
					want["CLAUDE_CONFIG_DIR"] = filepath.Join(home, ".claude")
					want["DISABLE_AUTOUPDATER"] = "1"
				}
				for _, k := range []string{"HOME", "TMPDIR", "GOPATH", "GOMODCACHE", "GOCACHE", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "DISABLE_AUTOUPDATER"} {
					got := envValues(sp.Env, k)
					if v, ok := want[k]; ok && !slices.Equal(got, []string{v}) || !ok && len(got) > 0 {
						t.Errorf("%s = %q, want %q", k, got, want[k])
					}
					if v, ok := want[k]; ok && k != "DISABLE_AUTOUPDATER" {
						if fi, serr := os.Lstat(v); k != "GOPATH" && k != "GOMODCACHE" && k != "GOCACHE" && (serr != nil || !fi.IsDir()) {
							t.Errorf("%s %s was not made: %v", k, v, serr)
						}
					}
				}
				if slices.ContainsFunc(sp.Env, func(kv string) bool { return strings.Contains(kv, "/host/") && !strings.HasPrefix(kv, "PATH=") }) {
					t.Errorf("a host directory reached %s: %v", name, sp.Env)
				}
				switch name {
				case "exec:codex":
					for _, pin := range []string{"features.hooks=false", `approval_policy="never"`, `model_provider="openai"`,
						"sandbox_workspace_write.writable_roots=[" + strconv.Quote(filepath.Join(out, "tmp")) + "]",
						"sandbox_workspace_write.exclude_slash_tmp=true", "sandbox_workspace_write.exclude_tmpdir_env_var=true", "notify=[]"} {
						if !hasRun(sp.Argv, []string{"-c", pin}) {
							t.Errorf("codex lacks -c %s: %v", pin, sp.Argv)
						}
					}
					if !slices.Contains(sp.Argv, "--ignore-rules") {
						t.Errorf("codex reads saved rules: %v", sp.Argv)
					}
					if got := hasRun(sp.Argv, []string{"-c", "features.use_legacy_landlock=true"}); got != walled {
						t.Errorf("codex's own Landlock sandbox = %v, walled %v", got, walled)
					}
				case "exec:claude":
					for _, pin := range [][]string{{"--setting-sources", ""}, {"--settings", `{"disableAllHooks":true}`}, {"--strict-mcp-config"},
						{"--mcp-config", `{"mcpServers":{}}`}, {"--tools", "Read,Grep,Glob"}} {
						if !hasRun(sp.Argv, pin) {
							t.Errorf("claude lacks %q: %v", pin, sp.Argv)
						}
					}
				}
			})
		}
	}
}

// Run's spawn for a walled attempt passes checkPins; each pin taken away
// is refused.
func TestCheckPins(t *testing.T) {
	for _, name := range pinnedExecutors {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: t.TempDir(), Out: out, Walled: true}
			writes := []string{a.Dir, a.Out, filepath.Join(filepath.Dir(a.Dir), "bifrost-ui")}
			sp, err := Executors("/opt/agentd", hostEnv, Secrets{})[name](a)
			if err != nil {
				t.Fatal(err)
			}
			sp, err = withSetup("/opt/agentd", a.Out, [][]string{{"npm", "ci"}}, sp)
			if err != nil {
				t.Fatal(err)
			}
			sp.Argv = append(wallArgv("/opt/agentd", writes), sp.Argv...)
			if err = checkPins(name, "/opt/agentd", a, writes, sp); err != nil {
				t.Fatalf("the spawn as Run builds it: %v", err)
			}
			bad := map[string]func(Spawn) Spawn{
				"no wall": func(s Spawn) Spawn { s.Argv = s.Argv[len(wallArgv("/opt/agentd", writes)):]; return s },
				"one more directory": func(s Spawn) Spawn {
					s.Argv = append(wallArgv("/opt/agentd", append(writes, "/home/u")), s.Argv[len(wallArgv("/opt/agentd", writes)):]...)
					return s
				},
				"another agentd":        func(s Spawn) Spawn { s.Argv = append([]string{"/tmp/agentd"}, s.Argv[1:]...); return s },
				"HOME elsewhere":        func(s Spawn) Spawn { s.Env = withEnv(s.Env, "HOME", "/home/u"); return s },
				"HOME twice":            func(s Spawn) Spawn { s.Env = append(slices.Clone(s.Env), "HOME=/home/u"); return s },
				"no TMPDIR":             func(s Spawn) Spawn { s.Env = withoutEnv(s.Env, "TMPDIR"); return s },
				"the host's Go cache":   func(s Spawn) Spawn { s.Env = withEnv(s.Env, "GOCACHE", "/home/u/.cache/go-build"); return s },
				"a CLAUDE_CONFIG_DIR":   func(s Spawn) Spawn { s.Env = withEnv(s.Env, "CLAUDE_CONFIG_DIR", "/home/u/.claude"); return s },
				"the host's CODEX_HOME": func(s Spawn) Spawn { s.Env = withEnv(s.Env, "CODEX_HOME", "/home/u/.codex"); return s },
			}
			if name == "exec:claude" {
				bad["no --setting-sources"] = func(s Spawn) Spawn { s.Argv = dropRun(s.Argv, []string{"--setting-sources", ""}); return s }
				bad["hooks on"] = func(s Spawn) Spawn {
					s.Argv = dropRun(s.Argv, []string{"--settings", `{"disableAllHooks":true}`})
					return s
				}
			}
			if name == "exec:codex" {
				bad["bubblewrap inside the wall"] = func(s Spawn) Spawn { s.Argv = dropRun(s.Argv, []string{"-c", codexWalled}); return s }
				bad["hooks on"] = func(s Spawn) Spawn { s.Argv = dropRun(s.Argv, []string{"-c", "features.hooks=false"}); return s }
				bad["saved rules"] = func(s Spawn) Spawn { s.Argv = dropRun(s.Argv, []string{"--ignore-rules"}); return s }
			}
			for what, mutate := range bad {
				cp := sp
				cp.Argv, cp.Env = slices.Clone(sp.Argv), slices.Clone(sp.Env)
				if checkPins(name, "/opt/agentd", a, writes, mutate(cp)) == nil {
					t.Errorf("%s: passed", what)
				}
			}
		})
	}
	if checkPins("exec:other", "/opt/agentd", &Attempt{Out: "/x"}, nil, Spawn{Argv: []string{"x"}}) == nil {
		t.Fatal("an executor with no pins passed")
	}
}

func dropRun(argv, run []string) []string {
	for i := 0; i+len(run) <= len(argv); i++ {
		if slices.Equal(argv[i:i+len(run)], run) {
			return append(slices.Clone(argv[:i]), argv[i+len(run):]...)
		}
	}
	return argv
}

func TestCursorRefusesRepoHooks(t *testing.T) {
	for _, c := range []struct {
		files   []string
		link    bool
		refused bool
	}{
		{nil, false, false},
		{[]string{".cursor/rules/x.mdc"}, false, false},
		{[]string{".cursor/hooks.json"}, false, true},
		{[]string{".cursor/mcp.json"}, false, true},
		{[]string{"hooks.json"}, true, true}, // .cursor is a link to a directory that has one
	} {
		dir, other := t.TempDir(), t.TempDir()
		base := dir
		if c.link {
			base = other
			if err := os.Symlink(other, filepath.Join(dir, ".cursor")); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range c.files {
			must(t, os.MkdirAll(filepath.Dir(filepath.Join(base, f)), 0o700))
			must(t, os.WriteFile(filepath.Join(base, f), []byte("{}"), 0o600))
		}
		a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: dir, Out: t.TempDir()}
		_, err := cursorExecutor(nil)(a)
		if (err != nil) != c.refused {
			t.Errorf("%v link=%v: err = %v, want refused %v", c.files, c.link, err, c.refused)
		}
	}
}

// What is gone after an executor: its Go and npm caches and temporary
// files, through links too only inside the output; its codex home stays.
func TestAfterExecutor(t *testing.T) {
	out, elsewhere := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(elsewhere, "keep"), []byte("x"), 0o600))
	mod := filepath.Join(out, "home", "go", "pkg", "mod", "m@v1")
	must(t, os.MkdirAll(mod, 0o700))
	must(t, os.WriteFile(filepath.Join(mod, "a.go"), []byte("package m"), 0o400))
	must(t, os.Chmod(mod, 0o500))
	must(t, os.MkdirAll(filepath.Join(out, "home", ".codex", "sessions"), 0o700))
	must(t, os.MkdirAll(filepath.Join(out, "tmp"), 0o700))
	must(t, os.Symlink(elsewhere, filepath.Join(out, "tmp", "away")))
	must(t, os.Symlink(elsewhere, filepath.Join(out, "home", ".cache")))
	afterExecutor("exec:cursor", "", out, t.Logf)
	for _, gone := range []string{"home/go", "home/.cache", "tmp"} {
		if _, err := os.Lstat(filepath.Join(out, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "home", ".codex", "sessions")); err != nil {
		t.Errorf("codex's sessions went: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(elsewhere, "keep")); err != nil || string(b) != "x" {
		t.Fatalf("a link led the removal out: %q %v", b, err)
	}
	afterExecutor("exec:cursor", "", filepath.Join(out, "none"), t.Logf) // no output: nothing to do
}

func TestRunConfineRefuses(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	must(t, os.Symlink(dir, link))
	mark := filepath.Join(dir, "ran")
	for args, want := range map[string]int{
		"-- touch " + mark:                       2,
		"--write " + dir + " touch " + mark:      2,
		"--write " + dir + " --":                 2,
		"--write rel -- touch " + mark:           126,
		"--write " + link + " -- touch " + mark:  126,
		"--write " + mark + "x -- touch " + mark: 126,
	} {
		if got := RunConfine(strings.Fields(args)); got != want {
			t.Errorf("confine %s = %d, want %d", args, got, want)
		}
	}
	if runtime.GOOS != "linux" {
		if got := RunConfine([]string{"--write", dir, "--", "touch", mark}); got != 126 {
			t.Errorf("without Landlock confine = %d, want 126", got)
		}
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("the command ran")
	}
}

// The checks find a wall that holds nothing: run outside any wall, they
// report the writes that went through, and exit 1.
func TestConfineCheckFindsNoWall(t *testing.T) {
	in, outside := t.TempDir(), t.TempDir()
	file := filepath.Join(outside, "file")
	must(t, os.WriteFile(file, []byte("before\n"), 0o600))
	cmd := exec.Command(os.Args[0], CheckArg, "--in", in, "--outside", outside, "--file", file, "--pid", strconv.Itoa(os.Getpid()))
	cmd.Env = launcherEnv()
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("exit %v:\n%s", err, out)
	}
	for _, want := range []string{"ok: write inside", "FAIL: make a file outside was allowed", "FAIL: append to a file outside was allowed",
		"FAIL: truncate a file outside was allowed", "FAIL: rename out of the wall was allowed", "ok: write /dev/null"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
}

// Without Landlock (macOS) there is no wall, and nothing is walled.
func TestProveWallWithoutLandlock(t *testing.T) {
	if _, err := landlockABI(); err == nil {
		t.Skip("this kernel has Landlock: TestWallHolds proves it")
	}
	w, lines := ProveWall(t.Context(), os.Args[0], t.TempDir(), launcherEnv(), true)
	if w != nil || len(lines) == 0 {
		t.Fatalf("wall %+v, %q", w, lines)
	}
	for _, name := range pinnedExecutors {
		if w.holds(name) {
			t.Errorf("%s is walled", name)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
