package agentgovernance

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type brokenOverrides struct{ readErr, writeErr error }

func (b brokenOverrides) List(context.Context) (map[string]TrustOverride, error) {
	if b.readErr != nil {
		return nil, b.readErr
	}
	return map[string]TrustOverride{}, nil
}
func (b brokenOverrides) Put(context.Context, TrustOverride) error { return b.writeErr }
func (b brokenOverrides) Location() string                         { return "test" }

func putOverride(h *Handler, skillID, body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Put("/trust-overrides/{skill_id}", h.HandlePutTrustOverride)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/trust-overrides/"+skillID, bytes.NewBufferString(body)))
	return rec
}

func TestTrustOverridePutWriteFailIsServerError(t *testing.T) {
	h := newTestHandler(t)
	h.UseTrustOverrideStore(brokenOverrides{writeErr: errors.New("disk full")})
	rec := putOverride(h, "research-loop-batch", `{"level":"L0","reason":"test"}`)
	if rec.Code < 500 {
		t.Fatalf("PUT with a failing store = %d, want 5xx; body = %s", rec.Code, rec.Body.String())
	}
}

func TestTrustOverrideListReadFailIsServiceUnavailable(t *testing.T) {
	h := newTestHandler(t)
	h.UseTrustOverrideStore(brokenOverrides{readErr: errors.New("permission denied")})
	for name, handle := range map[string]http.HandlerFunc{
		"trust-overrides": h.HandleTrustOverrides,
		"trust-matrix":    h.HandleTrustMatrix,
		"snapshot":        h.HandleSnapshot,
	} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with an unreadable store = %d, want 503", name, rec.Code)
		}
	}
	if rec := putOverride(h, "research-loop-batch", `{"level":"L0"}`); rec.Code < 500 {
		t.Errorf("PUT with an unreadable store = %d, want 5xx", rec.Code)
	}
}

func TestTrustOverrideFileReadFailSurfaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_GOVERNANCE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "trust_overrides.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewTrustOverrideStore()
	if _, err := s.List(context.Background()); err == nil {
		t.Fatal("List on a corrupt file returned no error")
	}
	if err := s.Put(context.Background(), TrustOverride{SkillID: "x", Level: "L0"}); err == nil {
		t.Fatal("Put over a corrupt file returned no error (it would have dropped every other grant)")
	}
}

func TestTrustOverrideFileStoreNeverUsesHome(t *testing.T) {
	t.Setenv("PLATFORM_GOVERNANCE_DIR", "")
	t.Setenv("PLATFORM_PROJECT_ROOT", "")
	t.Setenv("PLATFORM_DATA_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if loc := NewTrustOverrideStore().Location(); strings.Contains(loc, home) {
		t.Fatalf("file store resolved under $HOME: %s", loc)
	}
}
