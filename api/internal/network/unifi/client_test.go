package unifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginAndLegacyGet(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
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
