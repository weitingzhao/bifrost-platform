package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestProbeHealthOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "ok",
			"pool":          "stocks",
			"jobs_done":     12,
			"jobs_failed":   1,
			"uptime_sec":    42.5,
			"last_claim_at": time.Now().UTC().Format(time.RFC3339),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc := &Service{
		cfg:    Config{StocksHealthURL: srv.URL + "/health"},
		client: srv.Client(),
	}
	worker, reach, errMsg := svc.probeHealth(context.Background(), svc.cfg.StocksHealthURL)
	if reach != probe.ReachOK {
		t.Fatalf("reach=%s err=%s", reach, errMsg)
	}
	if worker == nil || worker.Pool != "stocks" || worker.JobsDone != 12 {
		t.Fatalf("unexpected worker: %+v", worker)
	}
}

func TestProbeHealthFail(t *testing.T) {
	svc := &Service{
		cfg:    Config{StocksHealthURL: "http://127.0.0.1:1/health"},
		client: &http.Client{Timeout: 200 * time.Millisecond},
	}
	_, reach, errMsg := svc.probeHealth(context.Background(), svc.cfg.StocksHealthURL)
	if reach != probe.ReachFail {
		t.Fatalf("expected fail, got %s (%s)", reach, errMsg)
	}
}

func TestStatusProbesBothPools(t *testing.T) {
	stocksMux := http.NewServeMux()
	stocksMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "pool": "stocks", "jobs_done": 10, "jobs_failed": 0, "uptime_sec": 1,
		})
	})
	stocksSrv := httptest.NewServer(stocksMux)
	defer stocksSrv.Close()

	optionsMux := http.NewServeMux()
	optionsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "pool": "options", "jobs_done": 7, "jobs_failed": 1, "uptime_sec": 2,
		})
	})
	optionsSrv := httptest.NewServer(optionsMux)
	defer optionsSrv.Close()

	svc := &Service{
		cfg: Config{
			StocksHealthURL:  stocksSrv.URL + "/health",
			OptionsHealthURL: optionsSrv.URL + "/health",
		},
		client: http.DefaultClient,
		freshnessProbe: func(ctx context.Context) ([]FreshnessInfo, probe.Reachability, string) {
			return []FreshnessInfo{{
				Dimension: "stock_daily", Status: "ok", AgeHours: 1, Verdict: "ok", RowsWritten: 10,
			}}, probe.ReachOK, ""
		},
	}
	resp := svc.Status(context.Background())
	if len(resp.Workers) != 2 {
		t.Fatalf("workers=%d want 2: %+v", len(resp.Workers), resp.Workers)
	}
	pools := map[string]bool{}
	for _, w := range resp.Workers {
		pools[w.Pool] = true
	}
	if !pools["stocks"] || !pools["options"] {
		t.Fatalf("expected both pools, got %+v", resp.Workers)
	}
	if resp.HealthReach != probe.ReachOK {
		t.Fatalf("health_reach=%s", resp.HealthReach)
	}
	if resp.FreshnessReach != probe.ReachOK || len(resp.Freshness) != 1 {
		t.Fatalf("freshness_reach=%s rows=%+v", resp.FreshnessReach, resp.Freshness)
	}
}

func TestParseFreshnessOutput(t *testing.T) {
	now := time.Date(2024, 6, 20, 18, 0, 0, 0, time.UTC)
	out := "stock_daily|2024-06-20 17:00:00+00|42|ok\noption_snapshot|2024-06-18 17:00:00+00|5|ok\n"
	rows := parseFreshnessOutput(out, now)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].Dimension != "stock_daily" || rows[0].Verdict != "ok" || rows[0].RowsWritten != 42 {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[1].Verdict != "stale" {
		t.Fatalf("expected stale for old snapshot, got %+v", rows[1])
	}
}

func TestStatusFreshnessUnknownDoesNotDegrade(t *testing.T) {
	stocksMux := http.NewServeMux()
	stocksMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "pool": "stocks", "jobs_done": 1, "jobs_failed": 0, "uptime_sec": 1,
		})
	})
	stocksSrv := httptest.NewServer(stocksMux)
	defer stocksSrv.Close()

	optionsMux := http.NewServeMux()
	optionsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "pool": "options", "jobs_done": 1, "jobs_failed": 0, "uptime_sec": 1,
		})
	})
	optionsSrv := httptest.NewServer(optionsMux)
	defer optionsSrv.Close()

	svc := &Service{
		cfg: Config{
			StocksHealthURL:  stocksSrv.URL + "/health",
			OptionsHealthURL: optionsSrv.URL + "/health",
		},
		client: http.DefaultClient,
		deploymentsOverride: []DeploymentInfo{
			{Namespace: pluginNamespace, Name: stocksDeployName, Ready: "1/1", Reach: probe.ReachOK},
			{Namespace: pluginNamespace, Name: optionsDeployName, Ready: "1/1", Reach: probe.ReachOK},
		},
		freshnessProbe: func(ctx context.Context) ([]FreshnessInfo, probe.Reachability, string) {
			return nil, probe.ReachUnknown, "no ingest_freshness rows"
		},
	}
	resp := svc.Status(context.Background())
	if resp.FreshnessReach != probe.ReachUnknown {
		t.Fatalf("freshness_reach=%s want unknown", resp.FreshnessReach)
	}
	if resp.HealthReach != probe.ReachOK {
		t.Fatalf("health_reach=%s", resp.HealthReach)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("overall reachability=%s want ok (freshness unknown must not degrade)", resp.Reachability)
	}
	if resp.Error != "" {
		t.Fatalf("error=%q want empty (informational freshness unknown must not set Error)", resp.Error)
	}
}

