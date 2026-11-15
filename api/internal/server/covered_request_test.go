package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// Origin: 2026-11-15. The policy's apply clause was signed at 07:39 and the
// migration clause at 11:46; POST /api/v1/approvals answered "call directly"
// for both, and the session that asked had no tool for either route (its tool
// list dates from its start). Three cluster objects and migration 0022 waited
// for a new session that could not be opened. The request is now run where
// the policy covers it.
//
// The router here is a real chi mux with the middleware on it, so the second
// pass is routed the way production routes it; the approval service and the
// action's route are stubs that record what reached them.
func coveredHarness(t *testing.T, approvalsAnswer func(w http.ResponseWriter)) (http.Handler, *[]string, *[]map[string]any) {
	t.Helper()
	var reached []string
	var bodies []map[string]any
	r := chi.NewRouter()
	r.Use(coveredRequestRuns(r))
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/approvals", func(w http.ResponseWriter, _ *http.Request) {
			reached = append(reached, "approvals")
			approvalsAnswer(w)
		})
		for _, route := range []string{"/actuation/manifests/apply", "/agentrt/migrations/run", "/delivery/pipelines/{name}/runs"} {
			route := route
			r.Post(route, func(w http.ResponseWriter, req *http.Request) {
				reached = append(reached, route+" as "+req.Header.Get("Authorization"))
				raw, _ := io.ReadAll(req.Body)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)
				bodies = append(bodies, body)
				writeJSON(w, http.StatusAccepted, map[string]string{"apply_run": "apply-1"})
			})
		}
	})
	return r, &reached, &bodies
}

func callDirectly(policy string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		body := map[string]any{"error": "call directly", "action": "x", "tier": "C"}
		if policy != "" {
			body["auto_approved_by"] = policy
		}
		writeJSON(w, http.StatusBadRequest, body)
	}
}

func ask(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer caller")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestACoveredRequestIsRunOnTheActionsOwnRoute(t *testing.T) {
	h, reached, bodies := coveredHarness(t, callDirectly("rp-1"))
	rec := ask(h, `{"action":"apply_manifest","params":{"plan_id":"plan-1"},"reason":"why","rollback":"how back"}`)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "apply-1") || rec.Header().Get(RanDirectlyHeader) != "rp-1" {
		t.Fatalf("answer = %d %s header=%q", rec.Code, rec.Body.String(), rec.Header().Get(RanDirectlyHeader))
	}
	want := []string{"approvals", "/actuation/manifests/apply as Bearer caller"}
	if strings.Join(*reached, "|") != strings.Join(want, "|") {
		t.Fatalf("reached %v, want %v (the action's route, with the caller's own credentials)", *reached, want)
	}
	got := (*bodies)[0]
	if got["plan_id"] != "plan-1" || got["reason"] != "why" || got["rollback"] != "how back" || got["unchecked"] == "" {
		t.Fatalf("the action's route got %v", got)
	}

	h, reached, bodies = coveredHarness(t, callDirectly("rp-2"))
	if rec := ask(h, `{"action":"run_agentrt_migration","params":{"commit":"abc"},"reason":"r"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("migration: %d %s", rec.Code, rec.Body.String())
	}
	if len(*reached) != 2 || !strings.HasPrefix((*reached)[1], "/agentrt/migrations/run") || (*bodies)[0]["commit"] != "abc" {
		t.Fatalf("migration reached %v with %v", *reached, *bodies)
	}
}

// The controls: everything that is not "this action, covered by a named
// policy" is the approval service's own answer, and the action's route is not
// touched.
func TestOnlyACoveredRequestOfAListedActionIsRun(t *testing.T) {
	created := func(w http.ResponseWriter) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "appr_1", "status": "pending"})
	}
	cases := []struct {
		name   string
		answer func(http.ResponseWriter)
		body   string
		code   int
	}{
		{"not covered: a card is created", created, `{"action":"apply_manifest","params":{"plan_id":"p"}}`, http.StatusCreated},
		{"tier B call directly names no policy", callDirectly(""), `{"action":"apply_manifest","params":{"plan_id":"p"}}`, http.StatusBadRequest},
		{"start_pipeline_run keeps its own door", callDirectly("rp-1"), `{"action":"start_pipeline_run","params":{"name":"bifrost-deliver-platform-prod"}}`, http.StatusBadRequest},
		{"an action that is not listed", callDirectly("rp-1"), `{"action":"scale_deployment","params":{}}`, http.StatusBadRequest},
		{"a refusal", func(w http.ResponseWriter) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		},
			`{"action":"apply_manifest","params":{"plan_id":"p"}}`, http.StatusForbidden},
		{"not JSON", callDirectly("rp-1"), `{`, http.StatusBadRequest},
	}
	for _, c := range cases {
		h, reached, _ := coveredHarness(t, c.answer)
		rec := ask(h, c.body)
		if rec.Code != c.code || rec.Header().Get(RanDirectlyHeader) != "" {
			t.Errorf("%s: answer = %d header=%q", c.name, rec.Code, rec.Header().Get(RanDirectlyHeader))
		}
		if len(*reached) != 1 || (*reached)[0] != "approvals" {
			t.Errorf("%s: reached %v, want the approval service alone", c.name, *reached)
		}
	}
}

// Every listed action's route takes its parameters from the body and nothing
// from the path: the second pass has only a body to give.
func TestRoutesOfRequestRunsDirectlyHaveNoPathParameter(t *testing.T) {
	for action, route := range requestRunsDirectly {
		if strings.ContainsAny(route, "{}") || !strings.HasPrefix(route, "/api/v1/") {
			t.Errorf("%s: route %q", action, route)
		}
		act, ok := actions.ByID(action)
		if !ok || act.Method != http.MethodPost || act.Pattern != route {
			t.Errorf("%s: the catalog has %s %q, the list says POST %q", action, act.Method, act.Pattern, route)
		}
	}
}
