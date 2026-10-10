package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// anonymousWrites lists the mutating routes allowed to skip auth.Require, each
// with the reason it is safe. Adding a route here is a review decision, not a
// way to turn the test green.
var anonymousWrites = map[string]string{}

// ticketAuthenticated are reads that open a privileged session and therefore
// authenticate in the handler instead of through auth.Require.
var ticketAuthenticated = map[string]string{}

// reachesHandlerAnonymously runs a route's middleware chain around a sentinel
// endpoint and sends it a request without a token. Matching middleware by
// function name breaks under inlining; asking the chain itself does not.
func reachesHandlerAnonymously(method string, mws []func(http.Handler) http.Handler) bool {
	reached := false
	var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/", nil))
	return reached
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// Every route that changes state must carry auth.Require. A spot check once
// let /cluster/sync-kubeconfig ship anonymous; this walk catches the next one.
func TestEveryMutatingRouteRequiresARole(t *testing.T) {
	srv, err := New(newTestConfig(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	routes, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("Router() is not a chi.Routes")
	}

	var walked, gated int
	seenTicket := map[string]bool{}
	err = chi.Walk(routes, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		key := method + " " + strings.TrimSuffix(route, "/")
		if route == "/" {
			key = method + " /"
		}
		if _, ok := ticketAuthenticated[key]; ok {
			seenTicket[key] = true
			return nil
		}
		if !isMutating(method) {
			return nil
		}
		walked++
		if !reachesHandlerAnonymously(method, mws) {
			gated++
			return nil
		}
		if reason, ok := anonymousWrites[key]; ok && reason != "" {
			return nil
		}
		t.Errorf("%s answers a request without a token: put it in an auth.Require group, or list it in anonymousWrites with a reason", key)
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	// Floor is a broken-walk detector. LANE-W32 deleted several mutating routes; 40 still fails a walk that misses the router.
	if walked < 40 {
		t.Fatalf("walked only %d mutating routes — the walk is not seeing the router", walked)
	}
	for key := range ticketAuthenticated {
		if !seenTicket[key] {
			t.Errorf("ticketAuthenticated lists %s, which is no longer routed", key)
		}
	}
	t.Logf("%d mutating routes, %d behind auth.Require", walked, gated)
}

func TestHealthReportsAuthNotLoaded(t *testing.T) {
	srv, err := New(newTestConfig(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if !strings.Contains(rec.Body.String(), `"auth_loaded":false`) {
		t.Fatalf("/health with a missing platform-auth.yaml must say auth_loaded=false; body = %s", rec.Body.String())
	}
}