func TestHandleStatusWithoutCluster(t *testing.T) {
	h := &Handler{svc: NewService(nil)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/market-data/status", nil)
	rr := httptest.NewRecorder()
	h.HandleStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var body StatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Autonomy != "L0" {
		t.Fatalf("autonomy=%s", body.Autonomy)
	}
	if len(body.Deployments) != 2 {
		t.Fatalf("deployments=%d", len(body.Deployments))
	}
	if body.FreshnessReach != probe.ReachUnknown {
		t.Fatalf("freshness_reach=%s want unknown", body.FreshnessReach)
	}
	if body.ReadinessRollup != nil {
		t.Fatalf("readiness_rollup=%+v want nil without cluster", body.ReadinessRollup)
	}
}

func TestParseReadinessRollupOutput(t *testing.T) {
	r := parseReadinessRollupOutput("1200|980|118|2026-08-01\n")
	if r == nil {
		t.Fatal("expected rollup")
	}
	if r.Universe != 1200 || r.SnapshotCovered != 980 || r.VendorGapCount != 118 || r.AsOf != "2026-08-01" {
		t.Fatalf("unexpected rollup: %+v", r)
	}
	if parseReadinessRollupOutput("") != nil {
		t.Fatal("empty should be nil")
	}
	if parseReadinessRollupOutput("bad|row") != nil {
		t.Fatal("short row should be nil")
	}
}

func TestStatusIncludesReadinessRollup(t *testing.T) {
	stocksMux := http.NewServeMux()
	stocksMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "pool": "stocks", "jobs_done": 1, "jobs_failed": 0, "uptime_sec": 1,
		})
	})
	stocksSrv := httptest.NewServer(stocksMux)
	defer stocksSrv.Close()

	optionsMux := http.NewServeMux()
	optionsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "pool": "options", "jobs_done": 1, "jobs_failed": 0, "uptime_sec": 1,
		})
	})
	optionsSrv := httptest.NewServer(optionsMux)
	defer optionsSrv.Close()

	svc := &Service{
		cfg: Config{
			StocksHealthURL:  stocksSrv.URL + "/health",
			OptionsHealthURL: optionsSrv.URL + "/health",
		},
		client: http.DefaultClient,
		deploymentsOverride: []DeploymentInfo{
			{Namespace: pluginNamespace, Name: stocksDeployName, Ready: "1/1", Reach: probe.ReachOK},
			{Namespace: pluginNamespace, Name: optionsDeployName, Ready: "1/1", Reach: probe.ReachOK},
		},
		freshnessProbe: func(ctx context.Context) ([]FreshnessInfo, probe.Reachability, string) {
			return []FreshnessInfo{{
				Dimension: "stock_daily", Status: "ok", AgeHours: 1, Verdict: "ok", RowsWritten: 10,
			}}, probe.ReachOK, ""
		},
		readinessProbe: func(ctx context.Context) *ReadinessRollup {
			return &ReadinessRollup{
				Universe: 5348, SnapshotRows: 13131, SnapshotCovered: 5307,
				VendorGapCount: 118, AsOf: "2026-08-19", Source: "plugin",
			}
		},
	}
	resp := svc.Status(context.Background())
	if resp.ReadinessRollup == nil || resp.ReadinessRollup.Universe != 5348 || resp.ReadinessRollup.VendorGapCount != 118 {
		t.Fatalf("readiness_rollup=%+v", resp.ReadinessRollup)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("reachability=%s (rollup must not affect reach)", resp.Reachability)
	}
}

func TestProbeReadinessRollupFromPluginAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/market/readiness/snapshot-coverage", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "row_count": 13131, "session_date": "2026-08-19",
			"by_instrument_type": []map[string]any{
				{"code": "CS", "snapshot_row_count": 5307, "universe_ticker_count": 5348},
			},
		})
	})
	mux.HandleFunc("/market/readiness/vendor-gap", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "gap_count": 118, "session_date": "2026-08-19"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	svc := &Service{
		cfg:    Config{APIBaseURL: srv.URL},
		client: http.DefaultClient,
	}
	r := svc.probeReadinessRollup(context.Background())
	if r == nil {
		t.Fatal("expected rollup")
	}
	if r.Universe != 5348 || r.SnapshotRows != 13131 || r.SnapshotCovered != 5307 || r.VendorGapCount != 118 {
		t.Fatalf("unexpected rollup: %+v", r)
	}
	if r.Source != "plugin" || r.AsOf != "2026-08-19" {
		t.Fatalf("source/as_of: %+v", r)
	}
}
