package versionedread

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Origin: 2026-11-18, read routes R03 and R05 get a version: Console polls them
// and took the whole answer each time. A changed answer must never be called
// unchanged, and an error must not get a version.
func TestAnAnswerIsUnchangedOnlyWhenItIs(t *testing.T) {
	body, code := "one", http.StatusOK
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	get := func(etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	first := get("")
	etag := first.Header().Get("ETag")
	if first.Code != 200 || first.Body.String() != "one" || etag == "" || first.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("first read: %d %q etag %q", first.Code, first.Body.String(), etag)
	}
	if again := get(etag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("same answer: %d %q", again.Code, again.Body.String())
	}
	body = "two"
	if changed := get(etag); changed.Code != 200 || changed.Body.String() != "two" || changed.Header().Get("ETag") == etag {
		t.Fatalf("changed answer: %d %q", changed.Code, changed.Body.String())
	}
	code = http.StatusBadGateway
	if failed := get(""); failed.Code != http.StatusBadGateway || failed.Header().Get("ETag") != "" {
		t.Fatalf("an error got a version: %d %q", failed.Code, failed.Header().Get("ETag"))
	}
}
