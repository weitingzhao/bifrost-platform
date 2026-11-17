package agentgovernance

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

)

// research-loop-batch sat at L1 with 0 consecutive successes across 694 recorded
// jobs — not because it kept failing, but because nothing ever recorded a run
// for it. Three successes are needed; the counter could not move at all.
// The outcome store writes to disk. Each test gets its own directory.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", t.TempDir())
	t.Setenv("PLATFORM_GOVERNANCE_DIR", t.TempDir())
	return NewHandler()
}

func post(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/skill-runs", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.HandleRecordSkillRun(rec, req)
	return rec
}

func TestRecordedRunsEarnTrust(t *testing.T) {
	h := newTestHandler(t)
	for i := 0; i < PromotionThreshold; i++ {
		if rec := post(t, h, `{"scope":"research.loop.batch","status":"done"}`); rec.Code != 200 {
			t.Fatalf("record %d → %d", i, rec.Code)
		}
	}
	entry := findEntry(t, h, "research-loop-batch")
	if entry.ConsecutiveSuccesses != PromotionThreshold {
		t.Fatalf("successes = %d, want %d", entry.ConsecutiveSuccesses, PromotionThreshold)
	}
	if !entry.PromotionEligible {
		t.Fatal("a skill that succeeded the threshold number of times is still not eligible")
	}
}

func TestAFailureBreaksTheStreak(t *testing.T) {
	h := newTestHandler(t)
	post(t, h, `{"scope":"research.loop.batch","status":"done"}`)
	post(t, h, `{"scope":"research.loop.batch","status":"failed","error":"boom"}`)
	if e := findEntry(t, h, "research-loop-batch"); e.ConsecutiveSuccesses != 0 {
		t.Fatalf("a failure left %d successes standing", e.ConsecutiveSuccesses)
	}
}

func TestTheCatalogIdIsAcceptedAndCounts(t *testing.T) {
	// The matrix shows `research-loop-batch`; the catalog files it under
	// `research.loop.batch`, and grouping is by scope. Accepting the id but
	// storing it verbatim wrote a record that was never read — a 200 that
	// changed nothing, which is the exact disconnection this endpoint exists to
	// end. Asserting only the status code is what let that through.
	h := newTestHandler(t)
	if rec := post(t, h, `{"scope":"research-loop-batch","status":"done"}`); rec.Code != 200 {
		t.Fatalf("catalog id rejected: %d", rec.Code)
	}
	if e := findEntry(t, h, "research-loop-batch"); e.ConsecutiveSuccesses != 1 {
		t.Fatalf("recorded under the id did not count: successes = %d", e.ConsecutiveSuccesses)
	}
}

func TestBothSpellingsLandInTheSameStreak(t *testing.T) {
	h := newTestHandler(t)
	post(t, h, `{"scope":"research-loop-batch","status":"done"}`)
	post(t, h, `{"scope":"research.loop.batch","status":"done"}`)
	if e := findEntry(t, h, "research-loop-batch"); e.ConsecutiveSuccesses != 2 {
		t.Fatalf("id and scope split the streak: successes = %d", e.ConsecutiveSuccesses)
	}
}

func TestAnUnknownScopeIsRefused(t *testing.T) {
	// It would be written and then never read — trust only groups catalogued
	// scopes, so accepting it would look like it worked and change nothing.
	h := newTestHandler(t)
	if rec := post(t, h, `{"scope":"not-a-skill","status":"done"}`); rec.Code != 404 {
		t.Fatalf("unknown scope → %d, want 404", rec.Code)
	}
}

func TestStatusMustBeAnOutcome(t *testing.T) {
	h := newTestHandler(t)
	for _, body := range []string{
		`{"scope":"research.loop.batch","status":"running"}`,
		`{"scope":"research.loop.batch"}`,
	} {
		if rec := post(t, h, body); rec.Code != 400 {
			t.Fatalf("%s → %d, want 400", body, rec.Code)
		}
	}
}

func findEntry(t *testing.T, h *Handler, skillID string) TrustMatrixEntry {
	t.Helper()
	for _, e := range computeTrustMatrixRaw(h.store.List()).Entries {
		if e.SkillID == skillID {
			return e
		}
	}
	t.Fatalf("%s not in trust matrix", skillID)
	return TrustMatrixEntry{}
}

// Origin: foundation code review item 12 (2026-11-15). A skill outcome that
// could not be written to disk was told to the caller as recorded. It must
// answer 500 instead, and write nothing.
func TestAnOutcomeThatCannotBeStoredIsNotAnswered200(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The store's directory sits under a plain file, so it can be neither made nor written.
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", filepath.Join(blocker, "jobs"))
	store := NewOutcomeStore()
	err := store.Put(Outcome{ID: "x", Status: OutcomeDone})
	if err == nil {
		t.Fatal("Put reported success for a directory it could not use")
	}
	// Positive control: a usable directory stores it.
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", filepath.Join(dir, "jobs"))
	if err = NewOutcomeStore().Put(Outcome{ID: "y", Status: OutcomeDone}); err != nil {
		t.Fatalf("Put into a usable directory: %v", err)
	}
}
