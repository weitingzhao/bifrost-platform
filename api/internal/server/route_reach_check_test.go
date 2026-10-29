package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
)

// The full-route test of DESIGN-crew-intake §5.2 (B3), on every route the
// platform serves, a route added later included:
//
//  1. the gate layer: an intake token gets through to no handler an
//     anonymous caller does not, but for the seven intake routes, and to all
//     seven;
//  2. the public routes that read a token in their handler, listed: what
//     they tell an intake token is who it is and that it may do nothing;
//     every other handler reads no token (a source scan holds it);
//  3. the old roles: each reaches what it reached on the base commit
//     (testdata/route_reach_base.txt, the same walk run there), the issue
//     readers (operator, admin) and only they reaching the crew's issue
//     routes besides, plus the explicit intended additions below.
//
// With ROUTE_REACH_OUT set, the old roles' reach is written there instead of
// checked (run on a base commit to remake the file).

// issueReaderRoutes are the crew's issue routes the readers list opens
// (agentrtapi/issues.go).
var issueReaderRoutes = regexp.MustCompile(`^(GET|POST) /api/v1/agentrt/issues(/merge|/\{id\}(/(additions|responses|brief|classification|design|reviews|close-checks|owner-cards|take|replies|close|park|resume|reopen))?)?$`)

var oldPrincipals = []string{"", "viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1", "executor"}

