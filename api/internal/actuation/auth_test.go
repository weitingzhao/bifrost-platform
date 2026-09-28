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

func writeAuthFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAuthSkipsPlaceholderWithoutEnv(t *testing.T) {
	t.Setenv("PLATFORM_ADMIN_TOKEN", "")
	auth, err := LoadAuth(writeAuthFile(t, `
tokens:
  - name: admin
    role: admin
    token_env: PLATFORM_ADMIN_TOKEN
    token: placeholder-override-via-env
`))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/capabilities", nil)
	req.Header.Set("Authorization", "Bearer placeholder-override-via-env")
	if _, ok := auth.Authenticate(req); ok {
		t.Fatal("placeholder must not authenticate")
	}
}

func TestLoadAuthRejectsSharedToken(t *testing.T) {
	t.Setenv("PLATFORM_OPERATOR_TOKEN", "")
	t.Setenv("PLATFORM_ADMIN_TOKEN", "")
	_, err := LoadAuth(writeAuthFile(t, `
tokens:
  - name: operator
    role: operator
    token_env: PLATFORM_OPERATOR_TOKEN
    token: shared-inline
  - name: admin
    role: admin
    token_env: PLATFORM_ADMIN_TOKEN
    token: shared-inline
`))
	if err == nil {
		t.Fatal("expected error when two roles resolve to the same token")
	}
}

func TestLoadAuthRejectsSharedEnvToken(t *testing.T) {
	t.Setenv("PLATFORM_OPERATOR_TOKEN", "same-secret")
	t.Setenv("PLATFORM_ADMIN_TOKEN", "same-secret")
	_, err := LoadAuth(writeAuthFile(t, `
tokens:
  - name: operator
    role: operator
    token_env: PLATFORM_OPERATOR_TOKEN
  - name: admin
    role: admin
    token_env: PLATFORM_ADMIN_TOKEN
`))
	if err == nil {
		t.Fatal("expected error when two env tokens are equal")
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
