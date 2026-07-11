package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginAndLegacyGet(t *testing.T) {
	t.Parallel()
	loginCount := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCount++
			w.Header().Set("Set-Cookie", "TOKEN=abc123; Path=/")
			w.Header().Set("X-Csrf-Token", "csrf-login")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"}}`))
		case "/proxy/network/api/s/default/stat/device":
			if r.Header.Get("Cookie") != "TOKEN=abc123" {
				http.Error(w, "missing cookie", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"name":"ucg","type":"ugw"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	c := New(Config{Host: host, User: "agent", Pass: "secret", Site: "default"})
	c.http = srv.Client()

	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("login: %v", err)
	}
	raw, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	// Second call must reuse cookie — no extra login.
	if _, err := c.ListDevices(context.Background()); err != nil {
		t.Fatalf("devices 2: %v", err)
	}
	if loginCount != 1 {
		t.Fatalf("expected 1 login, got %d", loginCount)
	}
	var payload struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Name != "ucg" {
		t.Fatalf("unexpected payload: %s", raw)
	}
}

func TestEnsureLoginSkipsWhenCached(t *testing.T) {
	t.Parallel()
	loginCount := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			loginCount++
			w.Header().Set("Set-Cookie", "TOKEN=cached; Path=/")
			w.Header().Set("X-Csrf-Token", "csrf")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	c := New(Config{Host: host, User: "u", Pass: "p", Site: "default"})
	c.http = srv.Client()
	ctx := context.Background()
	if err := c.EnsureLogin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureLogin(ctx); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 {
		t.Fatalf("expected 1 login for two EnsureLogin calls, got %d", loginCount)
	}
}

func TestReauthOn401(t *testing.T) {
	t.Parallel()
	loginCount := 0
	deviceHits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginCount++
			token := fmt.Sprintf("TOKEN=t%d", loginCount)
			w.Header().Set("Set-Cookie", token+"; Path=/")
			w.Header().Set("X-Csrf-Token", "csrf")
			_, _ = w.Write([]byte(`{}`))
		case "/proxy/network/api/s/default/stat/device":
			deviceHits++
			if deviceHits == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Cookie") != "TOKEN=t2" {
				http.Error(w, "bad cookie", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	c := New(Config{Host: host, User: "u", Pass: "p", Site: "default"})
	c.http = srv.Client()
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListDevices(context.Background()); err != nil {
		t.Fatalf("reauth devices: %v", err)
	}
	if loginCount != 2 {
		t.Fatalf("expected login+reauth (2), got %d", loginCount)
	}
}

func TestV2GetZones(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			w.Header().Set("Set-Cookie", "TOKEN=xyz; Path=/")
			w.Header().Set("X-Csrf-Token", "csrf")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path == "/proxy/network/v2/api/site/default/firewall/zone" {
			_, _ = w.Write([]byte(`{"data":[{"name":"Bifrost Server"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	c := New(Config{Host: host, User: "u", Pass: "p", Site: "default"})
	c.http = srv.Client()
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := c.ListZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Bifrost Server") {
		t.Fatalf("zones: %s", raw)
	}
}

func TestConfigFromEnvRequiresCredentials(t *testing.T) {
	t.Setenv("UNIFI_USER", "")
	t.Setenv("UNIFI_PASS", "")
	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("expected error when credentials missing")
	}
}

func TestIntegrationSitesHaveID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "key-123" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"site-uuid"}]}`))
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	c := New(Config{Host: host, User: "u", Pass: "p", APIKey: "key-123"})
	c.http = srv.Client()
	ok, err := c.IntegrationSitesHaveID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected site id")
	}
}
