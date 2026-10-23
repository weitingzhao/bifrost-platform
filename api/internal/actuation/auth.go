package actuation

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
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
	// carries `POST /cluster/workloads/scale`. That would let the
	// Loop scale the trade `daemon`, which D10 forbids outright.
	RoleReporter Role = "reporter"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
	// RoleExecutor is the out-of-band executor. It may claim approved work,
	// renew its lease and post the result, and nothing else: it sits below
	// viewer, so no Require(min) route accepts it, and only RequireAny routes
	// that name it do.
	RoleExecutor Role = "executor"
	// RoleAgentd is an Agent host's daemon (design v5 §5.2 鉴权): its token is
	// bound to one host, named in the principal as "agentd:<host_id>", and
	// may only claim work, renew leases, hand results over and upload
	// artifacts, for that host. Like executor it sits below viewer.
	RoleAgentd Role = "agentd"
	// RoleIntake is an external agent bringing business issues (W-31 BI-1,
	// DESIGN-crew-intake-2026-10-22 §5.2): its token is named
	// "intake:<name>", is configured only under intake_tokens (never under
	// tokens), and opens the intake routes that name it and nothing else.
	// Like executor and agentd it sits below viewer, so no Require(min) route
	// accepts it.
	RoleIntake Role = "intake"
)

// IntakePrefix starts the principal name of an intake token; the client's
// name follows it, as its actor agent:<name> has it.
const IntakePrefix = "intake:"

// intakeName is an intake token's name (§5.2: intake:<name>, the name as an
// agent: actor allows it).
var intakeName = regexp.MustCompile(`^intake:[a-z0-9-]{1,32}$`)

// IntakeClient is the runtime actor an intake principal writes and reads as
// (agent:<name>).
func IntakeClient(p Principal) (string, bool) {
	if p.Role != RoleIntake || !intakeName.MatchString(p.Name) {
		return "", false
	}
	return "agent:" + strings.TrimPrefix(p.Name, IntakePrefix), true
}

// tokenRoles are the roles a tokens: entry may name (§5.2: a closed set; ""
// is viewer). intake is not one: its tokens are read from intake_tokens only.
var tokenRoles = map[Role]bool{RoleViewer: true, RoleReporter: true, RoleOperator: true, RoleAdmin: true, RoleExecutor: true, RoleAgentd: true}

// AgentdPrefix starts the principal name of an agentd token; the host id
// follows it.
const AgentdPrefix = "agentd:"

// AgentdHost is the host an agentd principal is bound to.
func AgentdHost(p Principal) (string, bool) {
	host, ok := strings.CutPrefix(p.Name, AgentdPrefix)
	if p.Role != RoleAgentd || !ok || host == "" {
		return "", false
	}
	return host, true
}

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

// IntakeTokenConfig is an intake token (§5.2): its name and the
// environment variable its value comes from; its role is intake. Role and
// Token are read only to refuse them: an intake token states no role and
// carries no inline value.
type IntakeTokenConfig struct {
	Name     string `yaml:"name"`
	TokenEnv string `yaml:"token_env"`
	Role     Role   `yaml:"role"`
	Token    string `yaml:"token"`
}

// AuthFile is the platform's token configuration. IntakeTokens is a key of
// its own (intake_tokens, DESIGN-crew-intake §5.2, R2-A6): a platform from
// before it reads only tokens and ignores a key it does not know, so on a
// rollback an intake token is no token at all, never a viewer by an old
// ladder's default. IssueReaders names the operator and admin tokens that
// read business issues (§5.3; absent: operator and admin).
type AuthFile struct {
	Tokens       []TokenConfig       `yaml:"tokens"`
	IntakeTokens []IntakeTokenConfig `yaml:"intake_tokens"`
	IssueReaders *[]string           `yaml:"issue_readers"`
}

type AuthService struct {
	principals map[string]Principal
	// issueReaders are the token names that read business issues, beside
	// the role (§5.3: trade-satellite is an operator and reads none).
	issueReaders map[string]bool
}

// DefaultIssueReaders read business issues when the configuration names
// none (§5.3).
var DefaultIssueReaders = []string{"operator", "admin"}

// placeholderMarkers flag inline values that only exist to be overridden by
// token_env. They sit in a public repo, so one that is not overridden must not
// become a credential: a shared placeholder once authenticated as admin because
// principals are keyed by token and the last role holding it won.
var placeholderMarkers = []string{"placeholder", "replace_me", "replace-me", "changeme", "change-me"}

