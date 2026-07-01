package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestClassifyHTTP(t *testing.T) {
	tests := []struct {
		code     int
		wantReach Reachability
	}{
		{200, ReachOK},
		{503, ReachDegraded},
		{404, ReachFail},
	}
	for _, tc := range tests {
		reach, _ := classifyHTTP(tc.code)
		if reach != tc.wantReach {
			t.Fatalf("code %d: got %s want %s", tc.code, reach, tc.wantReach)
		}
	}
}

func TestProbeHTTP_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewProber()
	target := p.probeHTTP(context.Background(), "test", "trade_api", srv.URL, "", config.Environment{})
	if target.Reachability != ReachOK {
		t.Fatalf("reachability: %s", target.Reachability)
	}
}

func TestProbeHTTP_IngressHost(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewProber()
	env := config.Environment{IngressHost: "trade.bifrost.lan"}
	target := p.probeHTTP(context.Background(), "test", "trade_api", srv.URL, "", env)
	if target.Reachability != ReachOK {
		t.Fatalf("reachability: %s", target.Reachability)
	}
	if gotHost != "trade.bifrost.lan" {
		t.Fatalf("Host header: got %q want trade.bifrost.lan", gotHost)
	}
}

func TestProbeCapabilities_Authenticated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"identity":{"authenticated":true,"role":"operator","name":"test"},"capabilities":{"can_operate":true}}`))
	}))
	defer srv.Close()

	p := NewProber()
	target := p.probeCapabilities(context.Background(), srv.URL, "secret", config.Environment{})
	if target.Auth != AuthOK {
		t.Fatalf("auth: %s detail=%s", target.Auth, target.Detail)
	}
	if target.AuthorizationLevel != "L1-capable" {
		t.Fatalf("level: %s", target.AuthorizationLevel)
	}
}

func TestProbeEnvironment_IncludesPolicyBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewProber()
	p.Client = srv.Client()

	env := config.Environment{
		ID: "test", Label: "Test", NginxBase: srv.URL,
		Postgres: config.HostPort{Host: "127.0.0.1", Port: 1},
		Redis:    config.HostPort{Host: "127.0.0.1", Port: 1},
	}
	matrix := p.ProbeEnvironment(context.Background(), env)

	var foundBlocked bool
	for _, t := range matrix.Targets {
		if t.ID == "ib-operator-rpc" && t.Auth == AuthBlocked {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Fatal("expected policy-blocked ib-operator-rpc target")
	}
}
