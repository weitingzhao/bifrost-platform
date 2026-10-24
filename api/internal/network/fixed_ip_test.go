package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

// This controller lives entirely in memory: no listeners, sockets or credentials.
type fixedIPController struct {
	t              *testing.T
	known, current []ClientRecord
	devices        []ClientRecord
	failStatus     int
	writes         int
	mismatch       string
	failPath       string
	failBody       string
	writeBody      string
	lastBody       map[string]any
}

func (f *fixedIPController) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/proxy/network/api/s/default")
	if r.URL.Path == "/api/auth/login" {
		rec.Header().Set("Set-Cookie", "TOKEN=fake; Path=/")
		rec.Header().Set("X-CSRF-Token", "fake-csrf")
		_, _ = rec.WriteString(`{}`)
		return rec.Result(), nil
	}
	if r.Header.Get("Cookie") != "TOKEN=fake" || r.Header.Get("X-CSRF-Token") != "fake-csrf" {
		f.t.Fatal("missing session or CSRF")
	}
	if f.failPath == path && r.Method == http.MethodGet {
		if f.failStatus != 0 {
			rec.WriteHeader(f.failStatus)
		}
		_, _ = rec.WriteString(f.failBody)
		return rec.Result(), nil
	}
	var data []ClientRecord
	switch {
	case r.Method == http.MethodGet && path == "/rest/user":
		data = f.known
	case r.Method == http.MethodGet && path == "/stat/sta":
		data = f.current
	case r.Method == http.MethodGet && path == "/stat/device":
		data = f.devices
	case r.Method == http.MethodPut && path == "/rest/user/client-1":
		f.writes++
		if err := json.NewDecoder(r.Body).Decode(&f.lastBody); err != nil {
			f.t.Fatal(err)
		}
		if len(f.lastBody) != 3 || f.lastBody["use_fixedip"] != true || f.lastBody["fixed_ip"] == nil || f.lastBody["name"] == nil {
			f.t.Fatalf("unsafe write: %#v", f.lastBody)
		}
		f.known[0].UseFixedIP = true
		f.known[0].FixedIP, _ = f.lastBody["fixed_ip"].(string)
		f.known[0].Name, _ = f.lastBody["name"].(string)
		if f.writeBody != "" {
			_, _ = rec.WriteString(f.writeBody)
			return rec.Result(), nil
		}
		data = []ClientRecord{f.known[0]}
	case r.Method == http.MethodGet && path == "/rest/user/client-1":
		after := f.known[0]
		switch f.mismatch {
		case "ip":
			after.FixedIP = "192.168.10.90"
		case "name":
			after.Name = "wrong"
		case "flag":
			after.UseFixedIP = false
		case "mac":
			after.MAC = "00:11:22:33:44:55"
		case "id":
			after.ID = "wrong"
		}
		data = []ClientRecord{after}
		if f.mismatch == "two" {
			data = append(data, after)
		}
		if f.mismatch == "empty" {
			data = []ClientRecord{}
		}
	default:
		f.t.Fatalf("unexpected controller request %s %s", r.Method, r.URL.Path)
	}
	_ = json.NewEncoder(rec).Encode(map[string]any{"meta": map[string]string{"rc": "ok"}, "data": data})
	return rec.Result(), nil
}

func newFixedIPController(t *testing.T, cfg unifi.Config) (*fixedIPController, ServiceOption) {
	t.Helper()
	f := &fixedIPController{t: t, known: []ClientRecord{{ID: "client-1", MAC: "68:1d:ef:5f:29:dd", IP: "192.168.10.41", Name: "old-name"}}, current: []ClientRecord{}, devices: []ClientRecord{}}
	cfg.Host, cfg.Site = "controller.invalid", "default"
	client := unifi.New(cfg)
	client.SetHTTPClient(&http.Client{Transport: f})
	return f, WithDial(func(context.Context) (*unifi.Client, error) { return client, nil })
}

func fixedIPRequest() FixedIPRequest {
	return FixedIPRequest{MAC: "68:1d:ef:5f:29:dd", IP: "192.168.10.80", Name: "ubt-k3s-07"}
}

