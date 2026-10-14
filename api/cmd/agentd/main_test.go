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
