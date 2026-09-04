package actuation

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Role string

const (
	RoleViewer Role = "viewer"
	// RoleReporter may write evidence about itself and nothing else.
	//
	// It exists because the alternative was worse. A workload that needs to
	// record its own outcomes — the Research Loop's CronJob reporting to the
	// trust matrix — would otherwise need an operator token, and operator also
	// carries `POST /cluster/workloads/scale` and
	// `PUT /agent/governance/trust-overrides/{skill_id}`. That would let the
	// Loop scale the trade `daemon`, which D10 forbids outright, and grant
	// itself the very autonomy the trust gate is there to withhold.
	RoleReporter Role = "reporter"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Principal struct {
	Name string `json:"name"`
	Role Role   `json:"role"`
}

type TokenConfig struct {
	Name     string `yaml:"name"`
	Role     Role   `yaml:"role"`
	Token    string `yaml:"token"`
	TokenEnv string `yaml:"token_env"`
}

type AuthFile struct {
	Tokens []TokenConfig `yaml:"tokens"`
}

type AuthService struct {
	principals map[string]Principal
}

func LoadAuth(path string) (*AuthService, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read platform auth %s: %w", path, err)
	}
	var file AuthFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse platform auth: %w", err)
	}
	auth := &AuthService{principals: map[string]Principal{}}
	for _, t := range file.Tokens {
		token := strings.TrimSpace(t.Token)
		if t.TokenEnv != "" {
			if envToken := strings.TrimSpace(os.Getenv(t.TokenEnv)); envToken != "" {
				token = envToken
			}
		}
		if token == "" {
			continue
		}
		role := t.Role
		if role == "" {
			role = RoleViewer
		}
		name := t.Name
		if name == "" {
			name = string(role)
		}
		auth.principals[token] = Principal{Name: name, Role: role}
	}
	return auth, nil
}

func (a *AuthService) Capabilities(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.Authenticate(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
			"role":          RoleViewer,
			"can_operate":   false,
			"can_admin":     false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"principal":     principal.Name,
		"role":          principal.Role,
		"can_operate":   roleLevel(principal.Role) >= roleLevel(RoleOperator),
		"can_admin":     roleLevel(principal.Role) >= roleLevel(RoleAdmin),
	})
}

func (a *AuthService) Require(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := a.Authenticate(r)
			if !ok || roleLevel(principal.Role) < roleLevel(min) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": fmt.Sprintf("%s token required", min),
				})
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func (a *AuthService) Authenticate(r *http.Request) (Principal, bool) {
	if a == nil {
		return Principal{}, false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return Principal{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	for configured, principal := range a.principals {
		if subtle.ConstantTimeCompare([]byte(token), []byte(configured)) == 1 {
			return principal, true
		}
	}
	return Principal{}, false
}

func roleLevel(role Role) int {
	switch role {
	case RoleAdmin:
		return 4
	case RoleOperator:
		return 3
	case RoleReporter:
		return 2
	default:
		return 1
	}
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFromContext(ctx context.Context) Principal {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok {
		return Principal{Name: "anonymous", Role: RoleViewer}
	}
	return p
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
