package approvals_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/server"
)

func TestDirectCDRequiresApproval(t *testing.T) {
	h := newPlatform(t)
	// C: backup. D: drain. Both must refuse without an executed approval.
	for _, tc := range []struct {
		method, path, body, action, token string
	}{
		{http.MethodPost, "/api/v1/cluster/postgres/backup", "", "trigger_cnpg_backup", "operator-test-token"},
		{http.MethodPost, "/api/v1/cluster/nodes/node-a/drain", `{"force":true}`, "drain_node", "admin-test-token"},
		{http.MethodPost, "/api/v1/cluster/workloads/scale", `{"namespace":"bifrost-dev","kind":"Deployment","name":"api","replicas":2}`, "scale_deployment", "operator-test-token"},
		{http.MethodPost, "/api/v1/network/firewall/apply", `{}`, "unifi_firewall_apply", "operator-test-token"},
	} {
		rec := call(t, h, tc.method, tc.path, tc.body, tc.token)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"error":"approval required"`) || !strings.Contains(rec.Body.String(), tc.action) {
			t.Fatalf("%s %s = %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	// ib_mode is C. ib_self_heal is B (PROD already runs that loop).
	mode := call(t, h, http.MethodPost, "/api/v1/plugins/ib-gateway/control/mode", `{"mode":"paper"}`, "operator-test-token")
	if mode.Code != http.StatusForbidden || !strings.Contains(mode.Body.String(), `"action":"ib_mode"`) {
		t.Fatalf("ib_mode = %d %s", mode.Code, mode.Body.String())
	}
	selfHeal := call(t, h, http.MethodPost, "/api/v1/plugins/ib-gateway/control/self-heal", `{"enabled":false}`, "operator-test-token")
	if strings.Contains(selfHeal.Body.String(), "approval required") {
		t.Fatalf("ib_self_heal was approval-gated: %d %s", selfHeal.Code, selfHeal.Body.String())
	}
	// B stays on the direct path (the handler may fail closed on the missing cluster).
	wake := call(t, h, http.MethodPost, "/api/v1/cluster/nodes/node-a/wake", "", "operator-test-token")
	if strings.Contains(wake.Body.String(), "approval required") {
		t.Fatalf("B-tier wake was approval-gated: %d %s", wake.Code, wake.Body.String())
	}
	// Daemon scale-up is X: the direct call must reach the handler (D10), not the approval gate.
	up := call(t, h, http.MethodPost, "/api/v1/cluster/workloads/scale", `{"namespace":"bifrost-dev","kind":"Deployment","name":"daemon","replicas":2}`, "operator-test-token")
	if strings.Contains(up.Body.String(), "approval required") {
		t.Fatalf("daemon scale-up was replaced with the approval gate: %d %s", up.Code, up.Body.String())
	}
}

func TestExecutedApprovalUsesStoredParamsOnce(t *testing.T) {
	h := newPlatform(t)
	var calls int
	var got map[string]any
	actions.RegisterExecutor("cordon_node", func(_ context.Context, params map[string]any) (any, error) {
		calls++
		got = params
		return map[string]any{"ok": true}, nil
	})
	rec := call(t, h, http.MethodPost, "/api/v1/approvals", `{"action":"cordon_node","params":{"name":"node-a"},"reason":"patch","rollback":"uncordon"}`, "operator-test-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// The approve body tries to swap the node. Stored params must win.
	rec = call(t, h, http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", `{"channel":"console","params":{"name":"node-b"}}`, "operator-test-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("operator approve = %d %s", rec.Code, rec.Body.String())
	}
	rec = call(t, h, http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", `{"channel":"console","params":{"name":"node-b"}}`, "admin-test-token")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"executed"`) {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	if calls != 1 || got["name"] != "node-a" {
		t.Fatalf("executor calls=%d params=%#v", calls, got)
	}
	rec = call(t, h, http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", `{"channel":"chat"}`, "admin-test-token")
	if rec.Code != http.StatusConflict || calls != 1 {
		t.Fatalf("second approve = %d calls=%d %s", rec.Code, calls, rec.Body.String())
	}
	// The executor above is the internal path. The same params stay 403 on the
	// direct endpoint; one execution does not unlock later calls.
	same := call(t, h, http.MethodPost, "/api/v1/cluster/nodes/node-a/cordon", "", "operator-test-token")
	if same.Code != http.StatusForbidden || !strings.Contains(same.Body.String(), "approval required") || calls != 1 {
		t.Fatalf("direct call after execution = %d calls=%d %s", same.Code, calls, same.Body.String())
	}
	other := call(t, h, http.MethodPost, "/api/v1/cluster/nodes/node-b/cordon", "", "operator-test-token")
	if other.Code != http.StatusForbidden || !strings.Contains(other.Body.String(), "approval required") {
		t.Fatalf("other node = %d %s", other.Code, other.Body.String())
	}
}

func TestCreateNotifiesAndSurvivesRelayFailure(t *testing.T) {
	h := newPlatform(t)
	var hits int
	var got struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		ClickURL string `json:"click_url"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") == "" {
			t.Errorf("notify missing authorization")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode notify: %v", err)
		}
		if hits == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "notify-test-token")

	rec := call(t, h, http.MethodPost, "/api/v1/approvals", `{"action":"cordon_node","params":{"name":"node-a"},"reason":"patch","rollback":"uncordon"}`, "operator-test-token")
	if rec.Code != http.StatusCreated || hits != 1 {
		t.Fatalf("create with notify = %d hits=%d %s", rec.Code, hits, rec.Body.String())
	}
	if got.Title != "#1 · C · cordon_node · node" || !strings.Contains(got.Message, "name=node-a") ||
		!strings.Contains(got.Message, "cursor-b1") || !strings.Contains(got.ClickURL, "#approvals?id=") {
		t.Fatalf("notify body = %+v", got)
	}
	if strings.Contains(got.Message, "notify-test-token") {
		t.Fatal("notify message contains the token")
	}

	rec = call(t, h, http.MethodPost, "/api/v1/approvals", `{"action":"cordon_node","params":{"name":"node-b"},"reason":"patch","rollback":"uncordon"}`, "operator-test-token")
	if rec.Code != http.StatusCreated || hits != 2 {
		t.Fatalf("create after relay failure = %d hits=%d %s", rec.Code, hits, rec.Body.String())
	}

	// TD-286: each record shows what happened to its push.
	for _, tc := range []struct{ ref, result string }{{"1", "accepted"}, {"2", "failed"}} {
		rec = call(t, h, http.MethodGet, "/api/v1/approvals/"+tc.ref, "", "viewer-test-token")
		var a struct {
			Number     int `json:"number"`
			Deliveries []struct {
				Kind, Channel, Result, Error string
			} `json:"deliveries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("get #%s = %d %s", tc.ref, rec.Code, rec.Body.String())
		}
		if len(a.Deliveries) != 1 || a.Deliveries[0].Kind != "created" || a.Deliveries[0].Channel != "ntfy" || a.Deliveries[0].Result != tc.result {
			t.Fatalf("#%s deliveries = %s", tc.ref, rec.Body.String())
		}
		if tc.result == "failed" && !strings.Contains(a.Deliveries[0].Error, "502") {
			t.Fatalf("#%s failure reason = %s", tc.ref, rec.Body.String())
		}
	}
}

