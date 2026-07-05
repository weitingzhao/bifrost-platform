package satellite

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type allSatelliteBusDeepResponse struct {
	Buses []BusDeepResponse `json:"buses"`
}

func newSatelliteFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/monitor/status":
			_, _ = w.Write([]byte(`{
				"health": {"self_check":"ok","block_reasons":[],"status_lamp":"green"},
				"daemon": {"self_check":"ok","lamp":"green","block_reasons":[],"trading":{"trading_suspended":false},"heartbeat":{"daemon_alive":true}},
				"socket": {
					"massive":{"lamp":"green","self_check":"ok"},
					"ib_ingestor":{"lamp":"green","self_check":"ok"},
					"ib_account_agent":{"lamp":"green","self_check":"ok"},
					"ib_operator":{"lamp":"green","self_check":"ok"},
					"platform_ib_gateway":{"lamp":"green","self_check":"ok"}
				},
				"celery":{"broker_connected":true,"workers":["w1"],"worker_ib_connected":true},
				"account_sync_daemon":{"heartbeat":{"daemon_alive":true,"stream_lag":0}}
			}`))
		case "/api/ops/health":
			_, _ = w.Write([]byte(`{"status":"ok","service":"bifrost-ops","executor_mode":"local"}`))
		case "/api/ops/ops/market-ingest/services":
			_, _ = w.Write([]byte(`{
				"ok": true,
				"services": [
					{"id":"massive_ws","process_active":"active"},
					{"id":"ib_ingestor","process_active":"active"},
					{"id":"ib_account_agent","process_active":"active"},
					{"id":"ib_operator","process_active":"active"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHandleBusDeepSingleEnvironment(t *testing.T) {
	srv := newSatelliteFixtureServer(t)
	h := NewHandler(&config.Config{
		Environments: []config.Environment{
			{ID: "stg", Label: "Stg", NginxBase: srv.URL},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/satellite/bus-deep?env=stg", nil)
	rec := httptest.NewRecorder()
	h.HandleBusDeep(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload BusDeepResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Environment != "stg" {
		t.Fatalf("expected environment stg, got %q", payload.Environment)
	}
	if payload.Monitor.Reachability == "" {
		t.Fatalf("expected monitor reachability to be populated")
	}
}

func TestHandleBusDeepAllEnvironmentsAggregation(t *testing.T) {
	srv := newSatelliteFixtureServer(t)
	h := NewHandler(&config.Config{
		Environments: []config.Environment{
			{ID: "stg", Label: "Stg", NginxBase: srv.URL},
			{ID: "prod", Label: "Prod", NginxBase: srv.URL},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/satellite/bus-deep", nil)
	rec := httptest.NewRecorder()
	h.HandleBusDeep(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload allSatelliteBusDeepResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Buses) != 2 {
		t.Fatalf("expected 2 buses, got %d", len(payload.Buses))
	}
	if payload.Buses[0].Environment != "stg" || payload.Buses[1].Environment != "prod" {
		t.Fatalf("unexpected buses order/content: %+v", payload.Buses)
	}
}

func TestHandleBusDeepInvalidEnvironment(t *testing.T) {
	srv := newSatelliteFixtureServer(t)
	h := NewHandler(&config.Config{
		Environments: []config.Environment{
			{ID: "stg", Label: "Stg", NginxBase: srv.URL},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/satellite/bus-deep?env=dev", nil)
	rec := httptest.NewRecorder()
	h.HandleBusDeep(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected error message in payload")
	}
}
