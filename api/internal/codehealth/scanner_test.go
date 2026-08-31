package codehealth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCommitsMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc1234", "abc1234", true},
		{"abc1234", "abc1234567890", true},
		{"ABC1234", "abc1234", true},
		{"abc1234", "def5678", false},
		{"", "abc", false},
		{"unknown", "abc", false},
	}
	for _, tc := range cases {
		if got := commitsMatch(tc.a, tc.b); got != tc.want {
			t.Errorf("commitsMatch(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLooksLikeWorkspace(t *testing.T) {
	root := t.TempDir()
	if looksLikeWorkspace(root) {
		t.Fatal("empty dir should not look like workspace")
	}
	scriptDir := filepath.Join(root, "bifrost-trade-infra", "agent-config", "scripts", "code-health")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "scan.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if looksLikeWorkspace(root) {
		t.Fatal("scan.sh alone without measured repos should fail")
	}
	if err := os.MkdirAll(filepath.Join(root, "bifrost-platform"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikeWorkspace(root) {
		t.Fatal("expected workspace with scan.sh + bifrost-platform")
	}
}

func writeFakeScan(t *testing.T, dir, commit string, exitCode int) string {
	t.Helper()
	script := filepath.Join(dir, "fake-scan.sh")
	body := `#!/usr/bin/env bash
set -euo pipefail
cat <<'EOF'
{"generated_at":"2026-08-31T12:00:00Z","commit":"` + commit + `","source":"local","metrics":[{"id":"code.duplication.satellite","label":"dup","domain":"satellite","repo":"bifrost-trade-frontend","value":8,"baseline":12,"status":"improved","baseline_var":"DUPLICATION_SATELLITE_BASELINE"}]}
EOF
exit ` + strconv.Itoa(exitCode) + `
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func setupMiniWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	scriptDir := filepath.Join(ws, "bifrost-trade-infra", "agent-config", "scripts", "code-health")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "scan.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "bifrost-platform"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestRunLiveScanAcceptsExit1Over(t *testing.T) {
	ws := setupMiniWorkspace(t)
	fake := writeFakeScan(t, t.TempDir(), "deadbeef", 1)
	t.Setenv("BIFROST_WORKSPACE_ROOT", ws)
	t.Setenv("PLATFORM_CODE_HEALTH_SCAN_SH", fake)

	rep, _, err := runLiveScan(context.Background())
	if err != nil {
		t.Fatalf("runLiveScan: %v", err)
	}
	if rep.Commit != "deadbeef" {
		t.Fatalf("commit=%q", rep.Commit)
	}
	if rep.Source != "live-rescan" {
		t.Fatalf("source=%q", rep.Source)
	}
	if len(rep.Metrics) != 1 || rep.Metrics[0].Value != 8 {
		t.Fatalf("metrics=%+v", rep.Metrics)
	}
}

func TestRunLiveScanRejectsExit2(t *testing.T) {
	ws := setupMiniWorkspace(t)
	fake := writeFakeScan(t, t.TempDir(), "badc0de", 2)
	t.Setenv("BIFROST_WORKSPACE_ROOT", ws)
	t.Setenv("PLATFORM_CODE_HEALTH_SCAN_SH", fake)

	_, _, err := runLiveScan(context.Background())
	if err == nil {
		t.Fatal("expected error for exit 2")
	}
}

func TestHandleRescanStoresReading(t *testing.T) {
	t.Setenv("PLATFORM_CODE_HEALTH_DIR", t.TempDir())
	ws := setupMiniWorkspace(t)
	fake := writeFakeScan(t, t.TempDir(), "cafebabe", 0)
	t.Setenv("BIFROST_WORKSPACE_ROOT", ws)
	t.Setenv("PLATFORM_CODE_HEALTH_SCAN_SH", fake)

	h := NewHandler(nil)
	rec := httptest.NewRecorder()
	h.HandleRescan(rec, httptest.NewRequest(http.MethodPost, "/code-health/rescan", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["commit"] != "cafebabe" {
		t.Fatalf("body=%v", body)
	}
	got, ok := h.store.Latest()
	if !ok || got == nil || got.Commit != "cafebabe" || got.Source != "live-rescan" {
		t.Fatalf("stored=%+v ok=%v", got, ok)
	}
}

func TestGetIncludesFreshness(t *testing.T) {
	t.Setenv("PLATFORM_CODE_HEALTH_DIR", t.TempDir())
	t.Setenv("BIFROST_WORKSPACE_ROOT", "/nonexistent-workspace-xyz")
	h := NewHandler(nil)
	raw, _ := json.Marshal(sampleReport("oldcommit", 12))
	put := httptest.NewRecorder()
	h.HandleReport(put, httptest.NewRequest(http.MethodPost, "/code-health/report", bytes.NewReader(raw)))
	if put.Code != http.StatusOK {
		t.Fatalf("put status=%d", put.Code)
	}

	rec := httptest.NewRecorder()
	h.HandleGet(rec, httptest.NewRequest(http.MethodGet, "/code-health", nil))
	var resp StatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Freshness == nil {
		t.Fatal("Freshness missing")
	}
	if resp.Freshness.RescanAvailable {
		t.Fatal("rescan should be unavailable for bad workspace")
	}
	if resp.Freshness.ReadingCommit != "oldcommit" {
		t.Fatalf("ReadingCommit=%q", resp.Freshness.ReadingCommit)
	}
}