func TestXTierCreateForbidden(t *testing.T) {
	h := newPlatform(t)
	rec := call(t, h, http.MethodPost, "/api/v1/approvals", `{"action":"scale_deployment","params":{"namespace":"bifrost-dev","kind":"Deployment","name":"daemon","replicas":2},"reason":"grow"}`, "operator-test-token")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"tier":"X"`) {
		t.Fatalf("X create = %d %s", rec.Code, rec.Body.String())
	}
}

func call(t *testing.T, h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Bifrost-Session", "cursor-b1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newPlatform(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "programs"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"environments.yaml":  "environments:\n  - id: dev\n    label: Dev\n    nginx_base: http://127.0.0.1:8080\n",
		"topology.yaml":      "deployment_phase: k3s_partial\nnodes:\n  - id: node-a\n    label: Node A\n    host: 10.0.0.1\n    group: linux\n    grid: { row: 1, col: 2 }\nedges: []\n",
		"clusters.yaml":      "clusters:\n  - id: test-cluster\n    label: Test Cluster\n    distribution: k3s\n    api_server: https://10.0.0.1:6443\n    node_ip: 10.0.0.1\n",
		"ops-context.yaml":   "meta:\n  version: \"v1\"\n  catalog_version: \"v1\"\ndeployment:\n  phase: k3s_partial\nfocus:\n  headline: \"test focus\"\nmilestones:\n  - id: m1\n    status: SIGNED\n",
		"platform-auth.yaml": "tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n  - name: operator\n    role: operator\n    token: operator-test-token\n  - name: admin\n    role: admin\n    token: admin-test-token\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, "programs", "smoke-test.yaml"), []byte("id: smoke-test\ntitle: Smoke\nstatus: active\nphases:\n  - id: p1\n    title: Phase 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_CONFIG", filepath.Join(configDir, "environments.yaml"))
	t.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("PLATFORM_AUTH_CONFIG", filepath.Join(configDir, "platform-auth.yaml"))
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", filepath.Join(dir, "remediation-jobs"))
	t.Setenv("PLATFORM_AUDIT_LOG", filepath.Join(dir, "audit.json"))
	t.Setenv("PLATFORM_KUBECONFIG", filepath.Join(dir, "does-not-exist-kubeconfig.yaml"))
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	t.Setenv("PLATFORM_ROLE", "api")
	t.Setenv("PLATFORM_STATE_BACKEND", "file")
	t.Setenv("PATROL_DISPATCH", "stub")
	t.Setenv("PATROL_STATE_DIR", filepath.Join(dir, "patrol-state"))
	t.Setenv("PATROL_SKILLS_DIR", filepath.Join(configDir, "patrol-skills"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Router()
}
