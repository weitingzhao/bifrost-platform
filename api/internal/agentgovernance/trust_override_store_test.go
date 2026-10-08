package agentgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type brokenOverrides struct{ readErr error }

func (b brokenOverrides) List(context.Context) (map[string]TrustOverride, error) {
	if b.readErr != nil {
		return nil, b.readErr
	}
	return map[string]TrustOverride{}, nil
}
func (b brokenOverrides) Location() string { return "test" }

func repoTrustOverrides(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", "config", "trust-overrides.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("trust overrides file: %v", err)
	}
	return p
}

func TestYAMLTrustOverrideAppliesResearchLoopBatch(t *testing.T) {
	h := newTestHandler(t)
	path := repoTrustOverrides(t)
	h.UseTrustOverrideStore(NewYAMLTrustOverrideStore(path))
	rec := httptest.NewRecorder()
	h.HandleTrustMatrix(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("trust-matrix = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp TrustMatrixResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StoreError != "" {
		t.Fatalf("store_error = %q", resp.StoreError)
	}
	if resp.OverrideStore != path {
		t.Fatalf("override_store = %q, want %s", resp.OverrideStore, path)
	}
	var entry *TrustMatrixEntry
	for i := range resp.Entries {
		if resp.Entries[i].SkillID == "research-loop-batch" {
			entry = &resp.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("research-loop-batch missing from trust matrix")
	}
	if entry.CurrentLevel != "L0" {
		t.Fatalf("research-loop-batch level = %s, want L0", entry.CurrentLevel)
	}
	if entry.LastOverrideBy != "owner" {
		t.Fatalf("last_override_by = %q", entry.LastOverrideBy)
	}
}

func TestTrustOverrideMissingOrBadFileIs200(t *testing.T) {
	h := newTestHandler(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "trust-overrides.yaml")
	h.UseTrustOverrideStore(NewYAMLTrustOverrideStore(missing))
	assertStoreError200(t, h)

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("overrides: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewYAMLTrustOverrideStore(bad).List(context.Background()); err == nil {
		t.Fatal("List on a truncated file returned no error")
	}
	h.UseTrustOverrideStore(NewYAMLTrustOverrideStore(bad))
	assertStoreError200(t, h)

	h.UseTrustOverrideStore(brokenOverrides{readErr: errors.New("permission denied")})
	assertStoreError200(t, h)
	rec := httptest.NewRecorder()
	h.HandleSnapshot(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot with an unreadable store = %d, want 200", rec.Code)
	}
}

func assertStoreError200(t *testing.T, h *Handler) {
	t.Helper()
	for name, handle := range map[string]http.HandlerFunc{
		"trust-overrides": h.HandleTrustOverrides,
		"trust-matrix":    h.HandleTrustMatrix,
	} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200; body %s", name, rec.Code, rec.Body.String())
			continue
		}
		var body struct {
			StoreError string `json:"store_error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s decode: %v", name, err)
			continue
		}
		if body.StoreError == "" {
			t.Errorf("%s store_error is empty; body %s", name, rec.Body.String())
		}
	}
}