func TestFixedIPSuccessNoOpAndRead(t *testing.T) {
	f, opt := newFixedIPController(t, unifi.Config{})
	s := NewService(opt)
	req := fixedIPRequest()
	req.MAC = strings.ToUpper(req.MAC)
	out, err := s.SetClientFixedIP(context.Background(), req)
	if err != nil || f.writes != 1 || out["no_op"] != false {
		t.Fatalf("out=%#v err=%v writes=%d", out, err, f.writes)
	}
	if out["before"].(ClientRecord).Name != "old-name" || out["after"].(ClientRecord).FixedIP != req.IP {
		t.Fatalf("before/after=%#v", out)
	}
	out, err = s.SetClientFixedIP(context.Background(), req)
	if err != nil || f.writes != 1 || out["no_op"] != true {
		t.Fatalf("no-op=%#v err=%v writes=%d", out, err, f.writes)
	}
	h := NewHandler(nil, opt)
	router := chi.NewRouter()
	router.Get("/clients/{mac}", h.HandleClientByMAC)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/clients/"+req.MAC, nil))
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got["fixed_ip"] != req.IP || got["name"] != req.Name || len(got) != 6 {
		t.Fatalf("read=%d %s", rec.Code, rec.Body.String())
	}
}

func TestFixedIPRefusalsDoNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(*FixedIPRequest, *fixedIPController, *unifi.Config)
	}{

		{"device-conflict", "device", func(r *FixedIPRequest, f *fixedIPController, _ *unifi.Config) { f.devices = []ClientRecord{{IP: r.IP}} }},
		{"subnet-host-bits", "UNIFI_SERVER_SUBNET", func(_ *FixedIPRequest, _ *fixedIPController, c *unifi.Config) { c.ServerSubnet = "192.168.10.5/24" }},
		{"subnet-31", "UNIFI_SERVER_SUBNET", func(_ *FixedIPRequest, _ *fixedIPController, c *unifi.Config) { c.ServerSubnet = "192.168.10.0/31" }},
		{"gateway-broadcast", "UNIFI_SERVER_GATEWAY", func(_ *FixedIPRequest, _ *fixedIPController, c *unifi.Config) { c.ServerGateway = "192.168.10.255" }},
		{"custom-network", "network or broadcast", func(r *FixedIPRequest, _ *fixedIPController, c *unifi.Config) {
			c.ServerSubnet = "10.20.30.0/23"
			c.ServerGateway = "10.20.30.1"
			r.IP = "10.20.30.0"
		}},
		{"custom-broadcast", "network or broadcast", func(r *FixedIPRequest, _ *fixedIPController, c *unifi.Config) {
			c.ServerSubnet = "10.20.30.0/23"
			c.ServerGateway = "10.20.30.1"
			r.IP = "10.20.31.255"
		}},
		{"mac", "48-bit", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.MAC = "nope" }},
		{"eui64", "48-bit", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.MAC = "00:11:22:33:44:55:66:77" }},
		{"missing", "not found", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.MAC = "00:11:22:33:44:55" }},
		{"ipv6", "IPv4", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.IP = "::1" }},
		{"invalid-ip", "IPv4", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.IP = "192.168.10.999" }},
		{"outside", "inside server subnet", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.IP = "192.168.20.80" }},
		{"network", "network or broadcast", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.IP = "192.168.10.0" }},
		{"broadcast", "network or broadcast", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.IP = "192.168.10.255" }},
		{"gateway", "gateway", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.IP = "192.168.10.1" }},
		{"name-empty", "host name", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.Name = "" }},
		{"name-long", "host name", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.Name = strings.Repeat("a", 64) }},
		{"name-pattern", "host name", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.Name = "bad_name" }},
		{"name-leading-hyphen", "host name", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.Name = "-host" }},
		{"name-trailing-hyphen", "host name", func(r *FixedIPRequest, _ *fixedIPController, _ *unifi.Config) { r.Name = "host-" }},
		{"fixed-conflict", "another known client", func(r *FixedIPRequest, f *fixedIPController, _ *unifi.Config) {
			f.known = append(f.known, ClientRecord{MAC: "00:11:22:33:44:55", UseFixedIP: true, FixedIP: r.IP})
		}},
		{"known-current-conflict", "another known client", func(r *FixedIPRequest, f *fixedIPController, _ *unifi.Config) {
			f.known = append(f.known, ClientRecord{MAC: "00:11:22:33:44:55", IP: r.IP})
		}},
		{"online-conflict", "another known client", func(r *FixedIPRequest, f *fixedIPController, _ *unifi.Config) {
			f.current = append(f.current, ClientRecord{MAC: "00:11:22:33:44:55", IP: r.IP})
		}},
		{"duplicate", "multiple records", func(_ *FixedIPRequest, f *fixedIPController, _ *unifi.Config) { f.known = append(f.known, f.known[0]) }},
		{"missing-id", "record id", func(_ *FixedIPRequest, f *fixedIPController, _ *unifi.Config) { f.known[0].ID = "" }},
		{"unsafe-id", "record id", func(_ *FixedIPRequest, f *fixedIPController, _ *unifi.Config) { f.known[0].ID = "../networkconf" }},
		{"subnet-config", "UNIFI_SERVER_SUBNET", func(_ *FixedIPRequest, _ *fixedIPController, c *unifi.Config) { c.ServerSubnet = "bad" }},
		{"gateway-config", "UNIFI_SERVER_GATEWAY", func(_ *FixedIPRequest, _ *fixedIPController, c *unifi.Config) { c.ServerGateway = "192.168.20.1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, cfg := fixedIPRequest(), unifi.Config{}
			f, _ := newFixedIPController(t, cfg)
			tc.change(&req, f, &cfg)
			_, opt := newFixedIPController(t, cfg)
			s := NewService(opt)
			client, _ := s.session(context.Background())
			client.SetHTTPClient(&http.Client{Transport: f})
			_, err := s.SetClientFixedIP(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), tc.reason) || f.writes != 0 {
				t.Fatalf("err=%v writes=%d", err, f.writes)
			}
		})
	}
}

