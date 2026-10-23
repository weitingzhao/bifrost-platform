package actuation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Intake tokens and the issue readers (W-31 BI-1 slice 2,
// DESIGN-crew-intake-2026-10-22 §5.2, §5.3).

const intakeAuthFile = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-token-1
  - name: operator
    role: operator
    token: fixture-operator-token-1
  - name: trade-satellite
    role: operator
    token: fixture-satellite-token-1
  - name: admin
    role: admin
    token: fixture-admin-token-1
intake_tokens:
  - name: intake:dots-p2
    token_env: INTAKE_DOTS_P2_TOKEN
`

func request(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestIntakeTokensComeFromTheirOwnKey(t *testing.T) {
	t.Setenv("INTAKE_DOTS_P2_TOKEN", "fixture-intake-token-1")
	auth, err := LoadAuth(writeAuthFile(t, intakeAuthFile))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := auth.Authenticate(request("fixture-intake-token-1"))
	if !ok || p.Role != RoleIntake || p.Name != "intake:dots-p2" {
		t.Fatalf("principal %+v %v", p, ok)
	}
	if c, ok := IntakeClient(p); !ok || c != "agent:dots-p2" {
		t.Fatalf("client %q %v", c, ok)
	}
	for _, q := range []Principal{{Name: "intake:dots-p2", Role: RoleOperator}, {Name: "operator", Role: RoleIntake}, {Name: "intake:", Role: RoleIntake}} {
		if _, ok := IntakeClient(q); ok {
			t.Fatalf("%+v counted as an intake client", q)
		}
	}
	// Below viewer: no Require route takes it; RequireAny(intake) does.
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, min := range []Role{RoleViewer, RoleReporter, RoleOperator, RoleAdmin} {
		rec := httptest.NewRecorder()
		auth.Require(min)(sentinel).ServeHTTP(rec, request("fixture-intake-token-1"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Require(%s) answered an intake token %d", min, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	auth.RequireAny(RoleIntake)(sentinel).ServeHTTP(rec, request("fixture-intake-token-1"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("RequireAny(intake) answered %d", rec.Code)
	}
	// The public introspection says who it is and that it may do nothing.
	rec = httptest.NewRecorder()
	auth.Capabilities(rec, request("fixture-intake-token-1"))
	var caps map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatal(err)
	}
	if caps["can_operate"] != false || caps["can_admin"] != false || caps["role"] != "intake" {
		t.Fatalf("capabilities %v", caps)
	}
	// Without its environment variable an intake token is not loaded.
	t.Setenv("INTAKE_DOTS_P2_TOKEN", "")
	auth, err = LoadAuth(writeAuthFile(t, intakeAuthFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.Authenticate(request("fixture-intake-token-1")); ok {
		t.Fatal("an intake token without its variable authenticated")
	}
}

// A platform from before intake_tokens reads only tokens: the same file
// gives it no intake token at all (R2-A6), never a viewer by the old
// ladder's default.
func TestAnOlderPlatformReadsNoIntakeToken(t *testing.T) {
	var old struct {
		Tokens []struct {
			Name     string `yaml:"name"`
			Role     Role   `yaml:"role"`
			Token    string `yaml:"token"`
			TokenEnv string `yaml:"token_env"`
		} `yaml:"tokens"`
	}
	if err := yaml.Unmarshal([]byte(intakeAuthFile), &old); err != nil {
		t.Fatal(err)
	}
	for _, tok := range old.Tokens {
		if strings.HasPrefix(tok.Name, IntakePrefix) || tok.TokenEnv == "INTAKE_DOTS_P2_TOKEN" {
			t.Fatalf("the old file shape reads intake token %+v", tok)
		}
	}
	if len(old.Tokens) != 4 {
		t.Fatalf("the old shape read %d tokens", len(old.Tokens))
	}
}

// tokens: names a closed set of roles; intake is not one of them.
func TestTokenRolesAreClosed(t *testing.T) {
	t.Setenv("X_TOKEN", "fixture-x-token-1")
	for _, body := range []string{
		"tokens:\n  - name: intake:dots\n    role: intake\n    token: fixture-x-token-2\n",
		"tokens:\n  - name: odd\n    role: nonsense\n    token: fixture-x-token-2\n",
		"tokens:\n  - name: ops\n    role: Operator\n    token: fixture-x-token-2\n",
		"intake_tokens:\n  - name: dots\n    token_env: X_TOKEN\n",
		"intake_tokens:\n  - name: intake:Dots\n    token_env: X_TOKEN\n",
		"intake_tokens:\n  - name: intake:dots\n    role: admin\n    token_env: X_TOKEN\n",
		"intake_tokens:\n  - name: intake:dots\n    token: fixture-x-token-2\n",
		"intake_tokens:\n  - name: intake:dots\n",
		"tokens:\n  - name: viewer\n    role: viewer\n    token_env: X_TOKEN\nintake_tokens:\n  - name: intake:dots\n    token_env: X_TOKEN\n",
		"issue_readers: ['']\n",
	} {
		if _, err := LoadAuth(writeAuthFile(t, body)); err == nil {
			t.Fatalf("loaded:\n%s", body)
		}
	}
	// An empty role is still viewer, as before.
	auth, err := LoadAuth(writeAuthFile(t, "tokens:\n  - name: plain\n    token: fixture-x-token-2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := auth.Authenticate(request("fixture-x-token-2")); !ok || p.Role != RoleViewer {
		t.Fatalf("%+v %v", p, ok)
	}
}

// The issue readers are operator and admin tokens on the list (§5.3):
// trade-satellite is an operator and reads none; a viewer named admin reads
// none either.
func TestIssueReadersAreNamedTokens(t *testing.T) {
	t.Setenv("INTAKE_DOTS_P2_TOKEN", "fixture-intake-token-1")
	auth, err := LoadAuth(writeAuthFile(t, intakeAuthFile))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for token, want := range map[string]int{
		"fixture-operator-token-1": http.StatusNoContent, "fixture-admin-token-1": http.StatusNoContent,
		"fixture-satellite-token-1": http.StatusUnauthorized, "fixture-viewer-token-1": http.StatusUnauthorized,
		"fixture-intake-token-1": http.StatusUnauthorized, "": http.StatusUnauthorized,
	} {
		rec := httptest.NewRecorder()
		auth.RequireIssueReader()(sentinel).ServeHTTP(rec, request(token))
		if rec.Code != want {
			t.Fatalf("token %q: %d, want %d", token, rec.Code, want)
		}
	}
	if auth.IssueReader(Principal{Name: "admin", Role: RoleViewer}) {
		t.Fatal("a viewer named admin reads issues")
	}
	// A list of its own replaces the default.
	auth, err = LoadAuth(writeAuthFile(t, intakeAuthFile+"issue_readers: [admin]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if auth.IssueReader(Principal{Name: "operator", Role: RoleOperator}) || !auth.IssueReader(Principal{Name: "admin", Role: RoleAdmin}) {
		t.Fatal("the readers list is not the configured one")
	}
	auth, err = LoadAuth(writeAuthFile(t, intakeAuthFile+"issue_readers: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if auth.IssueReader(Principal{Name: "admin", Role: RoleAdmin}) {
		t.Fatal("an empty readers list reads issues")
	}
}
