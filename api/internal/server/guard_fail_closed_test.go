package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("broken body") }
func (failingBody) Close() error             { return nil }

// 2026-11-16, found by the decoupling line while checking protected rows: the
// guard handed a request on unjudged when its body could not be read, its id
// was not in the catalog, or a required param was missing, and the shared IB
// control route handed on any action it did not know. Each of these is now
// refused before the handler runs.
func TestGuardFailsClosed(t *testing.T) {
	hits := 0
	next := func(w http.ResponseWriter, _ *http.Request) { hits++; w.WriteHeader(http.StatusOK) }
	s := &Server{}

	cases := []struct {
		name string
		h    http.HandlerFunc
		req  func() *http.Request
		code int
	}{
		{"missing required param on a tier C action", s.guardUntraced("ensure_bifrost_namespaces", next),
			func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/api/v1/cluster/namespaces/ensure-bifrost", strings.NewReader(`{}`))
			}, http.StatusBadRequest},
		{"unreadable body", s.guardUntraced("ensure_bifrost_namespaces", next),
			func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/namespaces/ensure-bifrost", nil)
				r.Body = failingBody{}
				return r
			}, http.StatusBadRequest},
		{"id not in the catalog", s.guardUntraced("no_such_action", next),
			func() *http.Request { return httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`)) },
			http.StatusInternalServerError},
		{"unknown IB control action", s.guardIB(next),
			func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/ib-gateway/control/place-order", nil)
				rc := chi.NewRouteContext()
				rc.URLParams.Add("action", "place-order")
				return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			}, http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		c.h(rec, c.req())
		if rec.Code != c.code || hits != 0 {
			t.Errorf("%s: code=%d hits=%d %s, want %d and no handler call", c.name, rec.Code, hits, rec.Body.String(), c.code)
		}
	}

	// Positive control: a named, catalogued tier C call still stops at the
	// approval, which shows the 400s above come from the new checks.
	rec := httptest.NewRecorder()
	s.guardUntraced("ensure_bifrost_namespaces", next)(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cluster/namespaces/ensure-bifrost", strings.NewReader(`{"names":["bifrost-bus"]}`)))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "approval required") || hits != 0 {
		t.Errorf("positive control: code=%d hits=%d %s", rec.Code, hits, rec.Body.String())
	}
}
