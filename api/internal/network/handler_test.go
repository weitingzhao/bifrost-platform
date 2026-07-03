package network_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/network"
	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

func mockUnifiServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Set-Cookie", "TOKEN=test; Path=/")
			w.Header().Set("X-Csrf-Token", "csrf")
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"}}`))
		case "/proxy/network/api/s/default/stat/sysinfo":
			_, _ = w.Write([]byte(`{"data":[{"version":"10.4.57"}]}`))
		case "/proxy/network/api/s/default/rest/networkconf":
			_, _ = w.Write([]byte(`{"data":[{"name":"Server","purpose":"corporate","_id":"net1","vlan":10}]}`))
		case "/proxy/network/v2/api/site/default/firewall/zone":
			_, _ = w.Write([]byte(`{"data":[{"name":"Bifrost Server","network_ids":["net1"],"_id":"z1"}]}`))
		case "/proxy/network/v2/api/site/default/firewall-policies":
			_, _ = w.Write([]byte(`[{"name":"Bifrost | ALLOW Work → Server","action":"ALLOW"}]`))
		case "/proxy/network/api/s/default/stat/device":
			_, _ = w.Write([]byte(`{"data":[{"name":"ucg","type":"ugw"}]}`))
		case "/proxy/network/api/s/default/stat/sta":
			_, _ = w.Write([]byte(`{"data":[{"hostname":"laptop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")
	return srv, host
}

func TestHandleStatus(t *testing.T) {
	srv, host := mockUnifiServer(t)
	h := network.NewHandler(nil, network.WithDial(func(ctx context.Context) (*unifi.Client, error) {
		c := unifi.New(unifi.Config{Host: host, User: "agent", Pass: "secret", Site: "default"})
		c.SetHTTPClient(srv.Client())
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		return c, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/status", nil)
	rec := httptest.NewRecorder()
	h.HandleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["controller_version"] != "10.4.57" {
		t.Fatalf("unexpected version: %v", payload["controller_version"])
	}
}

func TestHandleAuditDriftOnPartialMock(t *testing.T) {
	srv, host := mockUnifiServer(t)
	h := network.NewHandler(nil, network.WithDial(func(ctx context.Context) (*unifi.Client, error) {
		c := unifi.New(unifi.Config{Host: host, User: "agent", Pass: "secret", Site: "default"})
		c.SetHTTPClient(srv.Client())
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		return c, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/audit", nil)
	rec := httptest.NewRecorder()
	h.HandleAudit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["auth_mode"] != "SESSION_PATH" {
		t.Fatalf("auth_mode: %v", payload["auth_mode"])
	}
	if payload["classification"] != "POLICY_DRIFT" {
		t.Fatalf("expected drift on partial mock: %v", payload["classification"])
	}
}

func TestHandleFirewallApply(t *testing.T) {
	h := network.NewHandler(nil, network.WithApplyFirewall(func(ctx context.Context, includeDefaultDeny bool) (map[string]any, error) {
		return map[string]any{
			"executor":             "mock",
			"include_default_deny": includeDefaultDeny,
			"stdout":               "Done.",
		}, nil
	}))

	body := strings.NewReader(`{"include_default_deny":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/firewall/apply", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleFirewallApply(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["action"] != "network.firewall.apply" {
		t.Fatalf("action: %v", payload["action"])
	}
	if payload["autonomy"] != "L1" {
		t.Fatalf("autonomy: %v", payload["autonomy"])
	}
}
