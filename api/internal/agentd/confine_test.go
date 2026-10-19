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
	"syscall"
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
					// codex 0.163 sandboxes only with bubblewrap: no legacy mode
					// (06, 2026-10-19), and no wall of agentd's around it.
					if slices.ContainsFunc(sp.Argv, func(a string) bool { return strings.Contains(a, "use_legacy_landlock") }) ||
						slices.Contains(sp.Argv, ConfineArg) {
						t.Errorf("codex in legacy mode or a wall: %v", sp.Argv)
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

// Run's spawn for a confined attempt passes checkPins (codex's with no wall
// of agentd's around it); each pin taken away, repeated or overridden is
// refused (review 300 A2, A3).
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
				Dir: t.TempDir(), Out: t.TempDir(), Walled: !selfSandboxed[c.name]}
			sibs := []string{filepath.Join(filepath.Dir(a.Dir), "bifrost-ui")}
			a.inner = innerWrites(a, sibs)
			bin := t.TempDir()
			must(t, os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755))
			env := withEnv(hostEnv, "PATH", bin+":/bin")
			w := &Wall{ABI: 4, Codex: filepath.Join(bin, "codex"), Path: bin + ":/bin"}
			sp, err := Executors(self, env, Secrets{CursorAPIKey: "fixture-key", ClaudeOAuthToken: "fixture-token"})[c.name](a)
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
			wall := wallArgv(self, executorWrites(c.name, a, sibs))
			if selfSandboxed[c.name] {
				wall = nil
			}
			for _, built := range []Spawn{sp, withS} {
				built.Argv = append(slices.Clone(wall), built.Argv...)
				if err = checkPins(w, c.name, self, a, sibs, built); err != nil {
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
					s.Argv = append(wallArgv(self, append(executorWrites(c.name, a, sibs), "/home/u")), s.Argv[n:]...)
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
			if selfSandboxed[c.name] {
				delete(bad, "no wall")
				delete(bad, "one more directory")
				bad["a wall around it"] = func(s Spawn) Spawn {
					s.Argv = append(wallArgv(self, executorWrites(c.name, a, sibs)), s.Argv...)
					return s
				}
			}
			bad["another directory"] = func(s Spawn) Spawn { s.Dir = t.TempDir(); return s }
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
				bad["legacy Landlock mode"] = beforePrompt("-c", "features.use_legacy_landlock=true")
				// Review 304 A2: the program and the PATH its sandbox was proven with.
				bad["PATH of the checkout"] = func(s Spawn) Spawn { s.Env = withEnv(s.Env, "PATH", a.Dir+"/bin:/bin"); return s }
				bad["codex by name"] = replace([]string{filepath.Join(bin, "codex"), "exec"}, []string{"codex", "exec"})
				bad["hooks on"] = replace([]string{"-c", "features.hooks=false"}, nil)
				bad["hooks on again"] = beforePrompt("-c", "features.hooks=true")
				bad["a pin twice"] = beforePrompt("-c", "features.hooks=false")
				bad["saved rules"] = replace([]string{"--ignore-rules"}, nil)
				bad["no sandbox mode"] = replace([]string{"-s", "workspace-write"}, nil)
				bad["full access"] = replace([]string{"-s", "workspace-write"}, []string{"-s", "danger-full-access"})
				bad["another report"] = replace([]string{"-o", filepath.Join(a.Out, "report.md")}, []string{"-o", "/home/u/r.md"})
			case "exec:cursor":
				bad["refusals not looked for after the setup"] = replace([]string{"--refuse", ".cursor/hooks.json"}, nil)
				bad["no CURSOR_DATA_DIR"] = func(s Spawn) Spawn { s.Env = withoutEnv(s.Env, "CURSOR_DATA_DIR"); return s }
				if c.effect != "branch" {
					bad["not in ask mode"] = replace([]string{"--mode", "ask"}, nil)
				}
			case "exec:deterministic":
				bad["its commands unwalled"] = func(s Spawn) Spawn { s.Argv = s.Argv[:len(s.Argv)-6]; return s }
			}
			for what, mutate := range bad {
				cp := sp
				cp.Argv, cp.Env = slices.Clone(sp.Argv), slices.Clone(sp.Env)
				if checkPins(w, c.name, self, a, sibs, mutate(cp)) == nil {
					t.Errorf("%s: passed", what)
				}
			}
		})
	}
	if checkPins(nil, "exec:other", "/opt/agentd", &Attempt{Claim: &Claimed{}, Out: "/x"}, nil, Spawn{Argv: []string{"x"}}) == nil {
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
	w, lines := ProveWall(t.Context(), os.Args[0], t.TempDir(), launcherEnv(), "/opt/codex")
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
			must(t, streamResultReport("cursor", "", out))
			if b, _ := os.ReadFile(victim); string(b) != "another attempt's report" {
				t.Fatalf("written through the %s: %q", how, b)
			}
			fi, err := os.Lstat(report)
			if b, _ := os.ReadFile(report); err != nil || !fi.Mode().IsRegular() || string(b) != "replaced" {
				t.Fatalf("the report = %q %v", b, err)
			}
		})
	}
	// The line of a failed attempt goes into a new log file: neither a link
	// out of the output nor a hard link to another file is written through
	// (review 302).
	for _, how := range []string{"link", "hard link"} {
		out, elsewhere := t.TempDir(), t.TempDir()
		victim := filepath.Join(elsewhere, "log.txt")
		must(t, os.WriteFile(victim, []byte("x"), 0o600))
		if how == "link" {
			must(t, os.Symlink(victim, filepath.Join(out, "log.txt")))
			if appendOwn(openRootT(t, out), "log.txt", "agentd: failed\n") == nil {
				t.Error("appended through a link out of the output")
			}
		} else {
			must(t, os.Link(victim, filepath.Join(out, "log.txt")))
			must(t, appendOwn(openRootT(t, out), "log.txt", "agentd: failed\n"))
			if b, _ := os.ReadFile(filepath.Join(out, "log.txt")); string(b) != "xagentd: failed\n" {
				t.Errorf("the log = %q", b)
			}
		}
		if b, _ := os.ReadFile(victim); string(b) != "x" {
			t.Fatalf("the log line went through the %s: %q", how, b)
		}
	}
	// The output itself, or the checkout, a link left by an earlier run:
	// agentd's report and index files do not go behind it (review 302).
	root, elsewhere := t.TempDir(), t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "tasks", "t1"), 0o700))
	must(t, os.WriteFile(filepath.Join(elsewhere, "report.md"), []byte("theirs"), 0o600))
	must(t, os.WriteFile(filepath.Join(elsewhere, "log.txt"), []byte(`{"type":"result","result":"replaced"}`+"\n"), 0o600))
	must(t, os.Symlink(elsewhere, filepath.Join(root, "tasks", "t1", "a1.out")))
	if streamResultReport("cursor", root, filepath.Join(root, "tasks", "t1", "a1.out")) == nil {
		t.Error("a report written behind a linked output")
	}
	if b, _ := os.ReadFile(filepath.Join(elsewhere, "report.md")); string(b) != "theirs" {
		t.Fatalf("written behind the linked output: %q", b)
	}
}

