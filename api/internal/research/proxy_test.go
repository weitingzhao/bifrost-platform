package research

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripProxyPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"health", "/health"},
		{"/health", "/health"},
		{"analytics/sepa/criteria-stats", "/analytics/sepa/criteria-stats"},
		{"/analytics/sepa/criteria-stats", "/analytics/sepa/criteria-stats"},
		{"", "/"},
	}
	for _, tc := range cases {
		if got := stripProxyPrefix(tc.in); got != tc.want {
			t.Fatalf("stripProxyPrefix(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestConfigFromEnvAPIBaseURL(t *testing.T) {
	t.Setenv("RESEARCH_API_URL", "  http://127.0.0.1:8795/  ")
	cfg := ConfigFromEnv()
	if cfg.APIBaseURL != "http://127.0.0.1:8795" {
		t.Fatalf("APIBaseURL=%q", cfg.APIBaseURL)
	}
}

func TestProxyHTTPForwardsPathAndQuery(t *testing.T) {
	var sawPath, sawQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawQuery = r.URL.RawQuery
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization must not be forwarded, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	svc := &Service{cfg: Config{APIBaseURL: upstream.URL}}
	src := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/research/analytics/sepa/criteria-stats?window=30d",
		nil,
	)
	src.Header.Set("Authorization", "Bearer platform-operator-dev")

	resp, err := svc.Proxy(src, stripProxyPrefix("analytics/sepa/criteria-stats"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if sawPath != "/analytics/sepa/criteria-stats" {
		t.Fatalf("path=%q", sawPath)
	}
	if sawQuery != "window=30d" {
		t.Fatalf("query=%q", sawQuery)
	}
}

func TestLegacyAnalyticsRedirectTarget(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/api/v1/plugins/analytics/status", "/api/v1/research/analytics/elementary"},
		{
			"/api/v1/plugins/analytics/api/elementary_report.html",
			"/api/v1/research/analytics/elementary/files/elementary_report.html",
		},
		{
			"/api/v1/plugins/analytics/api/static/app.js",
			"/api/v1/research/analytics/elementary/files/static/app.js",
		},
		{"/api/v1/plugins/analytics/api", "/api/v1/research/analytics/elementary/files/elementary_report.html"},
		{"/api/v1/plugins/analytics/api/", "/api/v1/research/analytics/elementary/files/elementary_report.html"},
	}
	for _, tc := range cases {
		if got := legacyAnalyticsRedirectTarget(tc.in); got != tc.want {
			t.Fatalf("legacyAnalyticsRedirectTarget(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestProxyHTTPHealthPath(t *testing.T) {
	var sawPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer upstream.Close()

	svc := &Service{cfg: Config{APIBaseURL: upstream.URL}}
	src := httptest.NewRequest(http.MethodGet, "/api/v1/research/health", nil)
	resp, err := svc.Proxy(src, stripProxyPrefix("health"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if sawPath != "/health" {
		t.Fatalf("path=%q", sawPath)
	}
}
