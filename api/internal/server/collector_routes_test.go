package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The collector's route gate (R5:5.5-12 §1.3, S1-1).

// collectorSurfaceFile classifies every route the platform serves for a
// collector token, one "<class>\t<METHOD> <route>" line each: allow (in
// collectorRoutes), deny (refused by the gate) or outside (not under
// /api/v1, so before the gate: the anonymous /health and /metrics).
const collectorSurfaceFile = "collector_route_surface.txt"

func readSurface(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", collectorSurfaceFile))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		class, route, ok := strings.Cut(line, "\t")
		if !ok || !slices.Contains([]string{"allow", "deny", "outside"}, class) {
			t.Fatalf("%s:%d is not <allow|deny|outside>\\t<METHOD> <route>: %q", collectorSurfaceFile, i+1, line)
		}
		if _, dup := out[route]; dup {
			t.Fatalf("%s:%d classifies %s twice", collectorSurfaceFile, i+1, route)
		}
		out[route] = class
	}
	return out
}

// CollectorRouteSurface (§1.3, §1.5): every route the router serves, each
// walked through its whole middleware chain with the collector's token on a
// path that route serves. The collector reaches exactly collectorRoutes;
// every other route of /api/v1 refuses it 403 at the gate, before its own
// gate; the classification file names every route, so a route added
// without a line, or a line left for a route that is gone, fails here.
func TestCollectorRouteSurface(t *testing.T) {
	srv := reachServer(t)
	routes, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() is not a chi.Routes")
	}
	surface := readSurface(t)
	walked := map[string]bool{}
	var missing []string
	err := chi.Walk(routes, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		walked[key] = true
		path := samplePath(route)
		// The walk's path is served by the route walked, as the gate finds it.
		if got := routes.Find(chi.NewRouteContext(), method, path); got != route {
			t.Errorf("%s: the path %s is served by %q", key, path, got)
		}
		class, classified := surface[key]
		if !classified {
			missing = append(missing, key)
			return nil
		}
		reached := false
		var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+reachTokens["collector:h1"])
		req.Header.Set("X-Bifrost-Session", "reach-walk")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		switch class {
		case "allow":
			if !reached {
				t.Errorf("%s is allowed, the collector does not reach it: %d %s", key, rec.Code, rec.Body)
			}
		case "deny":
			if reached || !gateRefusal(rec) {
				t.Errorf("%s: the collector is not refused at the gate: reached %v, %d %s", key, reached, rec.Code, rec.Body)
			}
			if !strings.HasPrefix(route, "/api/v1/") {
				t.Errorf("%s is denied but outside /api/v1, where the gate is not", key)
			}
		case "outside":
			if strings.HasPrefix(route, "/api/v1/") {
				t.Errorf("%s is under /api/v1, behind the gate: classify it allow or deny", key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("unclassified route %s: add the line \"deny\\t%s\" to testdata/%s, or allow it in collectorRoutes and there (R5:5.5-12 §1.3)",
			key, key, collectorSurfaceFile)
	}
	for key := range surface {
		if !walked[key] {
			t.Errorf("testdata/%s classifies %s, which the router no longer serves", collectorSurfaceFile, key)
		}
	}
	// The code's table is the file's allow lines, and the design's: no more.
	var allowed, table []string
	for key, class := range surface {
		if class == "allow" {
			allowed = append(allowed, key)
		}
	}
	for key := range collectorRoutes {
		table = append(table, key)
	}
	sort.Strings(allowed)
	sort.Strings(table)
	if !slices.Equal(allowed, table) {
		t.Errorf("collectorRoutes %v, the file allows %v", table, allowed)
	}
	if len(table) != 9 {
		t.Errorf("collectorRoutes has %d routes, the design's table nine", len(table))
	}
	t.Logf("%d routes: %d allowed to a collector", len(walked), len(allowed))
}

// gateRefusal is the gate's own answer (actuation.ConfineCollectors).
func gateRefusal(rec *httptest.ResponseRecorder) bool {
	var body map[string]string
	return rec.Code == http.StatusForbidden && json.Unmarshal(rec.Body.Bytes(), &body) == nil && body["error"] == "forbidden" &&
		strings.HasPrefix(body["message"], "a collector token reaches only")
}

// The gate on the real router, end to end: another spelling of an allowed
// route (a trailing or doubled slash, an escaped letter, a dot segment,
// another method, HEAD, OPTIONS, a method-override header), a public route,
// an introspection route, a route of each other module and no route at all
// are refused at the gate; a path or method the root routes nowhere is
// served nothing.
// A plain reporter is as before on the same requests; the collector's own
// routes are its.
func TestCollectorGateOnTheRouter(t *testing.T) {
	srv := reachServer(t)
	router := srv.Router()
	do := func(caller, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(""))
		if caller != "" {
			req.Header.Set("Authorization", "Bearer "+reachTokens[caller])
		}
		req.Header.Set("X-Bifrost-Session", "local_0b8e2f4a")
		req.Header.Set("X-HTTP-Method-Override", "GET")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range []struct{ method, target string }{
		{"GET", "/api/v1/agentrt/whoami/"},
		{"GET", "/api/v1//agentrt/whoami"},
		{"GET", "/api/v1/agentrt/%77hoami"},
		{"GET", "/api/v1/agentrt/./whoami"},
		{"HEAD", "/api/v1/agent/threads"},
		{"OPTIONS", "/api/v1/agent/threads"},
		{"POST", "/api/v1/agent/threads"},
		{"GET", "/api/v1/agent/threads/heartbeat"},
		{"DELETE", "/api/v1/agentrt/artifacts/sha256:0"},
		{"GET", "/api/v1/agentrt/plan-view?root=R2"},
		{"GET", "/api/v1/agentrt/events?stream=system"},
		{"GET", "/api/v1/agentrt/artifacts"},
		{"POST", "/api/v1/agentrt/controls"},
		{"POST", "/api/v1/agentrt/rules/routing:dev/tighten"},
		{"POST", "/api/v1/agentrt/missions"},
		{"PUT", "/api/v1/lineage/transcript-title"},
		{"PUT", "/api/v1/agent/progress"},
		{"POST", "/api/v1/agent/governance/skill-runs"},
		{"POST", "/api/v1/ops-agent/alertmanager"},
		{"GET", "/api/v1/questions"},
		{"GET", "/api/v1/environments"},
		{"GET", "/api/v1/auth/capabilities"},
		{"GET", "/api/v1/audit"},
		{"GET", "/api/v1/no-such-route"},
	} {
		if rec := do("collector:h1", c.method, c.target); !gateRefusal(rec) {
			t.Errorf("%s %s as the collector: %d %s", c.method, c.target, rec.Code, rec.Body)
		}
		if rec := do("reporter", c.method, c.target); gateRefusal(rec) {
			t.Errorf("%s %s as a plain reporter is refused at the collector's gate", c.method, c.target)
		}
	}
	// Before /api/v1 routes at all, nothing is served: a path outside it,
	// and a method chi does not know (the root answers 405).
	if rec := do("collector:h1", "GET", "/API/v1/agentrt/whoami"); rec.Code != http.StatusNotFound {
		t.Errorf("a path outside /api/v1: %d %s", rec.Code, rec.Body)
	}
	if rec := do("collector:h1", "PROPFIND", "/api/v1/agentrt/whoami"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("a method chi does not know: %d %s", rec.Code, rec.Body)
	}
	beat := `{"thread":"0b8e2f4a-1111-2222-3333-444455556666","vendor":"claude","host":"h1","event":"turn_start","turn_id":"turn-1","seq":1}`
	req := httptest.NewRequest("POST", "/api/v1/agent/threads/heartbeat", strings.NewReader(beat))
	req.Header.Set("Authorization", "Bearer "+reachTokens["collector:h1"])
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("the collector's own heartbeat: %d %s", rec.Code, rec.Body)
	}
	if rec := do("collector:h1", "GET", "/api/v1/agent/threads"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"host":"h1"`) {
		t.Fatalf("the collector's own list: %d %s", rec.Code, rec.Body)
	}
}

// CollectorTitleRefused, the title route (§1.5): the collector's PUT of a
// transcript title is refused at the gate and stores nothing; the
// reporter's path is as before (its title store is not reached in a test
// either way: the gate decides first). The heartbeat half is
// agentthreads.TestCollectorTitleRefused.
func TestCollectorTitleRefused(t *testing.T) {
	srv := reachServer(t)
	router := srv.Router()
	for _, target := range []string{"/api/v1/lineage/transcript-title", "/api/v1/lineage/thread-title"} {
		req := httptest.NewRequest("PUT", target, strings.NewReader(`{"transcript":"0b8e2f4a-1111-2222-3333-444455556666","title":"x"}`))
		req.Header.Set("Authorization", "Bearer "+reachTokens["collector:h1"])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if !gateRefusal(rec) {
			t.Fatalf("the collector's PUT %s: %d %s", target, rec.Code, rec.Body)
		}
	}
	beat := `{"thread":"0b8e2f4a-1111-2222-3333-444455556666","vendor":"claude","host":"h1","event":"turn_start","turn_id":"turn-1","seq":1,"title":"x"}`
	req := httptest.NewRequest("POST", "/api/v1/agent/threads/heartbeat", strings.NewReader(beat))
	req.Header.Set("Authorization", "Bearer "+reachTokens["collector:h1"])
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("the collector's heartbeat with a title through the router: %d %s", rec.Code, rec.Body)
	}
}
