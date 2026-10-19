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
	"time"
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
				home, tmp := filepath.Join(out, "home"), filepath.Join(out, "tmp")
				want := map[string]string{"HOME": home, "TMPDIR": tmp, "GOPATH": filepath.Join(tmp, "go"),
					"GOMODCACHE": filepath.Join(tmp, "go", "pkg", "mod"), "GOCACHE": filepath.Join(tmp, "go-build")}
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
						if !hasRunT(sp.Argv, []string{"-c", pin}) {
							t.Errorf("codex lacks -c %s: %v", pin, sp.Argv)
						}
					}
					if !slices.Contains(sp.Argv, "--ignore-rules") {
						t.Errorf("codex reads saved rules: %v", sp.Argv)
					}
					if got := hasRunT(sp.Argv, []string{"-c", "features.use_legacy_landlock=true"}); got != walled {
						t.Errorf("codex's own Landlock sandbox = %v, walled %v", got, walled)
					}
				case "exec:claude":
					for _, pin := range [][]string{{"--setting-sources", ""}, {"--settings", `{"disableAllHooks":true}`}, {"--strict-mcp-config"},
						{"--mcp-config", `{"mcpServers":{}}`}, {"--tools", "Read,Grep,Glob"}} {
						if !hasRunT(sp.Argv, pin) {
							t.Errorf("claude lacks %q: %v", pin, sp.Argv)
						}
					}
				}
			})
		}
	}
}

