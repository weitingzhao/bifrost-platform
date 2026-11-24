//bifrost:owner plan

package server

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/runtimedb"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/schemawatch"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

const agentRuntimeAuthYAML = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-agentrt
  - name: agentd:mac-dev
    role: agentd
    token: fixture-agentd-agentrt
`

// Without AGENTRT_DSN the agent runtime's routes are mounted but answer 503,
// after authentication: an anonymous write is still refused as such.
func TestAgentRuntimeRoutesWithoutDatabase(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	t.Setenv("PLATFORM_AGENT_THREAD_WATCH", "off")
	t.Setenv("AGENTRT_DSN", "")
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(agentRuntimeAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, cfg)
	do := func(method, path, token, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do(http.MethodGet, "/api/v1/agentrt/progress", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous read: %d, want 401", code)
	}
	if code := do(http.MethodGet, "/api/v1/agentrt/whoami", "fixture-viewer-agentrt", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("whoami without a database: %d, want 503", code)
	}
}

// Review 528 A1b: the routes open with the moment they did already recorded,
// and /health says agentrt_ready only then: no reader sees the routes ready
// without the moment the API's silence counting starts from.
func TestTheRoutesOpenWithTheirMoment(t *testing.T) {
	db := agentrttest.Migrated(t)
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	srv := newTestServer(t, newTestConfig(t))
	late := &runtimedb.Late{}
	srv.agentRuntime = &agentRuntime{schema: &schemawatch.Watch{}, late: late}
	ready := func() any {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		var body map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("health %d %s", rec.Code, rec.Body.String())
		}
		return body["agentrt_ready"]
	}
	if late.Ready() || !late.Back().IsZero() || ready() != false {
		t.Fatalf("before connecting: ready %v back %v health %v", late.Ready(), late.Back(), ready())
	}
	// Whatever moment a reader catches between the two stores, it never sees
	// the routes ready with no moment recorded.
	at := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !late.Ready() {
		}
		if late.Back().IsZero() {
			t.Error("the routes were ready before their moment was recorded")
		}
	}()
	late.Open(pool, at)
	<-done
	if !late.Ready() || !late.Back().Equal(at) || ready() != true {
		t.Fatalf("after connecting: ready %v back %v health %v", late.Ready(), late.Back(), ready())
	}
}

// historyGitea serves one repo's main in the delivery org "bifrost" from
// commits (sha → message, parents) and no result commit in either org: every
// read below recovers the result from main's history.
type historyGitea struct {
	head    string
	commits map[string][2][]string // sha → {message}, parents
	results map[string]int         // org → result reads
}

func (g *historyGitea) add(sha, msg string, parents ...string) {
	g.commits[sha] = [2][]string{{msg}, parents}
	g.head = sha
}

func (g *historyGitea) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/api/v1/repos/bifrost/repo/branches/main":
		_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"id": g.head}})
	case strings.HasPrefix(p, "/api/v1/repos/agents/repo/git/commits/"):
		g.results["agents"]++
		http.Error(w, "not found", http.StatusNotFound)
	case strings.HasPrefix(p, "/api/v1/repos/bifrost/repo/git/commits/"):
		g.results["bifrost"]++
		http.Error(w, "not found", http.StatusNotFound)
	case p == "/api/v1/repos/bifrost/repo/commits":
		out := []map[string]any{}
		if r.URL.Query().Get("page") == "1" {
			seen := map[string]bool{}
			for q := []string{r.URL.Query().Get("sha")}; len(q) > 0; q = q[1:] {
				c, ok := g.commits[q[0]]
				if !ok || seen[q[0]] {
					continue
				}
				seen[q[0]] = true
				parents := []map[string]any{}
				for _, x := range c[1] {
					parents = append(parents, map[string]any{"sha": x})
				}
				out = append(out, map[string]any{"sha": q[0], "commit": map[string]any{"message": c[0][0], "committer": map[string]any{"date": time.Now()}}, "parents": parents})
				q = append(q, c[1]...)
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	default:
		http.Error(w, "unexpected "+p, http.StatusNotFound)
	}
}

// Origin: 2026-11-20, domain wall (OWNER_LAW #95): lineage's plan history
// reader imported verification's landinggit to judge main's history; now it
// takes the rules through lineage.HistoryRules and server binds landinggit
// there (planHistoryRules). This is the PV2 table and the borrowed-Change-Id
// case that lived in lineage's planhistory_regression_test.go until then,
// unchanged in input and expectation, run through the real binding: a bad
// binding (a trailer rule swapped for another) turns rows.
func TestPlanHistoryReadsThroughLandingRules(t *testing.T) {
	t.Parallel()
	sha := func(c string) string { return strings.Repeat(c, 40) }
	read := func(t *testing.T, g *historyGitea, task, result string) (landing.MainHistory, error) {
		t.Helper()
		srv := httptest.NewServer(g)
		t.Cleanup(srv.Close)
		r := &lineage.PlanHistoryReader{Access: func(context.Context) (lineage.Access, error) {
			return lineage.Access{Base: srv.URL, Org: "bifrost", AgentOrg: "agents"}, nil
		}, Client: srv.Client(), MaxPages: 10, Rules: planHistoryRules()}
		return r.History(context.Background(), "repo", task, result, "")
	}
	a, b, c, rv, base := sha("a"), sha("b"), sha("c"), sha("d"), sha("0")
	const task = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	line := "(cherry picked from commit " + a + ")"
	trailers := "Agentrt-Task: " + task + "\nChange-Id: I" + b
	for _, tt := range []struct {
		name, task, message string
		want                landing.History
	}{
		{"quoted body and no result", "", "documentation\n\nDo not use this old marker: " + line + "\n\nChange-Id: I" + b, landing.Unknown},
		{"no bound task", "", "result\n\n" + trailers + "\n" + line, landing.Unknown},
		{"missing task trailer", task, "result\n\nChange-Id: I" + b + "\n" + line, landing.Unknown},
		{"wrong task", "other", "result\n\n" + trailers + "\n" + line, landing.Unknown},
		{"duplicate task", task, "result\n\n" + trailers + "\nAgentrt-Task: " + task + "\n" + line, landing.Unknown},
		{"malformed second task", task, "result\n\n" + trailers + "\nAgentrt-Task: two values\n" + line, landing.Unknown},
		{"continued task", task, "result\n\nAgentrt-Task: " + task + "\n extra\nChange-Id: I" + b + "\n" + line, landing.Unknown},
		{"quoted provenance with bound task", task, "result\n\nExample: " + line + "\n\n" + trailers, landing.Unknown},
		{"duplicate provenance", task, "result\n\n" + trailers + "\n" + line + "\n" + line, landing.Unknown},
		{"exact task and provenance", task, "result\n\n" + trailers + "\n" + line, landing.Effective},
		{"bound task without Change-Id", task, "result\n\nAgentrt-Task: " + task + "\n" + line, landing.Effective},
		{"separate provenance paragraph", task, "result\n\n" + trailers + "\n\n" + line, landing.Effective},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := &historyGitea{commits: map[string][2][]string{}, results: map[string]int{}}
			g.add(base, "base")
			g.add(b, tt.message, base)
			h, err := read(t, g, tt.task, a)
			if h.State != tt.want || (err != nil) != (tt.want == landing.Unknown) {
				t.Fatalf("state=%s error=%v; want %s", h.State, err, tt.want)
			}
			if g.results["agents"] != 1 || g.results["bifrost"] != 1 {
				t.Fatalf("fallback did not try both result organizations: %v", g.results)
			}
		})
	}

	// A candidate's own Change-Id says nothing about the missing queried
	// result; importing it would falsely make this ambiguous.
	g := &historyGitea{commits: map[string][2][]string{}, results: map[string]int{}}
	g.add(base, "base")
	g.add(b, "result\n\nAgentrt-Task: "+task+"\nChange-Id: I"+b+"\n"+line, base)
	g.add(rv, "Revert \"result\"\n\nThis reverts commit "+b+".", b)
	g.add(c, "other attempt\n\nChange-Id: I"+b, rv)
	if h, err := read(t, g, task, a); err != nil || h.State != landing.Reverted || h.Commit != b || h.Snapshot != c {
		t.Fatalf("fallback imported candidate identity: %+v %v", h, err)
	}
}
