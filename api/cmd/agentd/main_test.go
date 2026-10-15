package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTools(t *testing.T) {
	got, err := tools("exec:codex=openai:gpt-5, exec:cursor=cursor-mix, exec:deterministic=local")
	if err != nil || len(got) != 3 || got[0].Model != "gpt-5" || got[1].Provider != "cursor-mix" || got[1].Model != "" {
		t.Fatalf("tools = %+v %v", got, err)
	}
	for _, bad := range []string{"", "exec:codex", "exec:codex=", "=openai"} {
		if _, err := tools(bad); err == nil {
			t.Errorf("took %q", bad)
		}
	}
}

func TestSecretRefusesAnOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("fixture-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := secret(path); err == nil {
		t.Fatal("read a token others can read")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, err := secret(path); err != nil || tok != "fixture-token" {
		t.Fatalf("secret = %q %v", tok, err)
	}
	if _, err := secret(""); err == nil {
		t.Fatal("no file read as a token")
	}
}

func TestConfigReadsWhereBranchesGo(t *testing.T) {
	token := filepath.Join(t.TempDir(), "agentd.token")
	if err := os.WriteFile(token, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"AGENTD_PLATFORM": "https://p.example/api/v1/agentrt", "AGENTD_HOST": "h1",
		"AGENTD_TOKEN_FILE": token, "AGENTD_TOOLS": "exec:codex=openai", "AGENTD_GIT_BASE": "https://g.example/bifrost",
		"AGENTD_GIT_TOKEN_FILE": "", "AGENTD_ROOT": t.TempDir(), "AGENTD_GIT_PUSH_BASE": " https://g.example/bifrost-agents/ "} {
		t.Setenv(k, v)
	}
	cfg, err := config()
	if err != nil {
		t.Fatal(err)
	}
	if got := remoteAt(cfg.pushBase)("bifrost-platform"); got != "https://g.example/bifrost-agents/bifrost-platform.git" {
		t.Fatalf("push remote = %q", got)
	}
	t.Setenv("AGENTD_GIT_PUSH_BASE", "")
	if cfg, err = config(); err != nil || remoteAt(cfg.pushBase) != nil {
		t.Fatalf("no push base: %q %v", cfg.pushBase, err)
	}
}

func TestPushBaseStaysOnTheGitServer(t *testing.T) {
	for _, c := range []struct {
		git, push string
		ok        bool
	}{
		{"https://g.example/bifrost", "", true},
		{"https://g.example/bifrost", "https://G.example/bifrost-agents", true},
		{"http://192.168.10.73:30300/bifrost", "http://192.168.10.73:30300/bifrost-agents", true},
		{"https://g.example/bifrost", "https://evil.example/bifrost-agents", false},
		{"https://g.example/bifrost", "http://g.example/bifrost-agents", false},
		{"http://192.168.10.73:30300/bifrost", "http://192.168.10.73:30301/bifrost-agents", false},
		{"https://g.example/bifrost", "/srv/agents", false},
	} {
		if err := sameServer(c.git, c.push); (err == nil) != c.ok {
			t.Errorf("%q %q: %v", c.git, c.push, err)
		}
	}
}