// Run's spawn for a walled attempt passes checkPins; each pin taken away,
// repeated or overridden is refused (review 300 A2, A3).
func TestCheckPins(t *testing.T) {
	for _, c := range []struct {
		name, effect, model string
	}{
		{"exec:deterministic", "none", ""}, {"exec:codex", "branch", "gpt-5"}, {"exec:codex", "none", ""},
		{"exec:cursor", "none", ""}, {"exec:cursor", "branch", "auto"}, {"exec:claude", "none", "opus"},
	} {
		t.Run(c.name+"/"+c.effect, func(t *testing.T) {
			const self = "/opt/agentd"
			a := &Attempt{Claim: &Claimed{EffectClass: c.effect, Tool: Tool{Model: c.model}}, Spec: TaskSpec{Text: "# do it"},
				Dir: t.TempDir(), Out: t.TempDir(), Walled: true}
			sibs := []string{filepath.Join(filepath.Dir(a.Dir), "bifrost-ui")}
			a.inner = innerWrites(a, sibs)
			sp, err := Executors(self, hostEnv, Secrets{CursorAPIKey: "fixture-key", ClaudeOAuthToken: "fixture-token"})[c.name](a)
			if err != nil {
				t.Fatal(err)
			}
			o := setupOpts{Home: setupHome(a.Out), Writes: a.inner}
			if c.name == "exec:cursor" {
				o.Refuse = cursorRefused
			}
			withS, err := withSetup(self, a.Out, [][]string{{"npm", "ci"}}, sp, o)
			if err != nil {
				t.Fatal(err)
			}
			wall := wallArgv(self, append([]string{a.Dir, a.Out}, sibs...))
			for _, built := range []Spawn{sp, withS} {
				built.Argv = append(slices.Clone(wall), built.Argv...)
				if err = checkPins(c.name, self, a, sibs, built); err != nil {
					t.Fatalf("the spawn as Run builds it: %v", err)
				}
			}
			sp = withS
			sp.Argv = append(slices.Clone(wall), sp.Argv...)
			n := len(wall)
			replace := func(old, new []string) func(Spawn) Spawn {
				return func(s Spawn) Spawn {
					for i := 0; i+len(old) <= len(s.Argv); i++ {
						if slices.Equal(s.Argv[i:i+len(old)], old) {
							s.Argv = append(append(slices.Clone(s.Argv[:i]), new...), s.Argv[i+len(old):]...)
							return s
						}
					}
					t.Fatalf("no %q in %q", old, s.Argv)
					return s
				}
			}
			beforePrompt := func(extra ...string) func(Spawn) Spawn {
				return func(s Spawn) Spawn {
					k := len(s.Argv) - 1
					if c.name == "exec:claude" {
						k-- // "--" stays right before the prompt
					}
					if c.name == "exec:deterministic" {
						k++
					}
					s.Argv = append(append(slices.Clone(s.Argv[:k]), extra...), s.Argv[k:]...)
					return s
				}
			}
			bad := map[string]func(Spawn) Spawn{
				"no wall": func(s Spawn) Spawn { s.Argv = s.Argv[n:]; return s },
				"one more directory": func(s Spawn) Spawn {
					s.Argv = append(wallArgv(self, append([]string{a.Dir, a.Out, sibs[0]}, "/home/u")), s.Argv[n:]...)
					return s
				},
				"another agentd":        func(s Spawn) Spawn { s.Argv = append([]string{"/tmp/agentd"}, s.Argv[1:]...); return s },
				"setup unwalled":        replace([]string{"--write", a.Dir, "--write", sibs[0], "--write", attemptTmp(a.Out)}, nil),
				"setup in the home":     replace([]string{"--home", setupHome(a.Out)}, []string{"--home", attemptHome(a.Out)}),
				"setup writing output":  replace([]string{"--write", attemptTmp(a.Out)}, []string{"--write", a.Out}),
				"HOME elsewhere":        func(s Spawn) Spawn { s.Env = withEnv(s.Env, "HOME", "/home/u"); return s },
				"HOME twice":            func(s Spawn) Spawn { s.Env = append(slices.Clone(s.Env), "HOME="+attemptHome(a.Out)); return s },
				"no TMPDIR":             func(s Spawn) Spawn { s.Env = withoutEnv(s.Env, "TMPDIR"); return s },
				"the host's Go cache":   func(s Spawn) Spawn { s.Env = withEnv(s.Env, "GOCACHE", "/home/u/.cache/go-build"); return s },
				"XDG_CONFIG_HOME":       func(s Spawn) Spawn { s.Env = append(slices.Clone(s.Env), "XDG_CONFIG_HOME=/home/u/.config"); return s },
				"the host's CODEX_HOME": func(s Spawn) Spawn { s.Env = withEnv(s.Env, "CODEX_HOME", "/home/u/.codex"); return s },
				"a flag more":           beforePrompt("--verbose"),
			}
			if c.name != "exec:claude" {
				bad["a CLAUDE_CONFIG_DIR"] = func(s Spawn) Spawn { s.Env = withEnv(s.Env, "CLAUDE_CONFIG_DIR", "/home/u/.claude"); return s }
			}
			switch c.name {
			case "exec:claude":
				bad["no --setting-sources"] = replace([]string{"--setting-sources", ""}, nil)
				bad["hooks on"] = replace([]string{"--settings", `{"disableAllHooks":true}`}, nil)
				bad["updater on"] = func(s Spawn) Spawn { s.Env = withoutEnv(s.Env, "DISABLE_AUTOUPDATER"); return s }
				bad["a tool more"] = replace([]string{"--tools", "Read,Grep,Glob"}, []string{"--tools", "Read,Grep,Glob,Bash"})
			case "exec:codex":
				bad["bubblewrap inside the wall"] = replace([]string{"-c", codexWalled}, nil)
				bad["hooks on"] = replace([]string{"-c", "features.hooks=false"}, nil)
				bad["hooks on again"] = beforePrompt("-c", "features.hooks=true")
				bad["a pin twice"] = beforePrompt("-c", "features.hooks=false")
				bad["saved rules"] = replace([]string{"--ignore-rules"}, nil)
				bad["no sandbox mode"] = replace([]string{"-s", "workspace-write"}, nil)
				bad["full access"] = replace([]string{"-s", "workspace-write"}, []string{"-s", "danger-full-access"})
				bad["another report"] = replace([]string{"-o", filepath.Join(a.Out, "report.md")}, []string{"-o", "/home/u/r.md"})
			case "exec:cursor":
				bad["refusals not looked for after the setup"] = replace([]string{"--refuse", ".cursor/hooks.json"}, nil)
				if c.effect != "branch" {
					bad["not in ask mode"] = replace([]string{"--mode", "ask"}, nil)
				}
			case "exec:deterministic":
				bad["its commands unwalled"] = func(s Spawn) Spawn { s.Argv = s.Argv[:len(s.Argv)-6]; return s }
			}
			for what, mutate := range bad {
				cp := sp
				cp.Argv, cp.Env = slices.Clone(sp.Argv), slices.Clone(sp.Env)
				if checkPins(c.name, self, a, sibs, mutate(cp)) == nil {
					t.Errorf("%s: passed", what)
				}
			}
		})
	}
	if checkPins("exec:other", "/opt/agentd", &Attempt{Claim: &Claimed{}, Out: "/x"}, nil, Spawn{Argv: []string{"x"}}) == nil {
		t.Fatal("an executor with no pins passed")
	}
}