func TestFixedIPCustomSubnet(t *testing.T) {
	f, opt := newFixedIPController(t, unifi.Config{ServerSubnet: "10.20.30.0/23", ServerGateway: "10.20.30.254"})
	req := fixedIPRequest()
	req.IP = "10.20.31.80"
	_, err := NewService(opt).SetClientFixedIP(context.Background(), req)
	if err != nil || f.writes != 1 {
		t.Fatalf("err=%v writes=%d", err, f.writes)
	}
}

func TestFixedIPReadbackMismatch(t *testing.T) {
	for _, field := range []string{"ip", "name", "flag", "mac", "id", "empty", "two"} {
		t.Run(field, func(t *testing.T) {
			f, opt := newFixedIPController(t, unifi.Config{})
			f.mismatch = field
			out, err := NewService(opt).SetClientFixedIP(context.Background(), fixedIPRequest())
			if err == nil || !strings.Contains(err.Error(), "readback mismatch") || f.writes != 1 || out["before"] == nil {
				t.Fatalf("out=%v err=%v writes=%d", out, err, f.writes)
			}
		})
	}
}

func TestFixedIPControllerErrors(t *testing.T) {
	for _, path := range []string{"/rest/user", "/stat/sta", "/stat/device", "/rest/user/client-1", "write"} {
		for _, body := range []string{`{"meta":{"rc":"error","msg":"required"}}`, `{"meta":{"rc":"ok"}}`, `not-json`} {
			if path == "write" && body == `{"meta":{"rc":"ok"}}` {
				continue
			}
			t.Run(fmt.Sprint(path, body), func(t *testing.T) {
				f, opt := newFixedIPController(t, unifi.Config{})
				f.failPath, f.failBody = path, body
				if path == "write" {
					f.writeBody = body
				}
				h := NewHandler(nil, opt)
				raw, _ := json.Marshal(fixedIPRequest())
				rec := httptest.NewRecorder()
				h.HandleClientFixedIP(rec, httptest.NewRequest("POST", "/", strings.NewReader(string(raw))))
				wantWrites := 0
				if path == "/rest/user/client-1" || path == "write" {
					wantWrites = 1
				}
				wantStatus := 502
				if wantWrites > 0 {
					wantStatus = 504
				}
				if rec.Code != wantStatus || f.writes != wantWrites {
					t.Fatalf("status=%d writes=%d body=%s", rec.Code, f.writes, rec.Body.String())
				}
			})
		}
	}
}