// Review 303 B: a log or a report the executor left as a FIFO with no
// writer is not read, and the reading does not wait for one.
func TestResultsDoNotWaitOnAFIFO(t *testing.T) {
	out := t.TempDir()
	for _, name := range []string{"log.txt", "report.md"} {
		if err := syscall.Mkfifo(filepath.Join(out, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &Attempt{Claim: &Claimed{Role: "implement"}, Out: out}
	done := make(chan results, 1)
	go func() { done <- a.results() }()
	select {
	case r := <-done:
		if r.reportBody != nil || len(r.logTail) > 0 {
			t.Fatalf("read a FIFO: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the results wait on a FIFO")
	}
}

// codex's proof runs its own sandbox with codexExecutor's pins and nothing
// of agentd's wall around it (the cut's §10 A).
func TestCodexProofArgv(t *testing.T) {
	argv := codexProofArgv("/opt/codex/bin/codex", "/opt/agentd", "/p/ws", "/p/home", "/p/tmp", "/p/out/file", "net:[1]", "Ix")
	if argv[0] != "/opt/codex/bin/codex" || argv[1] != "sandbox" || slices.Contains(argv, ConfineArg) ||
		slices.ContainsFunc(argv, func(a string) bool { return strings.Contains(a, "use_legacy_landlock") }) {
		t.Fatalf("argv = %q", argv)
	}
	for _, pin := range append(slices.Clone(codexPins), codexRoots("/p/tmp")) {
		if !hasRunT(argv, []string{"-c", pin}) {
			t.Errorf("no -c %s: %q", pin, argv)
		}
	}
	if !hasRunT(argv, []string{"--", "/opt/agentd", CheckArg, "--codex", "--in", "/p/ws", "--tmp", "/p/tmp",
		"--outside", "/p/home", "--file", "/p/out/file", "--netns", "net:[1]", "--nonce", "Ix"}) {
		t.Fatalf("argv = %q", argv)
	}
	// The legacy workspace-write mode, as codex exec -s workspace-write: a
	// named profile (-P) ignores exclude_slash_tmp and leaves the unit's
	// shared /tmp writable (06, 2026-10-19 11:33).
	if !hasRunT(argv, []string{"-c", `sandbox_mode="workspace-write"`}) || slices.Contains(argv, "-P") ||
		slices.Contains(argv, "--permission-profile") || slices.Contains(argv, "-C") {
		t.Fatalf("argv = %q", argv)
	}
}

// CodexBin is the codex PATH finds, absolute, executable; none is "".
func TestCodexBin(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(a, "codex"), []byte("x"), 0o600)) // not executable
	must(t, os.WriteFile(filepath.Join(b, "codex"), []byte("#!/bin/sh\n"), 0o755))
	if got := CodexBin([]string{"PATH=rel:" + a + ":" + b}); got != filepath.Join(b, "codex") {
		t.Fatalf("CodexBin = %q", got)
	}
	if got := CodexBin([]string{"PATH=" + a}); got != "" {
		t.Fatalf("CodexBin = %q", got)
	}
}

// The check in codex's mode finds a sandbox that holds nothing: run outside
// any, the write beside the workspace goes through, and it exits 1. Whether
// /tmp takes the write depends on where the test runs (agentd's wall refuses
// it: TD-380), so that line is not asserted.
func TestCodexCheckFindsNoSandbox(t *testing.T) {
	in, tmp, outside := t.TempDir(), t.TempDir(), t.TempDir()
	file := filepath.Join(t.TempDir(), "file")
	must(t, os.WriteFile(file, []byte("before\n"), 0o600))
	cmd := exec.Command(os.Args[0], CheckArg, "--codex", "--in", in, "--tmp", tmp, "--outside", outside, "--file", file, "--netns", "net:[none]", "--nonce", "Ix")
	cmd.Env = launcherEnv()
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("exit %v:\n%s", err, out)
	}
	for _, want := range []string{"ok: write inside", "ok: write the temporary directory", "FAIL: make a file outside was allowed",
		"FAIL: append to a file outside was allowed", checkDone + "Ix"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
}

// Review 305 A: codex is a wrapper whose interpreter PATH finds (npm's
// "#!/usr/bin/env node"); a relative PATH entry would find another one in the
// checkout, where the attempt runs. The spawn's PATH has none, so the
// interpreter is the one the proof found.
func TestCodexWrapperFindsTheTrustedInterpreter(t *testing.T) {
	trusted, checkout, out := t.TempDir(), t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(trusted, "fakenode"), []byte("#!/bin/sh\necho trusted\n"), 0o755))
	must(t, os.WriteFile(filepath.Join(trusted, "codex"), []byte("#!/usr/bin/env fakenode\n"), 0o755))
	must(t, os.WriteFile(filepath.Join(checkout, "fakenode"), []byte("#!/bin/sh\necho checkout\n"), 0o755))
	environ := []string{"PATH=.:" + trusted + ":/usr/bin:/bin"}
	a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: checkout, Out: out}
	sp, err := Executors("/opt/agentd", environ, Secrets{})["exec:codex"](a)
	must(t, err)
	cmd := exec.Command(sp.Argv[0])
	cmd.Env, cmd.Dir = sp.Env, sp.Dir
	b, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(b)) != "trusted" {
		t.Fatalf("the wrapper ran %q (%v)", b, err)
	}
	// The PATH the proof runs with (AllowedEnv) is the spawn's: it passes.
	w := &Wall{Codex: filepath.Join(trusted, "codex"), Path: envValues(AllowedEnv(environ), "PATH")[0]}
	if err = checkPins(w, "exec:codex", "/opt/agentd", a, nil, sp); err != nil {
		t.Fatal(err)
	}
}