func hasRunT(argv, run []string) bool {
	for i := 0; i+len(run) <= len(argv); i++ {
		if slices.Equal(argv[i:i+len(run)], run) {
			return true
		}
	}
	return false
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

// What is gone after an executor: its temporary directory (the Go caches
// in it) and npm's cache, through links too only inside the output; its
// codex home stays.
func TestAfterExecutor(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	out := filepath.Join(root, "tasks", "t1", "a1.out")
	must(t, os.WriteFile(filepath.Join(elsewhere, "keep"), []byte("x"), 0o600))
	mod := filepath.Join(out, "tmp", "go", "pkg", "mod", "m@v1")
	must(t, os.MkdirAll(mod, 0o700))
	must(t, os.WriteFile(filepath.Join(mod, "a.go"), []byte("package m"), 0o400))
	must(t, os.Chmod(mod, 0o500))
	must(t, os.MkdirAll(filepath.Join(out, "home", ".codex", "sessions"), 0o700))
	must(t, os.Symlink(elsewhere, filepath.Join(out, "tmp", "away")))
	must(t, os.Symlink(elsewhere, filepath.Join(out, "home", ".npm")))
	afterExecutor("exec:cursor", "", root, out, t.Logf)
	for _, gone := range []string{"tmp", "home/.npm"} {
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
	afterExecutor("exec:cursor", "", root, filepath.Join(root, "tasks", "t1", "none.out"), t.Logf) // no output: nothing to do
}

// Review 300 A1: an earlier run left a link where an attempt's output
// should be (the output itself, or a directory on the way, pointing at the
// home or at another attempt's output): nothing behind it is removed, and
// no login is taken from it.
func TestAfterExecutorTakesNoLinkedOutput(t *testing.T) {
	for _, c := range []struct{ name, link, out string }{
		{"the output", "tasks/t1/a1.out", "tasks/t1/a1.out"},
		{"a directory on the way", "tasks/t1", "tasks/t1/a1.out"},
		{"another attempt's output", "tasks/t1/a2.out", "tasks/t1/a2.out"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, master := t.TempDir(), t.TempDir()
			victim, target := filepath.Join(root, "tasks", "t9", "a1.out"), ""
			switch c.name {
			case "the output":
				victim = filepath.Join(t.TempDir(), "x")
				target = victim
			case "a directory on the way":
				target = t.TempDir()
				victim = filepath.Join(target, "a1.out")
			default:
				target = victim
			}
			for _, d := range []string{"tmp/go", "home/.codex"} {
				must(t, os.MkdirAll(filepath.Join(victim, d), 0o700))
			}
			must(t, os.WriteFile(filepath.Join(victim, "tmp", "go", "keep"), []byte("x"), 0o600))
			must(t, os.WriteFile(filepath.Join(master, "auth.json"), loginJSON(t, "acct-1", "r1", "2026-10-19T01:00:00Z", nil), 0o600))
			must(t, os.WriteFile(filepath.Join(victim, "home", ".codex", "auth.json"),
				loginJSON(t, "acct-1", "r2", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), nil), 0o600))
			must(t, os.MkdirAll(filepath.Dir(filepath.Join(root, c.link)), 0o700))
			must(t, os.Symlink(target, filepath.Join(root, c.link)))
			afterExecutor("exec:codex", master, root, filepath.Join(root, c.out), t.Logf)
			if _, err := os.Stat(filepath.Join(victim, "tmp", "go", "keep")); err != nil {
				t.Fatalf("removed behind a link: %v", err)
			}
			if b, _ := os.ReadFile(filepath.Join(master, "auth.json")); strings.Contains(string(b), "fixture-refresh-r2") {
				t.Fatal("a login was taken from behind a link")
			}
		})
	}
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

// Review 300 A3: what may not be in the checkout is looked for again once the
// setup is done, and the setup has a home of its own, not the executor's.
func TestSetupRefusesWhatItLeft(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	marker := filepath.Join(out, "executor-ran")
	home := setupHome(out)
	sp, err := withSetup(os.Args[0], out, [][]string{{"sh", "-c", `echo "$HOME" > home.txt && mkdir -p .cursor && echo '{}' > .cursor/hooks.json`}},
		Spawn{Argv: []string{"sh", "-c", "touch " + marker}}, setupOpts{Home: home, Refuse: cursorRefused})
	must(t, err)
	cmd := exec.Command(sp.Argv[0], sp.Argv[1:]...)
	cmd.Dir, cmd.Env = dir, append(launcherEnv(), "HOME=/executor/home")
	b, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != setupFailed || !strings.Contains(string(b), ".cursor/hooks.json") {
		t.Fatalf("exit %v:\n%s", err, b)
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Fatal("the executor started")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "home.txt")); strings.TrimSpace(string(got)) != home {
		t.Fatalf("the setup's HOME = %q, want %q", got, home)
	}
}

