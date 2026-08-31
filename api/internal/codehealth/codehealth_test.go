package codehealth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("PLATFORM_CODE_HEALTH_DIR", t.TempDir())
	return NewStore()
}

func sampleReport(commit string, value int) Report {
	return Report{
		GeneratedAt: time.Now().UTC(),
		Commit:      commit,
		Metrics: []Metric{{
			ID: "code.duplication.satellite", Label: "duplicated function names",
			Domain: "satellite", Repo: "bifrost-trade-frontend",
			Value: value, Baseline: 12, Status: "at_baseline",
		}},
		ReceivedAt: time.Now().UTC(),
	}
}

// The contract this whole package exists for: an empty store must report
// "never measured", never an empty-but-successful reading. A caller that
// cannot tell those apart will render unobserved code as healthy code.
func TestLatestDistinguishesNeverReportedFromClean(t *testing.T) {
	store := newTestStore(t)

	if _, ok := store.Latest(); ok {
		t.Fatal("Latest() on empty store = true, want false")
	}

	if err := store.Put(sampleReport("abc1234", 12)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, ok := store.Latest()
	if !ok {
		t.Fatal("Latest() after Put = false, want true")
	}
	if got.Commit != "abc1234" || len(got.Metrics) != 1 {
		t.Fatalf("Latest() = %+v", got)
	}
}

func TestGetReportsNotObservedWhenNothingStored(t *testing.T) {
	t.Setenv("PLATFORM_CODE_HEALTH_DIR", t.TempDir())
	h := NewHandler(nil)

	rec := httptest.NewRecorder()
	h.HandleGet(rec, httptest.NewRequest(http.MethodGet, "/code-health", nil))

	var resp StatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Reported {
		t.Fatal("Reported = true with nothing stored — this is the fake-green path")
	}
	if resp.Latest != nil {
		t.Fatalf("Latest = %+v, want nil", resp.Latest)
	}
	if resp.Note == "" {
		t.Fatal("Note is empty — an unobserved metric must say why")
	}
}

// A report carrying no metric would replace a real reading with an empty one
// and read as "all clear". scan.sh refuses to emit that; the endpoint must
// refuse to accept it.
func TestReportRejectsEmptyAndUnanchoredPayloads(t *testing.T) {
	cases := []struct {
		name string
		body Report
	}{
		{"no metrics", Report{Commit: "abc1234"}},
		{"no commit", Report{Metrics: sampleReport("x", 12).Metrics}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PLATFORM_CODE_HEALTH_DIR", t.TempDir())
			h := NewHandler(nil)

			raw, _ := json.Marshal(tc.body)
			rec := httptest.NewRecorder()
			h.HandleReport(rec, httptest.NewRequest(http.MethodPost, "/code-health/report", bytes.NewReader(raw)))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if _, ok := h.store.Latest(); ok {
				t.Fatal("a rejected report was stored anyway")
			}
		})
	}
}

func TestReportRoundTripAndHistoryOrder(t *testing.T) {
	t.Setenv("PLATFORM_CODE_HEALTH_DIR", t.TempDir())
	h := NewHandler(nil)

	for _, c := range []string{"commit-1", "commit-2", "commit-3"} {
		raw, _ := json.Marshal(sampleReport(c, 12))
		rec := httptest.NewRecorder()
		h.HandleReport(rec, httptest.NewRequest(http.MethodPost, "/code-health/report", bytes.NewReader(raw)))
		if rec.Code != http.StatusOK {
			t.Fatalf("report %s: status = %d, body = %s", c, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	h.HandleGet(rec, httptest.NewRequest(http.MethodGet, "/code-health", nil))
	var resp StatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Reported || resp.Latest == nil {
		t.Fatal("Reported/Latest missing after three reports")
	}
	if resp.Latest.Commit != "commit-3" {
		t.Fatalf("Latest.Commit = %q, want commit-3 (newest first)", resp.Latest.Commit)
	}
	if len(resp.History) != 3 {
		t.Fatalf("len(History) = %d, want 3", len(resp.History))
	}
}

func TestStorePrunesBeyondHistoryLimit(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < historyLimit+5; i++ {
		if err := store.Put(sampleReport("commit", 12)); err != nil {
			t.Fatalf("Put(%d) error = %v", i, err)
		}
	}
	if got := len(store.List(0)); got != historyLimit {
		t.Fatalf("len(List) = %d, want %d", got, historyLimit)
	}
}
