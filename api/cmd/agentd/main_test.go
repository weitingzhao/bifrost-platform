package main

import (
	"os"
	"path/filepath"
	"strings"
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

// The push base is gone (cut §9.8): Gitea is the primary, so attempt
// branches go back to where work is cloned from. A host whose settings still
// name one, even on another server (refused before, review 195 B), starts,
// and the setting is not read.
func TestConfigIgnoresALeftoverPushBase(t *testing.T) {
	token := filepath.Join(t.TempDir(), "agentd.token")
	if err := os.WriteFile(token, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"AGENTD_PLATFORM": "https://p.example/api/v1/agentrt", "AGENTD_HOST": "h1",
		"AGENTD_TOKEN_FILE": token, "AGENTD_TOOLS": "exec:codex=openai", "AGENTD_GIT_BASE": "https://g.example/bifrost",
		"AGENTD_GIT_TOKEN_FILE": "", "AGENTD_ROOT": t.TempDir(), "AGENTD_GIT_PUSH_BASE": "https://evil.example/bifrost-agents"} {
		t.Setenv(k, v)
	}
	cfg, err := config()
	if err != nil {
		t.Fatalf("a leftover AGENTD_GIT_PUSH_BASE stops agentd: %v", err)
	}
	if cfg.gitBase != "https://g.example/bifrost" {
		t.Fatalf("git base = %q", cfg.gitBase)
	}
}

func TestConfigReadsCursorsKeyFile(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "agentd.token")
	if err := os.WriteFile(token, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"AGENTD_PLATFORM": "https://p.example/api/v1/agentrt", "AGENTD_HOST": "h1",
		"AGENTD_TOKEN_FILE": token, "AGENTD_TOOLS": "exec:cursor=cursor-mix", "AGENTD_GIT_BASE": "https://g.example/bifrost",
		"AGENTD_GIT_TOKEN_FILE": "", "AGENTD_ROOT": t.TempDir(), "AGENTD_CURSOR_KEY_FILE": ""} {
		t.Setenv(k, v)
	}
	if cfg, err := config(); err != nil || cfg.cursorKey != "" {
		t.Fatalf("no key file: %q %v", cfg.cursorKey, err)
	}
	key := filepath.Join(dir, "cursor-api.key")
	if err := os.WriteFile(key, []byte(" key_abc \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTD_CURSOR_KEY_FILE", key)
	if cfg, err := config(); err != nil || cfg.cursorKey != "key_abc" {
		t.Fatalf("key file: %q %v", cfg.cursorKey, err)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config(); err == nil {
		t.Fatal("a key file others can read was taken")
	}
	if err := os.WriteFile(key, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config(); err == nil {
		t.Fatal("an empty key file was taken")
	}
	t.Setenv("AGENTD_CURSOR_KEY_FILE", filepath.Join(dir, "missing"))
	if _, err := config(); err == nil {
		t.Fatal("a missing key file was taken")
	}
}

// Review 196 B2: a link or an empty file is not a credential.
func TestSecretRefusesALinkAndAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := secret(link); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a link: %v", err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := secret(empty); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("an empty file: %v", err)
	}
}

// Review 197 A1: the load check's sysctl gets the allowed environment, not
// agentd's.
func TestExecOutputGetsNoHostSecret(t *testing.T) {
	bin, dump := t.TempDir(), filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(filepath.Join(bin, "sysctl"), []byte("#!/bin/sh\nenv > '"+dump+"'\necho '{ 1.00 1.00 1.00 }'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("CURSOR_API_KEY", "host-cursor-key")
	t.Setenv("HOST_SECRET_FIXTURE", "host-secret")
	if _, err := execOutput("sysctl", "-n", "vm.loadavg"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"host-cursor-key", "host-secret"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("sysctl saw %q", secret)
		}
	}
	if !strings.Contains(string(b), "PATH="+bin) {
		t.Fatalf("sysctl lost its PATH:\n%s", b)
	}
}
