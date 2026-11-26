package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Every route the platform serves is some domain's registered contract
// (config/domain-ownership.json, domains[].contracts.request_response): a
// route is registered before it exists, not found by a scan afterwards.
//
// Origin: 2026-11-22, ISSUE-69: like Kubernetes, a thing is registered
// before the next step is taken, or many lose track; LAW-10 (a change says which domain it belongs to). That day
// 87 of 235 routes had no contract row, and three real losses were counted
// (a package nobody placed defaulting to foundation, four compute interfaces
// unregistered, payload's charter path left behind after a rename). The 87
// are held per domain of their handler's package in
// routes_unregistered_<domain>_test.go and only shrink.

// unregisteredHeld is the baseline, filled from the per-domain files.
var unregisteredHeld = map[string]bool{}

func heldUnregistered(routes ...string) bool {
	for _, r := range routes {
		unregisteredHeld[r] = true
	}
	return true
}

// routeKey is how a route and a contract row are compared.
func routeKey(method, path string) string {
	return method + " " + strings.TrimSuffix(strings.ReplaceAll(path, "/*", ""), "/")
}

// unregisteredRoutes names, sorted, the routes the router serves that no
// contract row registers.
func unregisteredRoutes(t *testing.T, routes chi.Routes) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Domains []struct {
			Contracts struct {
				RequestResponse []struct{ Method, Path string } `json:"request_response"`
			}
		}
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	reg := map[string]bool{}
	for _, d := range doc.Domains {
		for _, r := range d.Contracts.RequestResponse {
			reg[routeKey(r.Method, r.Path)] = true
		}
	}
	var missing []string
	err = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if k := routeKey(method, route); !reg[k] {
			missing = append(missing, k)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(missing)
	return missing
}

// routeErrors compares the routes without a contract with the baseline.
func routeErrors(missing []string, held map[string]bool) []string {
	var out []string
	now := map[string]bool{}
	for _, k := range missing {
		now[k] = true
		if !held[k] {
			out = append(out, k+" has no contract row: register it under its domain's contracts.request_response in config/domain-ownership.json before it is served (laws 72, 75; the map line owns the table)")
		}
	}
	for k := range held {
		if !now[k] {
			out = append(out, k+" is registered or gone: take it off routes_unregistered_<domain>_test.go (the list only shrinks)")
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryRouteIsRegistered(t *testing.T) {
	routes, ok := reachServer(t).Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() is not a chi.Routes")
	}
	missing := unregisteredRoutes(t, routes)
	t.Logf("routes without a contract row: %d (held %d)", len(missing), len(unregisteredHeld))
	for _, msg := range routeErrors(missing, unregisteredHeld) {
		t.Error(msg)
	}
}

// The control: a new route without a row is named, a held one that got its
// row is named, a held one still missing is quiet.
func TestRouteErrorsNameNewAndRegistered(t *testing.T) {
	got := routeErrors([]string{"GET /a", "GET /new"}, map[string]bool{"GET /a": true, "GET /done": true})
	if len(got) != 2 || !strings.HasPrefix(got[0], "GET /done is registered or gone") || !strings.HasPrefix(got[1], "GET /new has no contract row") {
		t.Fatalf("routeErrors = %q", got)
	}
}