// Review 306 A: codex is not held when codex or a directory of its PATH is
// in agentd's directory, where an attempt's setup could put a program the
// proof did not run; through a link, or a directory that does not exist yet,
// too. The check comes before codex is run at all.
func TestCodexHeldNotWithAPathInAgentdsDirectory(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	link := filepath.Join(elsewhere, "into-root")
	must(t, os.Symlink(root, link))
	for _, path := range []string{
		filepath.Join(root, "tasks", "t1", "a1", "bin") + ":/usr/bin:/bin", // not there yet
		link + "/tasks/t1/a1/bin:/usr/bin:/bin",
		root + ":/usr/bin",
	} {
		_, err := codexHeld(t.Context(), "/opt/codex/bin/codex", "/opt/agentd", root, t.TempDir(), []string{"PATH=" + path})
		if err == nil || !strings.Contains(err.Error(), "agentd's directory") {
			t.Errorf("PATH %s: %v", path, err)
		}
	}
	if _, err := codexHeld(t.Context(), filepath.Join(root, "bin", "codex"), "/opt/agentd", root, t.TempDir(), []string{"PATH=/usr/bin"}); err == nil ||
		!strings.Contains(err.Error(), "agentd's directory") {
		t.Errorf("codex in agentd's directory: %v", err)
	}
	if err := pathOutside(root, "/opt/codex/bin/codex", []string{"PATH=/opt/codex/bin:/usr/bin:/bin"}); err != nil {
		t.Fatal(err)
	}
}

