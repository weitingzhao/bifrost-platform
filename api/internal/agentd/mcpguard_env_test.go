package agentd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review 196 A1: the codex MCP probe runs with the allowed environment only,
// so a secret in agentd's environment does not reach codex before any task.
func TestCodexProbeGetsNoHostSecret(t *testing.T) {
	bin, dump := t.TempDir(), filepath.Join(t.TempDir(), "env")
	script := "#!/bin/sh\nenv > '" + dump + "'\necho '[]'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("CURSOR_API_KEY", "host-cursor-key")
	t.Setenv("HOST_SECRET_FIXTURE", "host-secret")
	n, err := CodexMCP()
	if err != nil || n != 0 {
		t.Fatalf("CodexMCP = %d, %v", n, err)
	}
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	env := string(b)
	for _, secret := range []string{"host-cursor-key", "host-secret"} {
		if strings.Contains(env, secret) {
			t.Fatalf("the probe saw %q:\n%s", secret, env)
		}
	}
	if !strings.Contains(env, "PATH="+bin) {
		t.Fatalf("the probe lost its PATH:\n%s", env)
	}
}

// Review 196 B1: the launcher's helpers (pgrep, ps) get the allowed
// environment, not the executor's.
func TestHelpersGetTheAllowedEnvironment(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "k")
	cmd := helper("ps", "-o", "stat=", "-p", "1")
	if cmd.Env == nil {
		t.Fatal("a helper inherits the whole environment (nil Env)")
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "CURSOR_API_KEY=") {
			t.Fatalf("a helper got %s", kv)
		}
	}
}
