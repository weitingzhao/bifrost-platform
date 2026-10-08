package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestParseFreshnessJSONMatchesPipeRules(t *testing.T) {
	now := time.Date(2024, 6, 20, 18, 0, 0, 0, time.UTC) // Thursday
	body := []byte(`{"freshness":[
		{"dimension":"stock_daily","last_run_at":"2024-06-20T17:00:00+00:00","rows_written":42,"status":"ok","updated_at":"2024-06-20T17:00:00+00:00"},
		{"dimension":"option_snapshot","last_run_at":"2024-06-18T17:00:00+00:00","rows_written":5,"status":"ok"},
		{"dimension":"ratios","last_run_at":null,"rows_written":null,"status":null}
	]}`)
	rows, err := parseFreshnessJSON(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].Dimension != "stock_daily" || rows[0].Verdict != "ok" || rows[0].RowsWritten != 42 || rows[0].Status != "ok" {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[1].Verdict != "stale" {
		t.Fatalf("expected stale snapshot, got %+v", rows[1])
	}
	if rows[2].Verdict != "unknown" || rows[2].RowsWritten != 0 || rows[2].Status != "unknown" || rows[2].LastRunAt != "" {
		t.Fatalf("null columns should match COALESCE defaults, got %+v", rows[2])
	}
}

// The probe reads the freshness-only endpoint. db-summary carries the same
// array but also counts the whole database (~4.6 s, TD-259), and the Console
// polls plugin status every 30 s.
func TestProbeFreshnessHTTP(t *testing.T) {
	var paths []string
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/market/coverage/freshness" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		recent := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
		_, _ = w.Write([]byte(`{"ok":true,"source":"db","freshness":[{"dimension":"stock_daily","last_run_at":"` + recent + `","rows_written":42,"status":"ok"}]}`))
	}))
	defer srv.Close()

	svc := &Service{cfg: Config{APIBaseURL: srv.URL}}
	rows, reach, errMsg := svc.probeFreshness(context.Background())
	if len(paths) != 1 || paths[0] != "/market/coverage/freshness" || gotQuery != "" {
		t.Fatalf("paths=%q query=%q", paths, gotQuery)
	}
	if reach != probe.ReachOK || errMsg != "" || len(rows) != 1 || rows[0].Verdict != "ok" {
		t.Fatalf("reach=%s err=%q rows=%+v", reach, errMsg, rows)
	}
}

func TestProbeFreshnessUnreachableIsNotEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer srv.Close()

	svc := &Service{cfg: Config{APIBaseURL: srv.URL}}
	rows, reach, errMsg := svc.probeFreshness(context.Background())
	if reach != probe.ReachFail {
		t.Fatalf("reach=%s want fail", reach)
	}
	if len(rows) != 0 || !strings.Contains(errMsg, "502") {
		t.Fatalf("rows=%+v err=%q", rows, errMsg)
	}
}

func TestProbeFreshnessEmptyJSONIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"freshness":[]}`))
	}))
	defer srv.Close()

	svc := &Service{cfg: Config{APIBaseURL: srv.URL}}
	rows, reach, errMsg := svc.probeFreshness(context.Background())
	if reach != probe.ReachUnknown || errMsg != "no ingest_freshness rows" || len(rows) != 0 {
		t.Fatalf("reach=%s err=%q rows=%d", reach, errMsg, len(rows))
	}
}

func TestProbeFreshnessBadJSONIsDegraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	svc := &Service{cfg: Config{APIBaseURL: srv.URL}}
	_, reach, errMsg := svc.probeFreshness(context.Background())
	if reach != probe.ReachDegraded || errMsg != "freshness JSON unparseable" {
		t.Fatalf("reach=%s err=%q", reach, errMsg)
	}
}
