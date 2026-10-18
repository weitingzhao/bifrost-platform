package agentd

import (
	"os"
	"path/filepath"
	"slices"
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
			for flag, value := range map[string]string{
				"--output-format": "stream-json", "--mcp-config": `{"mcpServers":{}}`,
				"--allowedTools": "Read,Grep,Glob", "--tools": "Read,Grep,Glob",
				"--disallowedTools": "Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch",
				"--setting-sources": "", "--settings": `{"disableAllHooks":true}`,
			} {
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
				err := streamResultReport(vendor, log, report)
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