// Review 307: codex itself a link into agentd's directory, and a PATH entry
// a dangling link into it (its target not made yet): neither is trusted.
func TestCodexHeldNotThroughLinksIntoAgentdsDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	checkout := filepath.Join(root, "tasks", "t1", "a1")
	must(t, os.MkdirAll(checkout, 0o700))
	must(t, os.WriteFile(filepath.Join(checkout, "codex"), []byte("#!/bin/sh\n"), 0o755))
	must(t, os.MkdirAll(filepath.Join(outside, "bin"), 0o700))
	codex := filepath.Join(outside, "bin", "codex")
	must(t, os.Symlink(filepath.Join(checkout, "codex"), codex))
	if err := pathOutside(root, codex, []string{"PATH=/usr/bin:/bin"}); err == nil {
		t.Error("a codex linked into agentd's directory is trusted")
	}
	dangling := filepath.Join(outside, "future-bin")
	must(t, os.Symlink(filepath.Join(checkout, "bin"), dangling)) // not made yet
	if err := pathOutside(root, "/opt/codex/bin/codex", []string{"PATH=" + dangling + ":/usr/bin:/bin"}); err == nil {
		t.Error("a PATH entry linked into agentd's directory, not made yet, is trusted")
	}
	// A dangling link anywhere else is not trusted either: it cannot be resolved.
	nowhere := filepath.Join(outside, "nowhere")
	must(t, os.Symlink(filepath.Join(outside, "gone", "bin"), nowhere))
	if err := pathOutside(root, "/opt/codex/bin/codex", []string{"PATH=" + nowhere}); err == nil {
		t.Error("an unresolvable PATH entry is trusted")
	}
}

