package agentd

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestClaudeExecutor(t *testing.T) {
	for _, model := range []string{"", "opus"} {
		t.Run("model="+model, func(t *testing.T) {
			a := &Attempt{Claim: &Claimed{Tool: Tool{Model: model}}, Spec: TaskSpec{Text: "# plan"}, Dir: t.TempDir(), Out: t.TempDir()}
			sp, err := Executors("agentd", nil, Secrets{})["exec:claude"](a)
			if err != nil {
				t.Fatal(err)
			}
			if sp.Argv[0] != "claude" || sp.Dir != a.Dir || sp.After == nil || sp.Argv[len(sp.Argv)-1] != Prompt(a) ||
				sp.Argv[len(sp.Argv)-2] != "--" {
				t.Fatalf("claude = %+v", sp)
			}
			// Nothing but "--" may stand between the variadic tool lists and the
			// prompt's place: every flag comes before it (review 215 B1).
			if slices.Index(sp.Argv, "--") != len(sp.Argv)-2 {
				t.Fatalf("\"--\" must come once, right before the prompt: %v", sp.Argv)
			}
			for _, flag := range []string{"-p", "--verbose", "--strict-mcp-config"} {
				if !slices.Contains(sp.Argv, flag) {
					t.Errorf("missing %s: %v", flag, sp.Argv)
				}
			}
			for flag, value := range claudeFlags(false) {
				i := slices.Index(sp.Argv, flag)
				if i < 0 || i+1 >= len(sp.Argv) || sp.Argv[i+1] != value {
					t.Errorf("%s should have value %q: %v", flag, value, sp.Argv)
				}
			}
			i := slices.Index(sp.Argv, "--model")
			if (i >= 0) != (model != "") || (i >= 0 && sp.Argv[i+1] != model) {
				t.Fatalf("model %q: %v", model, sp.Argv)
			}
			for _, arg := range sp.Argv {
				if strings.Contains(arg, "skip-permissions") || strings.Contains(arg, "bypassPermissions") {
					t.Fatalf("permission bypass in argv: %v", sp.Argv)
				}
			}
			if err := os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte(`{"type":"result","result":"# plan"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := sp.After(); err != nil {
				t.Fatal(err)
			}
			if b, err := os.ReadFile(filepath.Join(a.Out, "report.md")); err != nil || string(b) != "# plan" {
				t.Fatalf("report = %q, %v", b, err)
			}
		})
	}
}

// claudeFlags are the flags a claude attempt has with their values: a
// reader's, or an implementer's, which also edits, writes and runs commands.
func claudeFlags(writes bool) map[string]string {
	f := map[string]string{
		"--output-format": "stream-json", "--mcp-config": `{"mcpServers":{}}`,
		"--allowedTools": "Read,Grep,Glob", "--tools": "Read,Grep,Glob",
		"--disallowedTools": "Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch",
		"--setting-sources": "", "--settings": `{"disableAllHooks":true}`,
	}
	if writes {
		f["--allowedTools"], f["--tools"] = "Read,Grep,Glob,Edit,Write,Bash", "Read,Grep,Glob,Edit,Write,Bash"
		f["--disallowedTools"] = "NotebookEdit,WebFetch,WebSearch"
	}
	return f
}

// W-31 2.4 「Claude 写核心代码」: an attempt that pushes a branch (an
// implementer's) gives claude Edit, Write and Bash, and only inside agentd's
// write wall; with no wall it does not start. Every other attempt keeps the
// read-only tools, walled or not. The pins stay either way: no settings
// file read, hooks off, no MCP server, the attempt's own configuration and
// temporary directories.
func TestClaudeImplementsOnlyInsideTheWall(t *testing.T) {
	for _, c := range []struct {
		effect        string
		walled        bool
		writes, start bool
	}{
		{"branch", true, true, true}, {"branch", false, false, false},
		{"none", true, false, true}, {"none", false, false, true}, {"", true, false, true},
	} {
		t.Run(c.effect+"/walled="+strconv.FormatBool(c.walled), func(t *testing.T) {
			a := &Attempt{Claim: &Claimed{EffectClass: c.effect, Tool: Tool{Model: "opus"}}, Spec: TaskSpec{Text: "# do it"},
				Dir: t.TempDir(), Out: t.TempDir(), Walled: c.walled}
			sp, err := Executors("/opt/agentd", hostEnv, Secrets{ClaudeOAuthToken: "fixture-token"})["exec:claude"](a)
			if !c.start {
				if err == nil || err.Error() != claudeNoWall {
					t.Fatalf("an implementer's claude with no wall: %v %v", err, sp.Argv)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for flag, value := range claudeFlags(c.writes) {
				i := slices.Index(sp.Argv, flag)
				if i < 0 || i+1 >= len(sp.Argv) || sp.Argv[i+1] != value || slices.Index(sp.Argv[i+1:], flag) >= 0 {
					t.Errorf("%s should be once, with %q: %v", flag, value, sp.Argv)
				}
			}
			if !slices.Contains(sp.Argv, "--strict-mcp-config") || slices.Index(sp.Argv, "--") != len(sp.Argv)-2 {
				t.Errorf("claude = %v", sp.Argv)
			}
			for k, v := range map[string]string{"CLAUDE_CONFIG_DIR": claudeDirOf(a.Out), "CLAUDE_CODE_TMPDIR": claudeTmpOf(a.Out),
				"DISABLE_AUTOUPDATER": "1", "HOME": attemptHome(a.Out), "TMPDIR": attemptTmp(a.Out)} {
				if got := envValues(sp.Env, k); !slices.Equal(got, []string{v}) {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			// claude's temporary root is its own: not the temporary directory
			// the setup and the checked code also write, nor /tmp.
			if tmp := claudeTmpOf(a.Out); filepath.Dir(tmp) != attemptHome(a.Out) {
				t.Errorf("claude's temporary root %s is outside its home", tmp)
			} else if fi, err := os.Lstat(tmp); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
				t.Errorf("claude's temporary root was not made its own: %v %v", fi, err)
			}
		})
	}
}

func TestClaudeTokenReachesClaudeAlone(t *testing.T) {
	for _, token := range []string{"", "oauth-fixture"} {
		a := &Attempt{Claim: &Claimed{}, Dir: t.TempDir(), Out: t.TempDir()}
		ex := Executors("agentd", []string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=host-token", "CURSOR_API_KEY=host-key"},
			Secrets{ClaudeOAuthToken: token, CursorAPIKey: "cursor-fixture"})
		for name, e := range ex {
			sp, err := e(a)
			if err != nil {
				t.Fatal(err)
			}
			var tokens, keys []string
			for _, kv := range sp.Env {
				if strings.HasPrefix(kv, "CLAUDE_CODE_OAUTH_TOKEN=") {
					tokens = append(tokens, kv)
				}
				if strings.HasPrefix(kv, "CURSOR_API_KEY=") {
					keys = append(keys, kv)
				}
			}
			var wantTokens, wantKeys []string
			if name == "exec:claude" && token != "" {
				wantTokens = []string{"CLAUDE_CODE_OAUTH_TOKEN=" + token}
			}
			if name == "exec:cursor" {
				wantKeys = []string{"CURSOR_API_KEY=cursor-fixture"}
			}
			if !slices.Equal(tokens, wantTokens) || !slices.Equal(keys, wantKeys) {
				t.Fatalf("%s: tokens %v, keys %v", name, tokens, keys)
			}
			for _, arg := range sp.Argv {
				if strings.Contains(arg, "oauth-fixture") {
					t.Fatalf("%s: OAuth token in argv", name)
				}
			}
		}
	}
}

func TestStreamResultReport(t *testing.T) {
	long := `{"type":"result","result":"` + strings.Repeat("x", 2*maxBody) + `"}`
	for _, vendor := range []string{"claude", "cursor"} {
		for _, tc := range []struct {
			name, log, want string
		}{
			{"last result", "{bad json}\n" + `{"type":"result","result":"first"}` + "\n" +
				`{"type":"assistant","result":"ignore"}` + "\n" + `{"type":"result","result":"# last\nok"}`, "# last\nok"},
			{"no result", `{"type":"assistant","result":"ignore"}`, ""},
			{"long line skipped", long + "\n" + `{"type":"result","result":"last"}`, "last"},
			{"long result ignored", `{"type":"result","result":"first"}` + "\n" + long + "\n", "first"},
			{"error after a result", `{"type":"result","result":"first"}` + "\n" +
				`{"type":"result","is_error":true,"result":"Failed to authenticate:\nOAuth session expired"}`, "error"},
			{"result after an error", `{"type":"result","is_error":true,"result":"Failed"}` + "\n" + `{"type":"result","result":"ok"}`, "ok"},
		} {
			t.Run(vendor+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				log, report := filepath.Join(dir, "log.txt"), filepath.Join(dir, "report.md")
				if err := os.WriteFile(log, []byte(tc.log), 0o600); err != nil {
					t.Fatal(err)
				}
				err := streamResultReport(vendor, "", dir)
				if tc.want == "error" {
					if err == nil || err.Error() != "agentd: "+vendor+" ended in error: Failed to authenticate: OAuth session expired" {
						t.Fatalf("error result: %v", err)
					}
					if _, err := os.Stat(report); !os.IsNotExist(err) {
						t.Fatalf("an error result became a report: %v", err)
					}
					return
				}
				if tc.want == "" {
					if err == nil || err.Error() != "agentd: "+vendor+" left no result" {
						t.Fatalf("missing result: %v", err)
					}
					if _, err := os.Stat(report); !os.IsNotExist(err) {
						t.Fatalf("missing result created a report: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if b, err := os.ReadFile(report); err != nil || string(b) != tc.want {
					t.Fatalf("report = %q, %v; want %q", b, err, tc.want)
				}
			})
		}
	}
}

func TestClaudeNeedsNoMCPCheck(t *testing.T) {
	ex := Executors("agentd", nil, Secrets{})
	// Only claude is offered here, so the real map never invokes another CLI.
	kept, why := Isolated(map[string]Executor{"exec:claude": ex["exec:claude"]}, MCPChecks)
	if kept["exec:claude"] == nil || len(why) != 0 {
		t.Fatalf("claude requires no host MCP probe: kept %v, why %v", kept, why)
	}
}

// checkPins on a confined claude, a reader's and an implementer's, as Run
// builds it: each passes; the other's tool lists, a tool more, a web fetch
// allowed, its temporary root missing or the shared temporary directory,
// and an implementer whose attempt is not walled, are refused (W-31 2.4).
func TestClaudeCheckPins(t *testing.T) {
	for _, effect := range []string{"none", "branch"} {
		t.Run(effect, func(t *testing.T) {
			const self = "/opt/agentd"
			a := &Attempt{Claim: &Claimed{EffectClass: effect, Tool: Tool{Model: "opus"}}, Spec: TaskSpec{Text: "# do it"},
				Dir: t.TempDir(), Out: t.TempDir(), Walled: true}
			sp, err := Executors(self, hostEnv, Secrets{ClaudeOAuthToken: "fixture-token"})["exec:claude"](a)
			if err != nil {
				t.Fatal(err)
			}
			sp.Argv = append(wallArgv(self, executorWrites("exec:claude", a, nil)), sp.Argv...)
			w := &Wall{ABI: 4, Executors: map[string]bool{"exec:claude": true}}
			if err = checkPins(w, "exec:claude", self, a, nil, sp); err != nil {
				t.Fatalf("the spawn as Run builds it: %v", err)
			}
			writes := effect == "branch"
			swap := func(old, new []string) func(Spawn) Spawn {
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
			tools, refused := claudeReadTools, claudeReadRefused
			if writes {
				tools, refused = claudeWriteTools, claudeWriteRefused
			}
			bad := map[string]func(Spawn) Spawn{
				"the other's tools": swap(claudeTools(writes), claudeTools(!writes)),
				"a tool more":       swap([]string{"--tools", tools}, []string{"--tools", tools + ",NotebookEdit"}),
				"a tool allowed":    swap([]string{"--allowedTools", tools}, []string{"--allowedTools", tools + ",WebFetch"}),
				"web fetch allowed": swap([]string{"--disallowedTools", refused}, []string{"--disallowedTools", "NotebookEdit"}),
				"no CLAUDE_CODE_TMPDIR": func(s Spawn) Spawn {
					s.Env = withoutEnv(s.Env, "CLAUDE_CODE_TMPDIR")
					return s
				},
				"the shared temporary directory as claude's": func(s Spawn) Spawn {
					s.Env = withEnv(s.Env, "CLAUDE_CODE_TMPDIR", attemptTmp(a.Out))
					return s
				},
				"/tmp as claude's": func(s Spawn) Spawn { s.Env = withEnv(s.Env, "CLAUDE_CODE_TMPDIR", "/tmp"); return s },
			}
			for what, mutate := range bad {
				cp := sp
				cp.Argv, cp.Env = slices.Clone(sp.Argv), slices.Clone(sp.Env)
				if checkPins(w, "exec:claude", self, a, nil, mutate(cp)) == nil {
					t.Errorf("%s: passed", what)
				}
			}
			a.Walled = false
			if err = checkPins(w, "exec:claude", self, a, nil, sp); (err == nil) == writes {
				t.Errorf("an attempt not walled, writes %v: %v", writes, err)
			}
		})
	}
}

// claude's temporary root goes with the attempt's other temporary files once
// the executor is gone.
func TestAfterExecutorRemovesClaudesTemporaryRoot(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "tasks", "t1", "a1.out")
	must(t, os.MkdirAll(filepath.Join(claudeTmpOf(out), "claude-501"), 0o700))
	must(t, os.MkdirAll(claudeDirOf(out), 0o700))
	afterExecutor("exec:claude", "", root, out, t.Logf)
	if _, err := os.Lstat(claudeTmpOf(out)); !os.IsNotExist(err) {
		t.Errorf("claude's temporary root is still there: %v", err)
	}
	if _, err := os.Stat(claudeDirOf(out)); err != nil {
		t.Errorf("claude's configuration directory went: %v", err)
	}
}

// Review 422 B1: only codex's commands have no network (its sandbox), and
// only codex is told so; claude's and cursor's commands have the host's.
func TestPromptSaysNoNetworkToCodexOnly(t *testing.T) {
	for _, ex := range []string{"exec:codex", "exec:claude", "exec:cursor", "exec:deterministic"} {
		p := Prompt(&Attempt{Claim: &Claimed{Tool: Tool{Executor: ex}}, Spec: TaskSpec{Text: "# do it"}})
		if strings.Contains(p, "no network") != (ex == "exec:codex") {
			t.Errorf("%s's prompt on the network: %q", ex, p)
		}
		if !strings.Contains(p, "keep them as they are, so that whatever the task's setup fetched is used") {
			t.Errorf("%s's prompt lost the cache note: %q", ex, p)
		}
	}
}

// refusals is a shell script that tries each write and says, in results,
// which went through ("WROTE"), which the kernel refused ("denied": Permission
// denied, Operation not permitted, a cross-device link) and which failed
// otherwise ("failed", with its error): a missing directory or a shell
// error is no refusal (review 422 C1). Each write's error goes to
// $TMPDIR/err, which the script must be able to write.
func refusals(results string, writes map[string]string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, name := range slices.Sorted(maps.Keys(writes)) {
		b.WriteString("if (" + writes[name] + ") 2>\"$TMPDIR/err\"; then echo 'WROTE " + name + "'; " +
			"elif grep -qE 'Permission denied|Operation not permitted|Invalid cross-device link' \"$TMPDIR/err\"; then echo 'denied " + name + "'; " +
			"else echo \"failed " + name + ": $(cat \"$TMPDIR/err\")\"; fi >> '" + results + "'\n")
	}
	return b.String()
}

// Review 423 C1: a write that fails for want of its directory is no
// refusal; one the permissions refuse is (as root, which the permissions do
// not hold, that case is left out).
func TestRefusalsTellARefusalFromAFailure(t *testing.T) {
	dir, tmp := t.TempDir(), t.TempDir()
	closed := filepath.Join(dir, "closed")
	must(t, os.Mkdir(closed, 0o500))
	t.Cleanup(func() { _ = os.Chmod(closed, 0o700) })
	results := filepath.Join(dir, "results")
	writes := map[string]string{"missing": "echo x > '" + filepath.Join(dir, "no", "such", "x") + "'", "open": "echo x > '" + filepath.Join(dir, "x") + "'"}
	want := []string{"failed missing:", "WROTE open"}
	if os.Geteuid() != 0 {
		writes["closed"] = "echo x > '" + filepath.Join(closed, "x") + "'"
		want = []string{"denied closed", "failed missing:", "WROTE open"}
	}
	script := filepath.Join(dir, "try")
	must(t, os.WriteFile(script, []byte(refusals(results, writes)), 0o700))
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, err := os.ReadFile(results)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(got) != len(want) {
		t.Fatalf("results:\n%s", b)
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i], w) {
			t.Errorf("%q, want it to start %q", got[i], w)
		}
	}
}