// agentd's own git in a checkout an executor had runs in the wall on a host
// that has one, and as it is on one that has none.
func TestGitWall(t *testing.T) {
	d := &Daemon{Self: "/opt/agentd"}
	if w := d.gitWall("/w/a1"); w != nil {
		t.Fatalf("no wall on the host, yet %q", w)
	}
	d.Wall = &Wall{ABI: 4}
	if w := d.gitWall("/w/a1"); !slices.Equal(w, []string{"/opt/agentd", ConfineArg, "--write", "/w/a1", "--"}) {
		t.Fatalf("git's wall = %q", w)
	}
}

// Review 301 A1: what agentd writes into an attempt's output after its
// executor (cursor's or claude's report, a line of the log) goes to the
// attempt's own file, never through a link or a hard link the executor left
// under that name.
func TestReportAndLogNotWrittenThroughLinks(t *testing.T) {
	for _, how := range []string{"link", "hard link", "plain"} {
		t.Run(how, func(t *testing.T) {
			out, elsewhere := t.TempDir(), t.TempDir()
			victim := filepath.Join(elsewhere, "report.md")
			must(t, os.WriteFile(victim, []byte("another attempt's report"), 0o600))
			report := filepath.Join(out, "report.md")
			switch how {
			case "link":
				must(t, os.Symlink(victim, report))
			case "hard link":
				must(t, os.Link(victim, report))
			}
			must(t, os.WriteFile(filepath.Join(out, "log.txt"), []byte(`{"type":"result","result":"replaced"}`+"\n"), 0o600))
			must(t, streamResultReport("cursor", filepath.Join(out, "log.txt"), report))
			if b, _ := os.ReadFile(victim); string(b) != "another attempt's report" {
				t.Fatalf("written through the %s: %q", how, b)
			}
			fi, err := os.Lstat(report)
			if b, _ := os.ReadFile(report); err != nil || !fi.Mode().IsRegular() || string(b) != "replaced" {
				t.Fatalf("the report = %q %v", b, err)
			}
		})
	}
	out, elsewhere := t.TempDir(), t.TempDir()
	victim := filepath.Join(elsewhere, "log.txt")
	must(t, os.WriteFile(victim, []byte("x"), 0o600))
	must(t, os.Symlink(victim, filepath.Join(out, "log.txt")))
	if appendOwn(out, "log.txt", "agentd: failed\n") == nil {
		t.Error("appended through a link out of the output")
	}
	if b, _ := os.ReadFile(victim); string(b) != "x" {
		t.Fatalf("the log line went through the link: %q", b)
	}
}
