package satellite

import (
	"context"
	"net/http"
	"net/http/httptest"
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
					"massive":{"lamp":"green","self_check":"ok","status":"connected"},
					"ib_ingestor":{"lamp":"yellow","self_check":"degraded","status":"retrying"},
					"ib_account_agent":{"lamp":"green","self_check":"ok","status":"connected"},
					"ib_operator":{"lamp":"green","self_check":"ok","status":"connected"},
					"platform_ib_gateway":{"lamp":"yellow","self_check":"degraded","status":"partial"}
				},
				"celery":{"broker_connected":true,"workers":["w1"],"worker_ib_connected":false,"worker_ib_client_id":77,"worker_last_updated_ts":1751700012},
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
					{"id":"massive_ws","process_active":"active","runtime_kind":"kubernetes"},
					{"id":"ib_ingestor","process_active":"inactive","runtime_kind":"kubernetes","platform_gateway_managed":true},
					{"id":"ib_account_agent","process_active":"inactive","runtime_kind":"kubernetes","platform_gateway_managed":true},
					{"id":"ib_operator","process_active":"inactive","runtime_kind":"kubernetes","platform_gateway_managed":true}
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
	if !resp.Monitor.Celery.BrokerConnected || len(resp.Monitor.Celery.Workers) != 1 {
		t.Fatalf("expected parsed celery workers, got %+v", resp.Monitor.Celery)
	}
	if resp.Monitor.AccountSync.Reachability != probe.ReachOK {
		t.Fatalf("expected account sync ok, got %s", resp.Monitor.AccountSync.Reachability)
	}
	if resp.Ops.K8sReachable == nil || *resp.Ops.K8sReachable {
		t.Fatalf("expected k8s_reachable=false, got %+v", resp.Ops.K8sReachable)
	}
	if resp.Ingest.Reachability != probe.ReachDegraded {
		t.Fatalf("expected ingest degraded with platform gateway managed rows, got %s", resp.Ingest.Reachability)
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
					"massive":{"lamp":"green","self_check":"ok"},
					"ib_ingestor":{"lamp":"green","self_check":"ok"},
					"ib_account_agent":{"lamp":"green","self_check":"ok"},
					"ib_operator":{"lamp":"green","self_check":"ok"},
					"platform_ib_gateway":{"lamp":"green","self_check":"ok"}
				},
				"celery":{"broker_connected":true,"workers":["w1","w2"],"worker_ib_connected":true},
				"account_sync_daemon":{"heartbeat":{"daemon_alive":true,"stream_lag":0}}
			}`))
		case "/api/ops/health":
			_, _ = w.Write([]byte(`{"status":"ok","executor_mode":"local"}`))
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