func isPlaceholderToken(token string) bool {
	lower := strings.ToLower(token)
	for _, marker := range placeholderMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// LoadAuth fails when two roles resolve to the same token, since one token can
// only map to one principal and which role survives would depend on file order.
func LoadAuth(path string) (*AuthService, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read platform auth %s: %w", path, err)
	}
	var file AuthFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse platform auth: %w", err)
	}
	auth := &AuthService{principals: map[string]Principal{}, issueReaders: map[string]bool{}}
	readers := DefaultIssueReaders
	if file.IssueReaders != nil {
		readers = *file.IssueReaders
	}
	for _, name := range readers {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("platform auth: issue_readers names an empty token")
		}
		auth.issueReaders[name] = true
	}
	for _, t := range file.Tokens {
		token := strings.TrimSpace(t.Token)
		if isPlaceholderToken(token) {
			token = ""
		}
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
		if role == RoleIntake {
			return nil, fmt.Errorf("platform auth: token %q: an intake token is configured under intake_tokens, not tokens", t.Name)
		}
		if !tokenRoles[role] {
			return nil, fmt.Errorf("platform auth: token %q has role %q, not one of viewer, reporter, operator, admin, executor, agentd", t.Name, role)
		}
		name := t.Name
		if name == "" {
			name = string(role)
		}
		if role == RoleAgentd {
			if _, ok := AgentdHost(Principal{Name: name, Role: role}); !ok {
				return nil, fmt.Errorf("platform auth: agentd token %q must be named %s<host_id>", name, AgentdPrefix)
			}
		}
		if prev, dup := auth.principals[token]; dup {
			return nil, fmt.Errorf("platform auth: %q and %q resolve to the same token", prev.Name, name)
		}
		auth.principals[token] = Principal{Name: name, Role: role}
	}
	for _, t := range file.IntakeTokens {
		switch {
		case !intakeName.MatchString(t.Name):
			return nil, fmt.Errorf("platform auth: intake token %q must be named %s<name> (a-z, 0-9, -; at most 32)", t.Name, IntakePrefix)
		case t.Role != "" || t.Token != "":
			return nil, fmt.Errorf("platform auth: intake token %q states no role and no inline token: its role is intake, its value comes from token_env", t.Name)
		case t.TokenEnv == "":
			return nil, fmt.Errorf("platform auth: intake token %q names its token_env", t.Name)
		}
		token := strings.TrimSpace(os.Getenv(t.TokenEnv))
		if token == "" {
			continue
		}
		if prev, dup := auth.principals[token]; dup {
			return nil, fmt.Errorf("platform auth: %q and %q resolve to the same token", prev.Name, t.Name)
		}
		auth.principals[token] = Principal{Name: t.Name, Role: RoleIntake}
	}
	return auth, nil
}

// IssueReader says whether a principal reads business issues (§5.3): an
// operator or admin token whose name is on the readers list.
func (a *AuthService) IssueReader(p Principal) bool {
	return a != nil && (p.Role == RoleOperator || p.Role == RoleAdmin) && a.issueReaders[p.Name]
}

// RequireIssueReader accepts the issue readers only (§5.3): the operator and
// admin tokens the readers list names. A token not on it is refused as one
// that may not read issues, whatever its role.
func (a *AuthService) RequireIssueReader() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := a.Authenticate(r)
			if !ok || !a.IssueReader(principal) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "an issue reader's token required"})
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
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

// RequireAny accepts exactly the listed roles; the ladder does not apply.
func (a *AuthService) RequireAny(roles ...Role) func(http.Handler) http.Handler {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, string(r))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := a.Authenticate(r)
			if ok {
				for _, want := range roles {
					if principal.Role == want {
						next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
						return
					}
				}
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": strings.Join(names, " or ") + " token required",
			})
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

// roleLevel is a role's rung on the ladder Require reads. It fails closed
// (DESIGN-crew-intake §5.2): executor, agentd, intake and any role not
// listed here sit below viewer, so no Require(min) route accepts them.
func roleLevel(role Role) int {
	switch role {
	case RoleAdmin:
		return 4
	case RoleOperator:
		return 3
	case RoleReporter:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
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
