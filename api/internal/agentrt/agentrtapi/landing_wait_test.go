package agentrtapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// In-process HTTP: no listening socket is needed. This tests authentication
// and registration only; executor/pin ownership is a store/PG integration.
func TestC4LandingRoutesAuthenticateBeforeConfiguration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte(`tokens:
  - name: agentd:coordinator
    role: agentd
    token: fixture-c4-agentd
  - name: viewer
    role: viewer
    token: fixture-c4-viewer
`), 0600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	Mount(router, auth, &API{})
	for _, tc := range []struct {
		method, path, token string
		code                int
	}{
		{"POST", "/agentrt/landing/refresh", "fixture-c4-agentd", 503},
		{"POST", "/agentrt/landing/refresh", "fixture-c4-viewer", 401},
		{"POST", "/agentrt/landing/refresh", "", 401},
		{"GET", "/agentrt/landing/operations/land:1", "fixture-c4-viewer", 503},
		{"GET", "/agentrt/landing/operations/land:1", "", 401},
	} {
		t.Run(tc.method+tc.path+tc.token, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"task":"t_00000000000000000000000001","op":"land:1"}`))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.code == http.StatusServiceUnavailable && !strings.Contains(w.Body.String(), "not_configured") {
				t.Fatal(w.Body.String())
			}
		})
	}
}
