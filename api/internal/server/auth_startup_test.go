package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Serial: newTestConfig sets process environment and the shared state backend.
func TestAuthConfigurationAtStartup(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		fatal, loaded bool
	}{
		{"unique seats", "tokens:\n  - {name: 'seat:coordinator', role: reporter, token: fixture-seat-a}\n  - {name: 'seat:chief', role: reporter, token: fixture-seat-b}\n", false, true},
		{"duplicate seat", "tokens:\n  - {name: 'seat:coordinator', role: reporter, token: fixture-seat-a}\n  - {name: 'seat:coordinator', role: reporter, token: fixture-seat-b}\n", true, false},
		{"duplicate unconfigured seat", "tokens:\n  - {name: 'seat:coordinator', role: reporter}\n  - {name: 'seat:coordinator', role: reporter}\n", true, false},
		{"duplicate seat and invalid readers", "issue_readers: ['']\ntokens:\n  - {name: 'seat:coordinator'}\n  - {name: 'seat:coordinator'}\n", true, false},
		{"missing file", "", false, false},
		{"malformed yaml", "tokens: [", false, false},
		{"duplicate values", "tokens:\n  - {name: operator, role: operator, token: fixture-same}\n  - {name: viewer, role: viewer, token: fixture-same}\n", false, false},
		{"invalid role", "tokens:\n  - {name: operator, role: invalid, token: fixture-same}\n", false, false},
		{"invalid readers", "issue_readers: ['']\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Serial: inherits the configuration's environment mutations.
			cfg := newTestConfig(t)
			t.Setenv("PLATFORM_ROLE", "api")
			if tc.body != "" {
				if err := os.WriteFile(cfg.PlatformAuthPath, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			srv, err := New(cfg)
			if srv != nil {
				t.Cleanup(srv.Close)
			}
			if tc.fatal {
				if err == nil || srv != nil || !strings.Contains(err.Error(), "is named twice") {
					t.Fatalf("duplicate seat must stop startup: server present=%v, err=%v", srv != nil, err)
				}
				return
			}
			if err != nil || srv == nil || srv.authLoaded != tc.loaded {
				t.Fatalf("startup: server present=%v, err=%v, want auth loaded=%v", srv != nil, err, tc.loaded)
			}
			req := httptest.NewRequest(http.MethodGet, "/gated", nil)
			req.Header.Set("Authorization", "Bearer fixture-same")
			rec := httptest.NewRecorder()
			srv.auth.Require("viewer")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("unconfigured token authenticated: %d", rec.Code)
			}
		})
	}
}
