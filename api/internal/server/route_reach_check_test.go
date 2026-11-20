//bifrost:owner foundation

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

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
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

var oldPrincipals = []string{"", "viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1", "executor"}

// Intended new routes are exact route/principal additions to the historical
// base, not a regenerated snapshot or a prefix exemption. Both extra reach and
// missing expected reach remain failures. The write route matches firewall/apply:
// this walk reaches the guarded handler, not the controller operation; the
// handler-level approval refusal is checked by TestFixedIPWriteMatchesFirewallApprovalBoundary.
var intendedRouteAdditions = map[string][]string{
	// 2026-11-20 (law 47; the coordinator gave the evaluation readout to the
	// metrics line): the evaluation numbers, read-only, for the console's readers.
	"GET /api/v1/readings/evaluation": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-18 (the coordinator chose it from the cockpit line's proposal):
	// the Owner's arrangement of the home's city; everyone reads, he alone saves.
	"GET /api/v1/home/layout": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"PUT /api/v1/home/layout": {"admin"},
	// 2026-11-18 (the Owner audits the laws, Skills and MCP tools on Ops):
	// read from the source at main, read-only, for the console's readers.
	"GET /api/v1/governance/instruments": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-17 (the Owner: "I ask for progress because Ops does not show
	// it"): every seat's latest status message, for everyone who reads the
	// runtime (read-only; a status is written with POST /messages).
	"GET /api/v1/agentrt/seats": {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	// 2026-11-17 (the Owner cannot see what the bus's work does): the
	// effect readings, read-only, for the Bus page's readers.
	"GET /api/v1/bus/effect": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-18 (OWNER_LAW #36): the problem loop's cases and counts, read-only, for the Bus page's readers.
	"GET /api/v1/bus/problems": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// The change of generation, read-only, for the same readers.
	"GET /api/v1/bus/nextgen":  {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/bus/contract": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// The team at work for the nerve map (2026-11-17), read-only.
	"GET /api/v1/bus/observe": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-17 (the coordinator: known regressions on main, information, no
	// gate): read by anyone who reads the bus; a seat records one, or its fix,
	// as itself (the writers the runtime's proposals have; the tenant's satellite too: a tenant may record a regression on its own main).
	"GET /api/v1/bus/regressions":        {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/bus/regressions":       {"reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/bus/regressions/fixed": {"reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-15 relay F: retained run/step timings, read-only like /units/runs.
	"GET /api/v1/delivery/pipelines/{name}/timings": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Wave 2b (2026-11-12): a trial of the release policy on one run request,
	// for the readers the release window's own GET has (read-only: it starts
	// nothing, creates no approval, writes no audit record).
	"GET /api/v1/delivery/pipelines/{name}/policy-check": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// CI landing wave 1 block D (2026-11-12): "is this commit green", read
	// from the evidence ledger, for the same readers (read-only).
	"GET /api/v1/evidence/commit": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Block F: the ledger beside the release policy's CI reading (read-only).
	// Block G: the ledger's CI status of one commit, read by the release
	// scripts and by in-cluster gates that hold no platform credential: open
	// like /releases (a repository, a commit, a run name; read-only).
	"GET /api/v1/evidence/ci": {"", "viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1", "executor"},
	// Head release (2026-11-13): "is my commit released" (read-only).
	"GET /api/v1/delivery/released": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Stage 2 block 2-B: CI's plan of one commit (read-only).
	"GET /api/v1/units/plan": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Stage 3: which modules of a commit run again, read by the in-cluster
	// task that decides it and holds no platform credential: open like
	// /evidence/ci (module names and input references of a declaration).
	"GET /api/v1/units/scope": {"", "viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1", "executor"},
	// Stage 3: how CI is set up for one project, for the configuration view (read-only).
	"GET /api/v1/units/config": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-14: the daily full CI's notes for the Ops run view (read-only).
	"GET /api/v1/units/full-ci": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Stage 3: what the reconciliation would name for two commits (read-only; it writes nothing).
	"GET /api/v1/units/full-ci/check": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Stage 3: the newest CI runs of one project, for the run view (read-only).
	"GET /api/v1/units/runs": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Owner 2026-11-16: the CI scorecard (how each repository uses CI, and CI
	// as their provider), for the Ops page, like /units/runs (read-only).
	"GET /api/v1/ci/scorecard": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// Owner 2026-11-16: the CI page's domain graph and what each commit
	// changed by domain, like /ci/scorecard (read-only; contracts v1).
	"GET /api/v1/ci/domains": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/ci/changes": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-20 (0.2 map line): the map's pool of what it cannot perceive,
	// as kept by the workers' scan (read-only; contract v1).
	"GET /api/v1/catalog/unperceived": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-20 (0.2 map line): who owns a path, and the map's table of
	// contents, from what the scan kept (read-only; contract v1).
	"GET /api/v1/catalog/owner": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/catalog/index": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-14: the approval records counted, for the Ops page (read-only).
	"GET /api/v1/readings/approvals": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/units/evidence":    {"operator", "admin", "trade-satellite"},
	// Owner 2026-11-15: the platform runs its own environment's migrations
	// (tier C: on the signed policy or an approved card; the guard decides).
	"POST /api/v1/agentrt/migrations/run": {"operator", "admin", "trade-satellite"},
	// 2026-11-15: the action record, read by seat, law and day (read-only).
	"GET /api/v1/records": {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	// 2026-11-15: a requester takes back its own card before it runs (narrows only).
	"POST /api/v1/approvals/{id}/withdraw": {"operator", "admin", "trade-satellite"},
	// TD-463: only the Owner may opt in to an assessment. The assess
	// ability requires RoleAdmin; readers, agents and intake clients cannot
	// request one. This records a request for advice, not an answer or approval.
	// C-2: the external host's exact claim, an agentd route; the store
	// refuses every host but the one the envelope names (claimpinned.go).
	// C-4: an authorization wait, for every reader, and the executor's
	// refresh of it, an agentd route the store holds to the pin's host.
	// C-5: a landing grant's material, for every reader; its revocation,
	// the Owner's alone (no route writes a grant).
	"GET /api/v1/agentrt/plan-view": {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	// R5:5.5-13 / 5.5-17 slice 1: a round's pace and its hours with no
	// registered work, for every reader, as plan-view is (read-only).
	"GET /api/v1/agentrt/plan-pace": {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	// R6:6.1-2 (AM-2 / AM-4): the delivery readings and their comparison
	// with a baseline, for every reader, as plan-pace is (read-only).
	// R5 phase 2 step 6: the plan's step timeline and the delivery stages,
	// read-only, for every reader, as plan-view is.
	"GET /api/v1/agentrt/delivery": {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	// R5:5.7-8 slice 1: the coordinator pins' share, read-only, every reader.
	// M-368 waves: the list for every reader; opening and closing behind
	// the same proposers' gate as POST missions (operator, admin and the
	// trade satellite reach it there too).
	// R5:5.7: the check value table, read-only, every reader.
	// The leak value projection, read-only, every reader, as /progress is.
	// S3 B3: what a landing card asked and what became of its answer, for
	// every reader (CUT §11.6 v3 item 8).
	// S3 B4: a mission's landing status, for every reader (CUT §11.8 v3).
	"GET /api/v1/agentrt/external-work/{id}":         {"viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1"},
	"POST /api/v1/agentrt/external-work":             {"reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/agentrt/external-work/{id}/events": {"reporter", "operator", "admin", "trade-satellite"},
	// A proposer withdraws its unstarted tasks: whoever may propose them
	// (POST /missions/{id}/tasks); the store holds it to their proposer.
	// 2026-11-16: bus step 1 (api/internal/bus/RULES.md §8): the gate's three
	// reads of the ledger, viewer and above (read-only). Gitea's push hook
	// holds no bearer: the body is signed with the hook's secret (bus/hook).
	"GET /api/v1/bus/events":                {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/bus/stream":                {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/bus/trace":                 {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"GET /api/v1/bus/clients":               {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/bus/hooks/gitea":          {"", "viewer", "reporter", "operator", "admin", "trade-satellite", "agentd:h1", "executor"},
	"GET /api/v1/network/clients/{mac}":     {"viewer", "reporter", "operator", "admin", "trade-satellite"},
	"POST /api/v1/network/clients/fixed-ip": {"operator", "admin", "trade-satellite"},
}

// Work 611 narrows plain reporters to external-work registration and their
// own observations. Keep the historical snapshot; remove only these exact
// reporter/proposal pairs, so either restored access or another lost route fails.
var reporterProposalRemovals = []string{
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
	// The intake routes were removed with business issues (the Owner
	// 2026-11-15); an intake token, where one is still configured, gets
	// through to nothing an anonymous caller does not.
	for _, r := range reach["intake:dots-p2"] {
		if !anon[r] {
			t.Errorf("an intake token gets through to %s, which an anonymous caller does not", r)
		}
	}

	// A collector (R5:5.5-12 §1.3) reaches its table and, of /api/v1,
	// nothing else, not even what an anonymous caller does (the gate is the
	// /api/v1 router's: /health and /metrics are before it); the old roles,
	// below, are as they were with it configured beside them.
	table := []string{"GET /health", "GET /metrics"}
	for route := range actuation.CollectorRoutes() {
		table = append(table, route)
	}
	slices.Sort(table)
	if !slices.Equal(reach["collector:h1"], table) {
		t.Errorf("a collector reaches %v, not its table %v", reach["collector:h1"], table)
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
			if !slices.Contains(then, r) {
				t.Errorf("%q reaches %s, which it did not on the base commit", p, r)
			}
		}
		for _, r := range then {
			if !slices.Contains(now, r) {
				t.Errorf("%q no longer reaches %s", p, r)
			}
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
	// its own. owner-exec, also another binary, reads the bearer of the page
	// that calls it only to pass it to this API, which is what authenticates
	// it. A handler on a public route gets the anonymous principal
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
	want := map[string]bool{"../actuation/auth.go": true, "../alertrelay/relay.go": true, "../ownerexec/server.go": true}
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
