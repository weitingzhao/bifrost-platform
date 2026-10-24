package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/network"
	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

type fixedIPTransport func(*http.Request) (*http.Response, error)

func (f fixedIPTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFixedIPReadRequiresViewer(t *testing.T) {
	s := reachServer(t)
	calls := 0
	s.network = network.NewHandler(nil, network.WithDial(func(context.Context) (*unifi.Client, error) {
		calls++
		return nil, unifi.ErrNotConfigured
	}))
	router := s.Router()
	for _, p := range append(slices.Clone(reachPrincipals), "invalid-token") {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/network/clients/68:1d:ef:5f:29:dd", nil)
			if p != "" {
				token := reachTokens[p]
				if p == "invalid-token" {
					token = "fixture-invalid"
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			before := calls
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			allowed := slices.Contains([]string{"viewer", "reporter", "operator", "admin", "trade-satellite"}, p)
			want, wantCalls := http.StatusUnauthorized, 0
			if allowed {
				want, wantCalls = http.StatusServiceUnavailable, 1
			}
			if rec.Code != want || calls-before != wantCalls {
				t.Fatalf("status=%d want=%d controller calls=%d want=%d", rec.Code, want, calls-before, wantCalls)
			}
		})
	}
}

// The route walk stops before the handler-level approval guard. Match the
// firewall's middleware reach, then exercise both guarded HTTP routes below.
func TestFixedIPWriteMatchesFirewallApprovalBoundary(t *testing.T) {
	s := reachServer(t)
	reach := routeReach(t, s)
	router := s.Router()
	for _, p := range reachPrincipals {
		t.Run(p, func(t *testing.T) {
			fixed := slices.Contains(reach[p], "POST /api/v1/network/clients/fixed-ip")
			firewall := slices.Contains(reach[p], "POST /api/v1/network/firewall/apply")
			if fixed != firewall {
				t.Fatalf("middleware reach: fixed IP=%v firewall=%v", fixed, firewall)
			}
			for _, tc := range []struct{ path, body, action string }{
				{"/api/v1/network/clients/fixed-ip", `{"mac":"68:1d:ef:5f:29:dd","ip":"192.168.10.80","name":"ubt-k3s-07"}`, "unifi_client_fixed_ip"},
				{"/api/v1/network/firewall/apply", `{}`, "unifi_firewall_apply"},
			} {
				req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
				if p != "" {
					req.Header.Set("Authorization", "Bearer "+reachTokens[p])
				}
				req.Header.Set("X-Approval-Executor", "true")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				want := http.StatusUnauthorized
				if fixed {
					want = http.StatusForbidden
					if !strings.Contains(rec.Body.String(), `"error":"approval required"`) || !strings.Contains(rec.Body.String(), tc.action) {
						t.Fatalf("%s: missing approval refusal: %s", tc.path, rec.Body)
					}
				}
				if rec.Code != want {
					t.Fatalf("%s: status=%d want=%d body=%s", tc.path, rec.Code, want, rec.Body)
				}
			}
		})
	}
}

// Exercise the real router, catalog executor and approval store with an entirely
// in-memory controller. In particular, malformed direct bodies must not escape
// the guard via its missing-parameter validation path.
func TestFixedIPApprovalEndToEnd(t *testing.T) {
	for _, outcome := range []string{"success", "transport", "timeout", "write-5xx", "readback"} {
		t.Run(outcome, func(t *testing.T) { fixedIPApprovalEndToEnd(t, outcome) })
	}
}

func fixedIPApprovalEndToEnd(t *testing.T, outcome string) {
	cfg := newTestConfig(t)
	t.Setenv("PLATFORM_ROLE", "api")
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	authPath := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(authPath, []byte("tokens:\n  - name: operator\n    role: operator\n    token: fake-operator\n  - name: admin\n    role: admin\n    token: fake-admin\n  - name: viewer\n    role: viewer\n    token: fake-viewer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_AUTH_CONFIG", authPath)
	cfg.PlatformAuthPath = authPath
	s := newTestServer(t, cfg)
	writes, reads := 0, 0
	record := map[string]any{"_id": "client-1", "mac": "68:1d:ef:5f:29:dd", "ip": "192.168.10.41", "name": "old", "use_fixedip": false, "private_field": "must-not-leak"}
	client := unifi.New(unifi.Config{Host: "controller.invalid", Site: "default"})
	client.SetHTTPClient(&http.Client{Transport: fixedIPTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if r.URL.Path == "/api/auth/login" {
			w.Header().Set("Set-Cookie", "TOKEN=fake; Path=/")
			_, _ = w.WriteString(`{}`)
			return w.Result(), nil
		}
		path := strings.TrimPrefix(r.URL.Path, "/proxy/network/api/s/default")
		data := []any{record}
		switch {
		case r.Method == "PUT" && path == "/rest/user/client-1":
			writes++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 3 || body["fixed_ip"] != "192.168.10.80" || body["name"] != "ubt-k3s-07" || body["use_fixedip"] != true {
				t.Fatalf("unapproved body: %v", body)
			}
			for k, v := range body {
				record[k] = v
			}
			switch outcome {
			case "transport":
				return nil, errors.New("connection lost after applying PUT")
			case "timeout":
				return nil, context.DeadlineExceeded
			case "write-5xx":
				w.WriteHeader(503)
				_, _ = w.WriteString("controller-private-response")
				return w.Result(), nil
			}
		case r.Method == "GET" && (path == "/rest/user" || path == "/rest/user/client-1"):
			reads++
			if outcome == "readback" && path == "/rest/user/client-1" {
				w.WriteHeader(500)
				_, _ = w.WriteString("controller-private-response")
				return w.Result(), nil
			}
		case r.Method == "GET" && (path == "/stat/sta" || path == "/stat/device"):
			reads++
			data = []any{}
		default:
			t.Fatalf("unexpected controller request %s %s", r.Method, path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]string{"rc": "ok"}, "data": data})
		return w.Result(), nil
	})})
	s.network = network.NewHandler(nil, network.WithDial(func(context.Context) (*unifi.Client, error) { return client, nil }))
	s.bindActionExecutors()
	router := s.Router()
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Bifrost-Session", "fixed-ip-test")
		r.Header.Set("X-Approval-Executor", "true") // Caller-controlled headers cannot authorize writes.
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	body := `{"mac":"68:1d:ef:5f:29:dd","ip":"192.168.10.80","name":"ubt-k3s-07"}`
	path := "/api/v1/network/clients/fixed-ip"
	for _, token := range []string{"fake-operator", "fake-admin"} {
		for _, input := range []string{body, `{}`, `null`, `{"MAC":"68:1d:ef:5f:29:dd","IP":"192.168.10.80","NAME":"ubt-k3s-07"}`, body + ` {}`, `{"mac":null,"MAC":"68:1d:ef:5f:29:dd","ip":"192.168.10.80","name":"ubt-k3s-07"}`, `{"mac":"68:1d:ef:5f:29:dd","ip":"192.168.10.80","name":null}`, `{"mac":"68:1d:ef:5f:29:dd","ip":"192.168.10.80","name":"ubt-k3s-07","name":null}`} {
			w := call("POST", path, input, token)
			if w.Code != 403 && w.Code != 400 {
				t.Fatalf("direct=%d %s", w.Code, w.Body.String())
			}
			if writes != 0 || reads != 0 {
				t.Fatalf("unapproved controller access: writes=%d reads=%d", writes, reads)
			}
		}
	}
	create := call("POST", "/api/v1/approvals", `{"action":"unifi_client_fixed_ip","params":`+body+`,"reason":"Reserve the new node","rollback":"Restore the recorded previous client settings"}`, "fake-operator")
	if create.Code != 201 {
		t.Fatalf("create=%d %s", create.Code, create.Body.String())
	}
	var created struct {
		ID   string `json:"id"`
		Tier string `json:"tier"`
	}
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	if created.Tier != "D" || writes != 0 {
		t.Fatalf("created=%+v writes=%d", created, writes)
	}
	opened := call("GET", "/api/v1/approvals/"+created.ID, "", "fake-viewer")
	var approval struct {
		Line   string `json:"approval_line"`
		Hash   string `json:"params_hash"`
		Number int    `json:"number"`
	}
	_ = json.Unmarshal(opened.Body.Bytes(), &approval)
	if approval.Line == "" || !strings.Contains(opened.Body.String(), "192.168.10.80") {
		t.Fatalf("card=%s", opened.Body.String())
	}
	approvePath := "/api/v1/approvals/" + created.ID + "/approve"
	approveBody := func(channel, hash string) string {
		raw, _ := json.Marshal(map[string]any{"channel": channel, "approval_line": approval.Line, "params_hash": hash, "confirm_number": approval.Number, "params": map[string]any{"ip": "192.168.10.99", "name": "swapped"}})
		return string(raw)
	}
	for _, tc := range []struct {
		channel, token, hash string
		status               int
	}{
		{"console", "fake-operator", approval.Hash, 401},
		{"chat", "fake-admin", approval.Hash, 403},
		{"console", "fake-admin", "wrong-hash", 409},
	} {
		w := call("POST", approvePath, approveBody(tc.channel, tc.hash), tc.token)
		if w.Code != tc.status || writes != 0 {
			t.Fatalf("rejected approval=%d writes=%d %s", w.Code, writes, w.Body.String())
		}
	}
	approved := call("POST", approvePath, approveBody("console", approval.Hash), "fake-admin")
	wantStatus := "executed"
	if outcome != "success" {
		wantStatus = "unknown"
	}
	if approved.Code != 200 || !strings.Contains(approved.Body.String(), `"status":"`+wantStatus+`"`) || writes != 1 {
		t.Fatalf("approved=%d writes=%d %s", approved.Code, writes, approved.Body.String())
	}
	stored := call("GET", "/api/v1/approvals/"+created.ID, "", "fake-viewer")
	if !strings.Contains(stored.Body.String(), `"status":"`+wantStatus+`"`) || strings.Contains(stored.Body.String(), "controller-private-response") {
		t.Fatalf("stored approval=%s", stored.Body)
	}
	if outcome == "success" {
		for _, field := range []string{`"before"`, `"after"`, `"no_op":false`} {
			if !strings.Contains(approved.Body.String(), field) {
				t.Fatalf("missing %s: %s", field, approved.Body.String())
			}
		}
	}
	again := call("POST", approvePath, approveBody("console", approval.Hash), "fake-admin")
	if again.Code != 409 || writes != 1 {
		t.Fatalf("repeat=%d writes=%d", again.Code, writes)
	}
	direct := call("POST", path, body, "fake-admin")
	if direct.Code != 403 || writes != 1 {
		t.Fatalf("post-approval direct=%d writes=%d", direct.Code, writes)
	}
	read := call("GET", "/api/v1/network/clients/68:1d:ef:5f:29:dd", "", "fake-viewer")
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"fixed_ip":"192.168.10.80"`) || strings.Contains(read.Body.String(), "must-not-leak") || strings.Contains(approved.Body.String(), "must-not-leak") {
		t.Fatalf("read=%d %s", read.Code, read.Body.String())
	}
}

func TestFixedIPPostWriteErrorsAreNotRequeued(t *testing.T) {
	for _, failAt := range []string{"write", "transport", "timeout", "readback", "mismatch"} {
		t.Run(failAt, func(t *testing.T) {
			writes := 0
			client := unifi.New(unifi.Config{Host: "controller.invalid", Site: "default"})
			client.SetHTTPClient(&http.Client{Transport: fixedIPTransport(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				if r.URL.Path == "/api/auth/login" {
					w.Header().Set("Set-Cookie", "TOKEN=fake; Path=/")
					_, _ = w.WriteString(`{}`)
					return w.Result(), nil
				}
				if r.Method == "PUT" {
					writes++
					if failAt == "transport" {
						return nil, errors.New("connection lost after PUT")
					}
					if failAt == "timeout" {
						return nil, context.DeadlineExceeded
					}
				}
				if (failAt == "write" && r.Method == "PUT") || (failAt == "readback" && writes > 0 && r.Method == "GET") {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.WriteString(`{"error":"UNIFI_USER required"}`)
				} else if strings.HasSuffix(r.URL.Path, "/stat/sta") || strings.HasSuffix(r.URL.Path, "/stat/device") {
					_, _ = w.WriteString(`{"meta":{"rc":"ok"},"data":[]}`)
				} else {
					_, _ = w.WriteString(`{"meta":{"rc":"ok"},"data":[{"_id":"client-1","mac":"68:1d:ef:5f:29:dd","name":"old"}]}`)
				}
				return w.Result(), nil
			})})
			h := network.NewHandler(nil, network.WithDial(func(context.Context) (*unifi.Client, error) { return client, nil }))
			w := httptest.NewRecorder()
			h.HandleClientFixedIP(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"mac":"68:1d:ef:5f:29:dd","ip":"192.168.10.80","name":"ubt-k3s-07"}`)))
			_, err := interpretAction(w.Code, w.Body.Bytes())
			if w.Code != 504 || !actions.IsUncertain(err) || actions.IsTransient(err) || writes != 1 {
				t.Fatalf("status=%d err=%v writes=%d", w.Code, err, writes)
			}
		})
	}
}