// cursor's data directory (CURSOR_DATA_DIR): on the agent host's two users
// its projects directory, where cursor puts its sockets, is at most 83
// characters, so cursor never falls back to /tmp/.cursor (STG 2026-10-19:
// EACCES under the wall); it is the attempt's own, in agentd's directory, and
// the wall lets cursor write it.
func TestCursorDataDirIsShortAndWalled(t *testing.T) {
	for _, root := range []string{"/home/bifrost-agentd/agent-work/agentd", "/home/bifrost-agentd-stg/agent-work/agentd"} {
		out := filepath.Join(root, "tasks", "t_01M5AHP9Q7G4RWDSW5DGR5DSY2", "a12.out")
		d := cursorDataDir(root, out)
		if n := len(filepath.Join(d, "projects")); n > cursorPathMax {
			t.Errorf("%s: %s/projects is %d characters", root, d, n)
		}
		if rel, err := filepath.Rel(root, d); err != nil || !filepath.IsLocal(rel) {
			t.Errorf("%s is not in %s", d, root)
		}
		if d == cursorDataDir(root, filepath.Join(root, "tasks", "t_01M5AHP9Q7G4RWDSW5DGR5DSY2", "a13.out")) {
			t.Errorf("two attempts share %s", d)
		}
		a := &Attempt{Dir: strings.TrimSuffix(out, ".out"), Out: out, root: root}
		if !slices.Contains(executorWrites("exec:cursor", a, nil), d) || slices.Contains(executorWrites("exec:claude", a, nil), d) {
			t.Errorf("the wall's directories: %q", executorWrites("exec:cursor", a, nil))
		}
	}
	// The builder sets it, makes it, and refuses one too long for the sockets.
	root, err := os.MkdirTemp("/tmp", "cw")
	if err != nil {
		root = t.TempDir() // inside agentd's wall /tmp is refused
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
	out := filepath.Join(root, "a1.out")
	must(t, os.MkdirAll(out, 0o700))
	a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: t.TempDir(), Out: out, root: root}
	sp, err := cursorExecutor(nil)(a)
	d := cursorDataDir(root, out)
	if len(filepath.Join(d, "projects")) > cursorPathMax {
		// Inside agentd's wall (/tmp refused, a long TMPDIR): only the
		// refusal can be checked here; the host's paths are checked above.
		if err == nil {
			t.Fatalf("cursor starts with %s, too long for its sockets", d)
		}
		t.Log("no short writable root here: the builder's refusal checked, its success not")
		return
	}
	must(t, err)
	if got := envValues(sp.Env, "CURSOR_DATA_DIR"); !slices.Equal(got, []string{d}) {
		t.Fatalf("CURSOR_DATA_DIR = %q, want %s", got, d)
	}
	if fi, serr := os.Lstat(d); serr != nil || !fi.IsDir() {
		t.Fatalf("%s: %v", d, serr)
	}
	sp.Argv = append(wallArgv("/opt/agentd", executorWrites("exec:cursor", a, nil)), sp.Argv...)
	if err = checkPins(nil, "exec:cursor", "/opt/agentd", a, nil, sp); err != nil {
		t.Fatal(err)
	}
	other := sp
	other.Env = withEnv(slices.Clone(sp.Env), "CURSOR_DATA_DIR", filepath.Join(out, "home", ".cursor"))
	if checkPins(nil, "exec:cursor", "/opt/agentd", a, nil, other) == nil {
		t.Fatal("cursor with another data directory passed")
	}
	other = sp
	other.Argv = append(wallArgv("/opt/agentd", []string{a.Dir, a.Out}), sp.Argv[len(wallArgv("/opt/agentd", executorWrites("exec:cursor", a, nil))):]...)
	if checkPins(nil, "exec:cursor", "/opt/agentd", a, nil, other) == nil {
		t.Fatal("cursor with no wall over its data directory passed")
	}
	// Gone after the attempt, through agentd's directory.
	afterExecutor("exec:cursor", "", root, out, t.Logf)
	if _, serr := os.Lstat(d); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("%s is still there: %v", d, serr)
	}
	// The output gone, or a link: the data directory still goes (review 360
	// B1); a link where its parent should be makes cursor not start.
	must(t, os.MkdirAll(d, 0o700))
	must(t, os.RemoveAll(out))
	afterExecutor("exec:cursor", "", root, out, t.Logf)
	if _, serr := os.Lstat(d); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("%s outlived its output: %v", d, serr)
	}
	must(t, os.MkdirAll(out, 0o700))
	elsewhere := t.TempDir()
	must(t, os.RemoveAll(filepath.Dir(d)))
	must(t, os.Symlink(elsewhere, filepath.Dir(d)))
	if _, err = cursorExecutor(nil)(a); err == nil {
		t.Fatal("cursor's data directory made through a link")
	}
	afterExecutor("exec:cursor", "", root, out, t.Logf)
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("made or removed behind the link: %v", entries)
	}
	must(t, os.Remove(filepath.Dir(d)))
	// A root too long for it: cursor does not start.
	long := filepath.Join(root, strings.Repeat("x", 80))
	must(t, os.MkdirAll(filepath.Join(long, "a1.out"), 0o700))
	if _, err = cursorExecutor(nil)(&Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: t.TempDir(),
		Out: filepath.Join(long, "a1.out"), root: long}); err == nil {
		t.Fatal("cursor starts with a data directory too long for its sockets")
	}
}

// Review 361 B1: an attempt that ends without its executor having run (a
// stop while it was prepared) or after a restart leaves no cursor data
// directory: every end that deletes its entry, with its group gone, removes
// it.
func TestSettleRemovesCursorData(t *testing.T) {
	root := t.TempDir()
	reg, err := OpenRegistry(filepath.Join(root, "registry.json"))
	must(t, err)
	d := &Daemon{Root: root, Registry: reg, Logf: t.Logf}
	work := filepath.Join(root, "tasks", "t1", "a1")
	data := cursorDataDir(root, work+".out")
	must(t, os.MkdirAll(data, 0o700))
	e := Entry{AttemptID: "a1", TaskID: "t1", Workdir: work, Phase: "ending", Group: GroupCleared}
	must(t, reg.Put(e))
	must(t, d.settle(&e))
	if _, serr := os.Lstat(data); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("%s outlived the attempt: %v", data, serr)
	}
}
