package satellite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestBusDeepParsesFixtureAndAggregatesReachability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/monitor/status":
			_, _ = w.Write([]byte(`{
				"health": {"self_check":"degraded","block_reasons":["socket_warning"],"status_lamp":"yellow"},
				"daemon": {
					"self_check":"degraded",
					"lamp":"yellow",
					"block_reasons":["ib_retrying"],
					"trading":{"trading_suspended":true,"auto_status":{"state":"waiting_ib"}},
					"heartbeat":{"daemon_alive":true,"ib_connected":false,"last_ts":1751700000}
				},
				"socket": {
					"polygon_ws":{"lamp":"green","self_check":"ok","status":"connected"},
					"ib_ingestor":{"lamp":"yellow","self_check":"degraded","status":"retrying"},
					"ib_account_agent":{"lamp":"green","self_check":"ok","status":"connected"},
					"ib_operator":{"lamp":"green","self_check":"ok","status":"connected"},
					"platform_ib_gateway":{"lamp":"yellow","self_check":"degraded","status":"partial"}
				},
				"account_sync_daemon":{"heartbeat":{"daemon_alive":true,"stream_lag":3,"last_ts":1751700012}}
			}`))
		case "/api/ops/health":
			_, _ = w.Write([]byte(`{
				"status":"ok",
				"service":"bifrost-ops",
				"executor_mode":"kubernetes",
				"k8s_reachable":false
			}`))
		case "/api/ops/ops/market-ingest/services":
			_, _ = w.Write([]byte(`{
				"ok": true,
				"services": [
					{"id":"polygon_ws","process_active":"inactive","runtime_status":"policy-off","display_active":"ws-disabled (REST-only)","runtime_kind":"kubernetes"},
					{"id":"ib_ingestor","process_active":"inactive","runtime_status":"active","display_active":"managed@platform-ib-gateway","runtime_kind":"kubernetes","platform_gateway_managed":true},
					{"id":"ib_account_agent","process_active":"inactive","runtime_status":"active","display_active":"managed@platform-ib-gateway","runtime_kind":"kubernetes","platform_gateway_managed":true},
					{"id":"ib_operator","process_active":"inactive","runtime_status":"active","display_active":"managed@platform-ib-gateway","runtime_kind":"kubernetes","platform_gateway_managed":true},
					{"id":"trading_engine","process_active":"inactive","runtime_status":"policy-off","display_active":"policy-off (daemon scale 0)","runtime_kind":"kubernetes"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		Environments: []config.Environment{{
			ID:        "stg",
			Label:     "Stg",
			NginxBase: srv.URL,
		}},
	}
	svc := NewService(cfg)
	resp, err := svc.BusDeep(context.Background(), "stg")
	if err != nil {
		t.Fatalf("BusDeep error: %v", err)
	}

	if resp.Monitor.Health.StatusLamp != "yellow" {
		t.Fatalf("expected monitor health lamp yellow, got %q", resp.Monitor.Health.StatusLamp)
	}
	if resp.Monitor.Daemon.Heartbeat["daemon_alive"] != true {
		t.Fatalf("expected daemon_alive true in heartbeat, got %#v", resp.Monitor.Daemon.Heartbeat)
	}
	if resp.Monitor.AccountSync.Reachability != probe.ReachOK {
		t.Fatalf("expected account sync ok, got %s", resp.Monitor.AccountSync.Reachability)
	}
	if resp.Ops.K8sReachable == nil || *resp.Ops.K8sReachable {
		t.Fatalf("expected k8s_reachable=false, got %+v", resp.Ops.K8sReachable)
	}
	if resp.Ingest.Reachability != probe.ReachOK {
		t.Fatalf("expected ingest ok with semantic runtime_status rows, got %s", resp.Ingest.Reachability)
	}
	if resp.Monitor.Socket.PolygonWs.Reachability != probe.ReachOK {
		t.Fatalf("expected polygon_ws policy-off ok, got %s", resp.Monitor.Socket.PolygonWs.Reachability)
	}
	if resp.Monitor.Socket.Massive.Reachability != "" && resp.Monitor.Socket.Massive.Reachability != probe.ReachUnknown {
		t.Fatalf("expected massive field unset/empty, got %s", resp.Monitor.Socket.Massive.Reachability)
	}
	if resp.Ingest.Services[0].ID != "polygon_ws" {
		t.Fatalf("expected ingest id polygon_ws, got %q", resp.Ingest.Services[0].ID)
	}
	if resp.Monitor.Socket.IBIngestor.Reachability != probe.ReachDegraded {
		t.Fatalf("expected ib_ingestor degraded from lamp, got %s", resp.Monitor.Socket.IBIngestor.Reachability)
	}
	if resp.Ingest.Services[1].DisplayActive != "managed@platform-ib-gateway" {
		t.Fatalf("expected display_active passthrough, got %q", resp.Ingest.Services[1].DisplayActive)
	}
	if resp.Reachability != probe.ReachDegraded {
		t.Fatalf("expected overall degraded, got %s", resp.Reachability)
	}
}

func TestBusDeepAllHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/monitor/status":
			_, _ = w.Write([]byte(`{
				"health": {"self_check":"ok","block_reasons":[],"status_lamp":"green"},
				"daemon": {"self_check":"ok","lamp":"green","block_reasons":[],"trading":{"trading_suspended":false},"heartbeat":{"daemon_alive":true}},
				"socket": {
					"polygon_ws":{"lamp":"green","self_check":"ok"},
					"ib_ingestor":{"lamp":"green","self_check":"ok"},
					"ib_account_agent":{"lamp":"green","self_check":"ok"},
					"ib_operator":{"lamp":"green","self_check":"ok"},
					"platform_ib_gateway":{"lamp":"green","self_check":"ok"}
				},
				"account_sync_daemon":{"heartbeat":{"daemon_alive":true,"stream_lag":0}}
			}`))
		case "/api/ops/health":
			_, _ = w.Write([]byte(`{"status":"ok","executor_mode":"local"}`))
		case "/api/ops/ops/market-ingest/services":
			_, _ = w.Write([]byte(`{
				"ok": true,
				"services": [
					{"id":"polygon_ws","process_active":"active"},
					{"id":"ib_ingestor","process_active":"active"},
					{"id":"ib_account_agent","process_active":"active"},
					{"id":"ib_operator","process_active":"active"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		Environments: []config.Environment{{
			ID:        "prod",
			Label:     "Prod",
			NginxBase: srv.URL,
		}},
	}
	svc := NewService(cfg)
	resp, err := svc.BusDeep(context.Background(), "prod")
	if err != nil {
		t.Fatalf("BusDeep error: %v", err)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("expected overall ok, got %s", resp.Reachability)
	}
	if resp.Monitor.Reachability != probe.ReachOK {
		t.Fatalf("expected monitor ok, got %s", resp.Monitor.Reachability)
	}
	if resp.Ingest.Reachability != probe.ReachOK {
		t.Fatalf("expected ingest ok, got %s", resp.Ingest.Reachability)
	}
}

func TestBusDeepBridgeMode(t *testing.T) {
	monitorBody := `{
		"health": {"self_check":"ok","block_reasons":[],"status_lamp":"green"},
		"daemon": {"self_check":"ok","lamp":"green","block_reasons":[],"trading":{},"heartbeat":{"daemon_alive":true}},
		"socket": {
			"polygon_ws":{"lamp":"green","self_check":"ok"},
			"ib_ingestor":{"lamp":"green","self_check":"ok"},
			"ib_account_agent":{"lamp":"green","self_check":"ok"},
			"ib_operator":{"lamp":"green","self_check":"ok"},
			"platform_ib_gateway":{"lamp":"green","self_check":"ok"}
		},
		"account_sync_daemon":{"heartbeat":{"daemon_alive":true,"stream_lag":0}}
	}`
	ingestBody := `{"ok":true,"services":[{"id":"polygon_ws","process_active":"active"}]}`

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bus-snapshot" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"source":"local-compose",
			"trade_nginx_base":"http://127.0.0.1:80",
			"monitor":{"ok":true,"body":` + monitorBody + `},
			"market_ingest":{"ok":true,"body":` + ingestBody + `}
		}`))
	}))
	defer bridge.Close()

	cfg := &config.Config{
		Environments: []config.Environment{{
			ID:             "dev-local",
			Label:          "Local",
			ProbeMode:      "bridge",
			TradeBridgeURL: bridge.URL,
		}},
	}
	svc := NewService(cfg)
	resp, err := svc.BusDeep(context.Background(), "dev-local")
	if err != nil {
		t.Fatalf("BusDeep error: %v", err)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("expected bridge overall ok, got %s", resp.Reachability)
	}
	if resp.Monitor.Reachability != probe.ReachOK {
		t.Fatalf("expected monitor ok via bridge, got %s", resp.Monitor.Reachability)
	}
	if len(resp.Ingest.Services) != 1 {
		t.Fatalf("expected 1 ingest service, got %d", len(resp.Ingest.Services))
	}
}

func TestSocketComponentDeepPlatformGatewayConnected(t *testing.T) {
	raw := map[string]any{
		"connected":     true,
		"transport":     "platform_gateway",
		"health_source": "platform_ib_gateway",
		"status":        "connected",
	}
	got := socketComponentDeep(raw)
	if got.Reachability != probe.ReachOK {
		t.Fatalf("expected ok when platform_gateway connected, got %s", got.Reachability)
	}
}

func TestSocketComponentDeepMassiveRestOnly(t *testing.T) {
	raw := map[string]any{
		"ws_mode":      "rest_only",
		"ws_connected": false,
		"configured":   false,
		"reachability": "unknown",
	}
	got := socketComponentDeep(raw)
	if got.Reachability != probe.ReachOK {
		t.Fatalf("expected policy-off ok for rest_only, got %s", got.Reachability)
	}
	if !strings.Contains(got.Detail, "policy-off") {
		t.Fatalf("expected policy-off detail, got %q", got.Detail)
	}
}
