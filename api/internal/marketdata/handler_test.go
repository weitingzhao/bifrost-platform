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
}
