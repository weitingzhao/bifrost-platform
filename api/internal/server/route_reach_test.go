package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// The route reach of every principal (W-31 BI-1 slice 2, DESIGN-crew-intake
// §5.2 「全路由测试」 ①): every route of the platform, walked from its
// router, its middleware chain run around a sentinel, from each principal.
// The gate layer only: no handler runs, so a parameter, a body or a store
// not configured never reads as a refusal. This file uses nothing a platform
// before slice 2 lacks, so the same walk run on the base commit gives the
// old roles' reach the test compares with (testdata/route_reach_base.txt).

// reachPrincipals are the callers walked, by name, with their tokens in
// reachAuthFile; "" is anonymous.
var reachPrincipals = []string{"", "viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1", "executor", "intake:dots-p2"}

const reachIntakeEnv = "SERVER_TEST_INTAKE_TOKEN"

var reachTokens = map[string]string{"viewer": "fixture-reach-viewer", "reporter": "fixture-reach-reporter", "operator": "fixture-reach-operator",
	"admin": "fixture-reach-admin", "trade-satellite": "fixture-reach-satellite", "agentd:h1": "fixture-reach-agentd",
	"executor": "fixture-reach-executor", "intake:dots-p2": "fixture-reach-intake"}

// reachAuthFile is the platform's auth file of the walk: one token of each
// role PROD and STG use, an executor, and an intake client (from its own
// key, which a platform before slice 2 ignores).
const reachAuthFile = `tokens:
  - name: viewer
    role: viewer
    token: fixture-reach-viewer
  - name: reporter
    role: reporter
    token: fixture-reach-reporter
  - name: operator
    role: operator
    token: fixture-reach-operator
  - name: admin
    role: admin
    token: fixture-reach-admin
  - name: trade-satellite
    role: operator
    token: fixture-reach-satellite
  - name: agentd:h1
    role: agentd
    token: fixture-reach-agentd
  - name: executor
    role: executor
    token: fixture-reach-executor
intake_tokens:
  - name: intake:dots-p2
    token_env: ` + reachIntakeEnv + `
`

// noDB is a database that is configured and refuses every transaction: the
// agent runtime's routes get past their configured check, and no handler
// runs here anyway.
type noDB struct{}

func (noDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("no database in the route walk")
}

// reachServer is a platform with the walk's auth file and a configured
// agent runtime.
func reachServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv(reachIntakeEnv, reachTokens["intake:dots-p2"])
	cfg := newTestConfig(t)
	path := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(path, []byte(reachAuthFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = path
	srv := newTestServer(t, cfg)
	srv.agentRuntime.api.DB = noDB{}
	return srv
}

// routeReach is, for each principal, the routes ("METHOD /path") whose
// middleware chain lets it through to the handler.
func routeReach(t *testing.T, srv *Server) map[string][]string {
	t.Helper()
	routes, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() is not a chi.Routes")
	}
	out := map[string][]string{}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		key := method + " " + strings.TrimSuffix(route, "/")
		if route == "/" {
			key = method + " /"
		}
		for _, p := range reachPrincipals {
			reached := false
			var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
			for i := len(mws) - 1; i >= 0; i-- {
				h = mws[i](h)
			}
			req := httptest.NewRequest(method, "/", nil)
			if p != "" {
				req.Header.Set("Authorization", "Bearer "+reachTokens[p])
			}
			req.Header.Set("X-Bifrost-Session", "reach-walk")
			h.ServeHTTP(httptest.NewRecorder(), req)
			if reached {
				out[p] = append(out[p], key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	for p := range out {
		sort.Strings(out[p])
	}
	return out
}

// reachText is a reach as the base file holds it: "<principal>\t<route>"
// lines, sorted; anonymous is "-".
func reachText(reach map[string][]string, principals []string) string {
	var b strings.Builder
	for _, p := range principals {
		name := p
		if name == "" {
			name = "-"
		}
		for _, r := range reach[p] {
			b.WriteString(name + "\t" + r + "\n")
		}
	}
	return b.String()
}
