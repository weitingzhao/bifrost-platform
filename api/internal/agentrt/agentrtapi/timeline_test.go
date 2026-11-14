package agentrtapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Without a database GET /delivery refuses the anonymous caller as such
// and answers a reader 503.
func TestDeliveryRouteNotConfigured(t *testing.T) {
	s := server(t, nil)
	if code, _ := call(t, s, "", "GET", "delivery?commit="+pushedAt, nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous %d", code)
	}
	if code, out := call(t, s, "viewer", "GET", "delivery?commit="+pushedAt, nil); code != http.StatusServiceUnavailable || out["error"] != "not_configured" {
		t.Fatalf("no database %d %v", code, out)
	}
}

// GET /delivery names one commit. The route once also read by task; with the
// dispatch lane retired (2026-11-13) a task is refused with a reason, so the
// Console and MCP callers that still send one are told why, not given a 404.
// The 200 answer is store.TestReadExternalDelivery's.
func TestDeliveryRoute(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	for _, q := range []string{"", "?task=" + task, "?commit=" + pushedAt + "&task=" + task} {
		if code, out := call(t, s, "viewer", "GET", "delivery"+q, nil); code != http.StatusBadRequest || out["error"] != "bad_request" {
			t.Fatalf("delivery%s: %d %v", q, code, out)
		}
	}
	if code, out := call(t, s, "viewer", "GET", "delivery?commit=XYZ", nil); code != http.StatusUnprocessableEntity || out["error"] != "invalid" {
		t.Fatalf("a bad commit %d %v", code, out)
	}
	if code, _ := call(t, s, "viewer", "GET", "delivery?commit=fedcba9876543", nil); code != http.StatusNotFound {
		t.Fatalf("an unknown commit %d", code)
	}
	if code, _ := call(t, s, "", "GET", "delivery?commit="+pushedAt, nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous %d", code)
	}
}

func TestDeliveryRouteCurrentEvidenceIsServerSupplied(t *testing.T) {
	pool, _ := world(t)
	called := false
	s := server(t, pool, func(a *agentrtapi.API) {
		a.ProductionDelivery = func(context.Context) store.DeliveryProduction {
			called = true
			return store.DeliveryProduction{Reason: "unsettled"}
		}
	})
	code, _ := call(t, s, "viewer", "GET", "delivery?commit=fedcba9876543&production=done", nil)
	if code != http.StatusNotFound || !called {
		t.Fatalf("trusted reader not called: status=%d called=%v", code, called)
	}
	called = false
	code, _ = call(t, s, "", "GET", "delivery?commit=fedcba9876543", nil)
	if code != http.StatusUnauthorized || called {
		t.Fatal("anonymous request reached release reader")
	}
}
