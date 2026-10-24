package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolsClaude(t *testing.T) {
	got, err := tools("exec:claude=anthropic:opus")
	if err != nil || len(got) != 1 || got[0].Executor != "exec:claude" || got[0].Provider != "anthropic" || got[0].Model != "opus" {
		t.Fatalf("tools = %+v, %v", got, err)
	}
}

func TestConfigReadsClaudeTokenFile(t *testing.T) {
	dir := t.TempDir()
	hostToken, claudeToken := filepath.Join(dir, "host.token"), filepath.Join(dir, "claude.token")
	if err := os.WriteFile(hostToken, []byte("host-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"AGENTD_PLATFORM": "https://p.example/api/v1/agentrt", "AGENTD_HOST": "h1", "AGENTD_TOKEN_FILE": hostToken,
		"AGENTD_TOOLS": "exec:codex=openai", "AGENTD_ROOT": dir, "AGENTD_GIT_BASE": "https://g.example/bifrost",
		"AGENTD_GIT_TOKEN_FILE": "", "AGENTD_CURSOR_KEY_FILE": "", "AGENTD_CLAUDE_TOKEN_FILE": "",
	} {
		t.Setenv(k, v)
	}
	if cfg, err := config(); err != nil || cfg.claudeToken != "" || len(cfg.tools) != 1 || cfg.tools[0].Executor != "exec:codex" {
		t.Fatalf("optional token or tools changed: token %q, tools %+v, %v", cfg.claudeToken, cfg.tools, err)
	}
	t.Setenv("AGENTD_CLAUDE_TOKEN_FILE", claudeToken)
	for _, tc := range []struct {
		name, body string
		mode       os.FileMode
		wantErr    string
	}{
		{"valid", " oauth-fixture \n", 0o600, ""},
		{"open", "oauth-fixture", 0o644, "readable by others"},
		{"empty", " \n", 0o600, "is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(claudeToken, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(claudeToken, tc.mode); err != nil {
				t.Fatal(err)
			}
			cfg, err := config()
			if tc.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), "AGENTD_CLAUDE_TOKEN_FILE:") || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("invalid token file: %v", err)
				}
			} else if err != nil || cfg.claudeToken != "oauth-fixture" || cfg.tools[0].Executor != "exec:codex" {
				t.Fatalf("token file: %q, tools %+v, %v", cfg.claudeToken, cfg.tools, err)
			}
		})
	}
	t.Setenv("AGENTD_CLAUDE_TOKEN_FILE", filepath.Join(dir, "missing"))
	if _, err := config(); err == nil || !strings.HasPrefix(err.Error(), "AGENTD_CLAUDE_TOKEN_FILE:") {
		t.Fatalf("missing token file: %v", err)
	}
}