// Intended new routes are exact route/principal additions to the historical
// base, not a regenerated snapshot or a prefix exemption. Both extra reach and
// missing expected reach remain failures. The write route matches firewall/apply:
// this walk reaches the guarded handler, not the controller operation; the
// handler-level approval refusal is checked by TestFixedIPWriteMatchesFirewallApprovalBoundary.
var intendedRouteAdditions = map[string][]string{
	"GET /api/v1/ops/facts/system":  {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/agentrt/plan-view": {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	// S3 B3: what a landing card asked and what became of its answer, for
	// every reader (CUT §11.6 v3 item 8).
	"GET /api/v1/agentrt/landing/cards/{question_id}": {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	// S3 B4: a mission's landing status, for every reader (CUT §11.8 v3).
	"GET /api/v1/agentrt/landing/missions/{id}":      {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	"GET /api/v1/agentrt/external-work/{id}":         {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	"POST /api/v1/agentrt/external-work":             {"reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/agentrt/external-work/{id}/events": {"reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/network/clients/{mac}":              {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/network/clients/fixed-ip":          {"operator", "admin", "trade-satellite"},
}

// Work 611 narrows plain reporters to external-work registration and their
// own observations. Keep the historical snapshot; remove only these exact
// reporter/proposal pairs, so either restored access or another lost route fails.
var reporterProposalRemovals = []string{
	"POST /api/v1/agentrt/missions",
	"POST /api/v1/agentrt/missions/{id}/tasks",
	"POST /api/v1/agentrt/goals",
	"POST /api/v1/agentrt/messages",
	"POST /api/v1/agentrt/messages/ack",
	"POST /api/v1/agentrt/plan/proposals",
	"POST /api/v1/agentrt/plan/imports",
	"POST /api/v1/agentrt/plan/{id}/notes",
	"POST /api/v1/agentrt/rules/{rule_id}/proposals",
	"POST /api/v1/agentrt/rules/{rule_id}/sign",
}

func TestEveryRouteKeepsTheIntakeTokenToItsOwn(t *testing.T) {
	srv := reachServer(t)
	reach := routeReach(t, srv)
	if out := os.Getenv("ROUTE_REACH_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(reachText(reach, oldPrincipals)), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	anon := map[string]bool{}
	for _, r := range reach[""] {
		anon[r] = true
	}
	intake := agentrtapi.IntakeRoutes()
	if len(intake) != 7 {
		t.Fatalf("the intake routes are %d, not the design's seven: %v", len(intake), intake)
	}
	for _, r := range reach["intake:dots-p2"] {
		if !anon[r] && !slices.Contains(intake, r) {
			t.Errorf("an intake token gets through to %s, which an anonymous caller does not", r)
		}
	}
	for _, r := range intake {
		if !slices.Contains(reach["intake:dots-p2"], r) {
			t.Errorf("an intake token does not reach its own %s", r)
		}
		for _, p := range oldPrincipals {
			if slices.Contains(reach[p], r) {
				t.Errorf("%q reaches the intake route %s", p, r)
			}
		}
	}

	// The old roles reach what they did on the base commit; the readers add
	// the crew's issue routes and the exact intended additions, nothing else changes.
	raw, err := os.ReadFile(filepath.Join("testdata", "route_reach_base.txt"))
	if err != nil {
		t.Fatal(err)
	}
	base := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		p, r, _ := strings.Cut(line, "\t")
		if p == "-" {
			p = ""
		}
		base[p] = append(base[p], r)
	}
	if len(base) != len(oldPrincipals) {
		t.Fatalf("the base file holds %d principals", len(base))
	}
	for _, p := range oldPrincipals {
		now, then := reach[p], slices.Clone(base[p])
		if p == "reporter" {
			for _, route := range reporterProposalRemovals {
				if !slices.Contains(then, route) {
					t.Fatalf("reporter removal %s was not in the base", route)
				}
			}
			then = slices.DeleteFunc(then, func(route string) bool {
				return slices.Contains(reporterProposalRemovals, route)
			})
		}
		for route, principals := range intendedRouteAdditions {
			if slices.Contains(principals, p) {
				then = append(then, route)
			}
		}
		for _, r := range now {
			if !slices.Contains(then, r) && !(issueReaderRoutes.MatchString(r) && (p == "operator" || p == "admin")) {
				t.Errorf("%q reaches %s, which it did not on the base commit", p, r)
			}
		}
		for _, r := range then {
			if !slices.Contains(now, r) {
				t.Errorf("%q no longer reaches %s", p, r)
			}
		}
		readers := 0
		for _, r := range now {
			if issueReaderRoutes.MatchString(r) {
				readers++
			}
		}
		if want := map[bool]int{true: 18, false: 0}[p == "operator" || p == "admin"]; readers != want {
			t.Errorf("%q reaches %d of the crew's issue routes, want %d", p, readers, want)
		}
	}
	t.Logf("%d routes reached by an intake token (%d by an anonymous caller)", len(reach["intake:dots-p2"]), len(reach[""]))
}

// The public routes that read a token in their handler (the gate layer
// cannot see them), each with what it may tell an intake token.
var publicIntrospection = map[string]func(t *testing.T, body map[string]any){
	"GET /api/v1/auth/capabilities": func(t *testing.T, body map[string]any) {
		if body["authenticated"] != true || body["principal"] != "intake:dots-p2" || body["role"] != "intake" ||
			body["can_operate"] != false || body["can_admin"] != false {
			t.Fatalf("capabilities for an intake token: %v", body)
		}
	},
}

func TestPublicIntrospectionTellsAnIntakeTokenNothingMore(t *testing.T) {
	srv := reachServer(t)
	for route, check := range publicIntrospection {
		method, path, _ := strings.Cut(route, " ")
		for _, caller := range []string{"", "intake:dots-p2"} {
			req := httptest.NewRequest(method, path, nil)
			if caller != "" {
				req.Header.Set("Authorization", "Bearer "+reachTokens[caller])
			}
			rec := httptest.NewRecorder()
			srv.Router().ServeHTTP(rec, req)
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("%s as %q: %d %s", route, caller, rec.Code, rec.Body)
			}
			if caller != "" {
				check(t, body)
			} else if body["authenticated"] != false {
				t.Fatalf("%s anonymous: %v", route, body)
			}
		}
	}
	// No other handler reads a token: only the auth service authenticates
	// (Require, RequireAny, RequireIssueReader and the capabilities route),
	// and the relay of the operator plane, served by another binary, reads
	// its own. A handler on a public route gets the anonymous principal
	// whatever token comes with the request.
	readers := map[string]bool{}
	pattern := regexp.MustCompile(`\.Authenticate\(|Header\.Get\("Authorization"\)`)
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if pattern.Match(src) {
			readers[filepath.ToSlash(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"../actuation/auth.go": true, "../alertrelay/relay.go": true}
	for p := range readers {
		if !want[p] {
			t.Errorf("%s reads a token in a handler: list the public route it serves in publicIntrospection", p)
		}
	}
	for p := range want {
		if !readers[p] {
			t.Errorf("%s no longer reads a token: drop it from the list", p)
		}
	}
}
