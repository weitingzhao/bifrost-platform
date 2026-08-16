package marketdata

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyUpstreamAuthRewritesMutatingBearer(t *testing.T) {
	svc := &Service{cfg: Config{WriteToken: "plugin-write-secret"}}
	src := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/market-data/api/market/ingest/enqueue", nil)
	src.Header.Set("Authorization", "Bearer platform-operator-dev")
	dst := httptest.NewRequest(http.MethodPost, "http://plugin/market/ingest/enqueue", nil)
	svc.applyUpstreamAuth(dst, src)
	got := dst.Header.Get("Authorization")
	if got != "Bearer plugin-write-secret" {
		t.Fatalf("Authorization=%q", got)
	}
	if dst.Header.Get("X-Market-Data-Write-Token") != "plugin-write-secret" {
		t.Fatalf("X-Market-Data-Write-Token=%q", dst.Header.Get("X-Market-Data-Write-Token"))
	}
}

func TestApplyUpstreamAuthStripsGetBearer(t *testing.T) {
	svc := &Service{cfg: Config{WriteToken: "plugin-write-secret"}}
	src := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/market-data/api/health", nil)
	src.Header.Set("Authorization", "Bearer platform-operator-dev")
	dst := httptest.NewRequest(http.MethodGet, "http://plugin/health", nil)
	svc.applyUpstreamAuth(dst, src)
	if got := dst.Header.Get("Authorization"); got != "" {
		t.Fatalf("GET must not forward Authorization, got %q", got)
	}
}

func TestProxyHTTPPostUsesWriteTokenNotOperator(t *testing.T) {
	var sawAuth, sawExtra string
	plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawExtra = r.Header.Get("X-Market-Data-Write-Token")
		if r.URL.Path != "/market/ingest/enqueue" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true,"deduped":true}`)
	}))
	defer plugin.Close()

	svc := &Service{
		cfg: Config{
			APIBaseURL: plugin.URL,
			WriteToken: "plugin-write-secret",
		},
	}
	src := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/plugins/market-data/api/market/ingest/enqueue",
		strings.NewReader(`{"kind":"stock_daily_grouped","payload":{"symbols":["NVDA"]}}`),
	)
	src.Header.Set("Authorization", "Bearer platform-operator-dev")
	src.Header.Set("Content-Type", "application/json")

	resp, err := svc.Proxy(src, "/market/ingest/enqueue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if sawAuth != "Bearer plugin-write-secret" {
		t.Fatalf("plugin Authorization=%q", sawAuth)
	}
	if sawExtra != "plugin-write-secret" {
		t.Fatalf("plugin X-Market-Data-Write-Token=%q", sawExtra)
	}
}

func TestConfigFromEnvWriteToken(t *testing.T) {
	t.Setenv("MARKET_DATA_WRITE_TOKEN", "  clustered-token  ")
	t.Setenv("MARKET_DATA_API_URL", "http://127.0.0.1:8790")
	cfg := ConfigFromEnv()
	if cfg.WriteToken != "clustered-token" {
		t.Fatalf("WriteToken=%q", cfg.WriteToken)
	}
	if cfg.APIBaseURL != "http://127.0.0.1:8790" {
		t.Fatalf("APIBaseURL=%q", cfg.APIBaseURL)
	}
}