func TestFixedIPMissingConfigIsPreWriteRefusal(t *testing.T) {
	h := NewHandler(nil, WithDial(func(context.Context) (*unifi.Client, error) { return nil, unifi.ErrNotConfigured }))
	w := httptest.NewRecorder()
	body, _ := json.Marshal(fixedIPRequest())
	h.HandleClientFixedIP(w, httptest.NewRequest("POST", "/", strings.NewReader(string(body))))
	if w.Code != 503 {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
}

func TestFixedIPMatchingValuesWithFlagOffStillWrites(t *testing.T) {
	f, opt := newFixedIPController(t, unifi.Config{})
	req := fixedIPRequest()
	f.known[0].FixedIP, f.known[0].Name = req.IP, req.Name
	out, err := NewService(opt).SetClientFixedIP(context.Background(), req)
	if err != nil || f.writes != 1 || out["no_op"] != false {
		t.Fatalf("out=%v err=%v writes=%d", out, err, f.writes)
	}
}

func TestFixedIPReservedTargetsFromEnv(t *testing.T) {
	t.Setenv("UNIFI_USER", "fake")
	t.Setenv("UNIFI_PASS", "fake")
	t.Setenv("UNIFI_SERVER_SUBNET", "192.168.10.0/24")
	t.Setenv("UNIFI_SERVER_GATEWAY", "192.168.10.1")
	for _, list := range []string{"192.168.10.80", " 192.168.10.100, 192.168.10.80 ", "not-an-ip", "::1"} {
		t.Run(list, func(t *testing.T) {
			t.Setenv("UNIFI_RESERVED_IPS", list)
			cfg, err := unifi.ConfigFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			f, opt := newFixedIPController(t, cfg)
			_, err = NewService(opt).SetClientFixedIP(context.Background(), fixedIPRequest())
			if err == nil || !strings.Contains(err.Error(), "reserved") || f.writes != 0 {
				t.Fatalf("err=%v writes=%d", err, f.writes)
			}
		})
	}
	t.Setenv("UNIFI_RESERVED_IPS", "192.168.10.100, 192.168.10.101")
	cfg, _ := unifi.ConfigFromEnv()
	f, opt := newFixedIPController(t, cfg)
	_, err := NewService(opt).SetClientFixedIP(context.Background(), fixedIPRequest())
	if err != nil || f.writes != 1 {
		t.Fatalf("unreserved host: err=%v writes=%d", err, f.writes)
	}
}

func TestFixedIPBoundarySuccess(t *testing.T) {
	for _, mode := range []string{"name-63", "own-uppercase-mac"} {
		t.Run(mode, func(t *testing.T) {
			f, opt := newFixedIPController(t, unifi.Config{})
			req := fixedIPRequest()
			if mode == "name-63" {
				req.Name = strings.Repeat("a", 63)
			} else {
				f.current = []ClientRecord{{MAC: strings.ToUpper(req.MAC), IP: req.IP}}
			}
			_, err := NewService(opt).SetClientFixedIP(context.Background(), req)
			if err != nil || f.writes != 1 {
				t.Fatalf("err=%v writes=%d", err, f.writes)
			}
		})
	}
}

func TestFixedIPErrorsDoNotExposeControllerBody(t *testing.T) {
	const private = "controller-private-response-marker"
	for _, mode := range []string{"read", "prewrite", "readback"} {
		t.Run(mode, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			f, opt := newFixedIPController(t, unifi.Config{})
			f.failPath, f.failBody, f.failStatus = "/rest/user", private, 500
			wantStatus, wantWrites := 502, 0
			if mode == "readback" {
				f.failPath = "/rest/user/client-1"
				wantStatus, wantWrites = 504, 1
			}
			audit := actuation.NewAuditLog(filepath.Join(t.TempDir(), "audit.json"))
			h := NewHandler(audit, opt)
			w := httptest.NewRecorder()
			if mode == "read" {
				router := chi.NewRouter()
				router.Get("/api/v1/network/clients/{mac}", h.HandleClientByMAC)
				router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/network/clients/"+fixedIPRequest().MAC, nil))
			} else {
				body, _ := json.Marshal(fixedIPRequest())
				h.HandleClientFixedIP(w, httptest.NewRequest("POST", "/", bytes.NewReader(body)))
			}
			var response struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &response)
			if w.Code != wantStatus || f.writes != wantWrites || strings.Contains(w.Body.String(), private) {
				t.Errorf("status=%d writes=%d body=%s", w.Code, f.writes, w.Body)
			}
			if mode != "readback" && response.Error != "UniFi read failed" {
				t.Errorf("error=%q", response.Error)
			}
			out := httptest.NewRecorder()
			audit.HandleList(out, httptest.NewRequest("GET", "/", nil))
			if strings.Contains(out.Body.String(), private) {
				t.Errorf("audit leaked: %s", out.Body)
			}
			if mode == "prewrite" && !strings.Contains(out.Body.String(), "UniFi read failed") {
				t.Errorf("audit missing safe detail: %s", out.Body)
			}
			if mode == "readback" && !strings.Contains(out.Body.String(), `"status":"unknown"`) {
				t.Errorf("audit missing unknown: %s", out.Body)
			}
			if !strings.Contains(logs.String(), private) {
				t.Error("controller detail missing from server log")
			}
		})
	}
}
