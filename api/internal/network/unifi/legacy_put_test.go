package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type memoryTransport func(*http.Request) (*http.Response, error)

func (fn memoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestLegacyPutSessionAndCSRFRetry(t *testing.T) {
	for _, status := range []int{401, 403} {
		for _, persistent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/persistent=%t", status, persistent), func(t *testing.T) {
				logins, puts := 0, 0
				body := map[string]any{"use_fixedip": true, "fixed_ip": "192.168.10.80", "name": "ubt-k3s-07"}
				c := New(Config{Host: "controller.invalid", Site: "test-site"})
				c.SetHTTPClient(&http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
					w := httptest.NewRecorder()
					if r.URL.Path == "/api/auth/login" {
						logins++
						w.Header().Set("Set-Cookie", fmt.Sprintf("TOKEN=%d; Path=/", logins))
						w.Header().Set("X-CSRF-Token", fmt.Sprintf("csrf-%d", logins))
					} else {
						if r.Method != "PUT" || r.URL.Path != "/proxy/network/api/s/test-site/rest/user/client-1" {
							t.Fatalf("request=%s %s", r.Method, r.URL.Path)
						}
						puts++
						csrf := fmt.Sprintf("csrf-%d", logins)
						if puts == 3 {
							csrf = "rotated"
						}
						if r.Header.Get("Cookie") != fmt.Sprintf("TOKEN=%d", logins) || r.Header.Get("X-CSRF-Token") != csrf {
							t.Fatalf("session headers=%v", r.Header)
						}
						var got map[string]any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Fatal(err)
						}
						if len(got) != 3 || got["fixed_ip"] != body["fixed_ip"] || got["name"] != body["name"] || got["use_fixedip"] != true {
							t.Fatalf("body=%v", got)
						}
						if puts == 1 || persistent {
							w.WriteHeader(status)
						} else {
							w.Header().Set("X-Updated-CSRF-Token", "rotated")
						}
					}
					_, _ = w.WriteString(`{"meta":{"rc":"ok"},"data":[]}`)
					return w.Result(), nil
				})})
				_, err := c.LegacyPut(context.Background(), "rest/user/client-1", body)
				if (err != nil) != persistent || logins != 2 || puts != 2 {
					t.Fatalf("err=%v logins=%d puts=%d", err, logins, puts)
				}
				if !persistent {
					if _, err := c.LegacyPut(context.Background(), "/rest/user/client-1", body); err != nil {
						t.Fatal(err)
					}
					if logins != 2 || puts != 3 {
						t.Fatalf("session not reused: %d %d", logins, puts)
					}
				}
			})
		}
	}
}

func TestFixedIPConfigFromEnv(t *testing.T) {
	t.Setenv("UNIFI_USER", "fake")
	t.Setenv("UNIFI_PASS", "fake")
	t.Setenv("UNIFI_SERVER_SUBNET", "")
	t.Setenv("UNIFI_SERVER_GATEWAY", "")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.ServerSubnet != "192.168.10.0/24" || cfg.ServerGateway != "192.168.10.1" {
		t.Fatalf("defaults not loaded: %v", err)
	}
	t.Setenv("UNIFI_SERVER_SUBNET", "10.2.0.0/24")
	t.Setenv("UNIFI_SERVER_GATEWAY", "10.2.0.254")
	cfg, err = ConfigFromEnv()
	if err != nil || cfg.ServerSubnet != "10.2.0.0/24" || cfg.ServerGateway != "10.2.0.254" {
		t.Fatalf("overrides not loaded: %v", err)
	}
}
