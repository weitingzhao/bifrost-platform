package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Serial: child main reads process environment and exits via log.Fatalf.
func TestDuplicateSeatProcessStartup(t *testing.T) {
	if os.Getenv("PLATFORM_TEST_DUPLICATE_SEAT_CHILD") == "1" {
		main()
		return
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"environments.yaml":  "environments:\n  - {id: dev, label: Dev, nginx_base: 'http://127.0.0.1:8080'}\n",
		"topology.yaml":      "deployment_phase: k3s_partial\nnodes:\n  - {id: test, label: Test, host: 127.0.0.1, group: linux}\nedges: []\n",
		"clusters.yaml":      "clusters:\n  - {id: test, label: Test, distribution: k3s, api_server: 'https://127.0.0.1:6443', node_ip: 127.0.0.1}\n",
		"ops-context.yaml":   "meta: {version: v1, catalog_version: v1}\ndeployment: {phase: k3s_partial}\nfocus: {headline: test}\nmilestones:\n  - {id: m1, status: SIGNED}\n",
		"platform-auth.yaml": "tokens:\n  - {name: 'seat:coordinator', role: reporter, token: fixture-a}\n  - {name: 'seat:coordinator', role: reporter, token: fixture-b}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDuplicateSeatProcessStartup$")
	cmd.Dir = dir // no repository .env is loaded
	cmd.Env = append(os.Environ(),
		"PLATFORM_TEST_DUPLICATE_SEAT_CHILD=1", "PLATFORM_ROLE=api", "PLATFORM_STATE_BACKEND=file",
		"AGENTRT_DSN=", "PLATFORM_DATA_DIR="+filepath.Join(dir, "data"), "PLATFORM_LISTEN=127.0.0.1:0",
		"PLATFORM_CONFIG="+filepath.Join(dir, "environments.yaml"),
		"PLATFORM_TOPOLOGY="+filepath.Join(dir, "topology.yaml"),
		"PLATFORM_CLUSTERS="+filepath.Join(dir, "clusters.yaml"),
		"PLATFORM_OPS_CONTEXT="+filepath.Join(dir, "ops-context.yaml"),
		"PLATFORM_AUTH_CONFIG="+filepath.Join(dir, "platform-auth.yaml"))
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 ||
		!strings.Contains(string(out), "server: platform startup:") || !strings.Contains(string(out), "is named twice") ||
		strings.Contains(string(out), "listening on") {
		t.Fatalf("startup must exit 1 before serving: %v, context=%v\n%s", err, ctx.Err(), out)
	}
}
