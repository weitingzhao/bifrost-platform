package flexquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestParseFreshnessJSONKeepsSQLStatusLiteral(t *testing.T) {
	now := time.Date(2024, 6, 20, 18, 0, 0, 0, time.UTC)
	body := []byte(`{"dimensions":[
		{"dimension":"flex-trades","latest_ts":"2024-06-20T10:30:24.455733Z","row_count":31,"updated_at":"2024-06-20T10:30:24.455733Z"},
		{"dimension":"flex-transactions","latest_ts":null,"row_count":null}
	]}`)
	rows, err := parseFreshnessJSON(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].Dimension != "flex-trades" || rows[0].RowsWritten != 31 || rows[0].Status != "ok" || rows[0].Verdict != "ok" {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[0].LastRunAt != "2024-06-20T10:30:24Z" {
		t.Fatalf("last_run_at=%q", rows[0].LastRunAt)
	}
	if rows[1].Status != "ok" || rows[1].Verdict != "unknown" || rows[1].RowsWritten != 0 {
		t.Fatalf("null latest_ts stays unknown with status ok, got %+v", rows[1])
	}
}

func TestProbeFreshnessHTTP(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		recent := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
		_, _ = w.Write([]byte(`{"dimensions":[{"dimension":"flex-trades","latest_ts":"` + recent + `","row_count":3}]}`))
	}))
	defer srv.Close()

	svc := &Service{cfg: Config{APIBaseURL: srv.URL}}
	rows, reach, errMsg := svc.probeFreshness(context.Background())
	if gotPath != "/flex/coverage/freshness" || gotQuery != "" {
		t.Fatalf("path=%q query=%q", gotPath, gotQuery)
	}
	if reach != probe.ReachOK || errMsg != "" || len(rows) != 1 || rows[0].Status != "ok" {
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
	if reach != probe.ReachFail || len(rows) != 0 || !strings.Contains(errMsg, "502") {
		t.Fatalf("reach=%s rows=%d err=%q", reach, len(rows), errMsg)
	}
}

func TestProbeFreshnessEmptyJSONIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"dimensions":[]}`))
	}))
	defer srv.Close()

	svc := &Service{cfg: Config{APIBaseURL: srv.URL}}
	_, reach, errMsg := svc.probeFreshness(context.Background())
	if reach != probe.ReachUnknown || errMsg != "no ingest_freshness rows" {
		t.Fatalf("reach=%s err=%q", reach, errMsg)
	}
}
