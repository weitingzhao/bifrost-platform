package actuation

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthEnvOverride(t *testing.T) {
	t.Setenv("PLATFORM_OPERATOR_TOKEN", "secret-token")
	path := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(path, []byte(`
tokens:
  - name: operator
    role: operator
    token_env: PLATFORM_OPERATOR_TOKEN
    token: placeholder
`), 0o600); err != nil {
		t.Fatal(err)
	}

	auth, err := LoadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/capabilities", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	principal, ok := auth.Authenticate(req)
	if !ok {
		t.Fatal("expected token to authenticate")
	}
	if principal.Role != RoleOperator || principal.Name != "operator" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
}

func TestRequireOperatorRejectsViewer(t *testing.T) {
	auth := &AuthService{
		principals: map[string]Principal{
			"viewer-token": {Name: "viewer", Role: RoleViewer},
		},
	}
	handler := auth.Require(RoleOperator)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/write", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusUnauthorized)
	}
}
