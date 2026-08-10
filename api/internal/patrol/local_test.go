package patrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalDispatcherProbesAndStreamsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mission/verify-snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"payload_verification": map[string]any{
					"summary": map[string]any{"overall": "PROBE_DRIFT"},
				},
			})
		case "/api/v1/matrix":
			_ = json.NewEncoder(w).Encode(map[string]any{"environments": []any{}})
		case "/api/v1/cluster/", "/api/v1/cluster":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/api/v1/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"north_star": "ops"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := &localDispatcher{client: srv.Client(), base: srv.URL}
	var steps []string
	out := d.Dispatch(context.Background(), PatrolSkill{
		ID:             "fleet-drift-scan",
		Name:           "Fleet Drift Scan",
		TrustLevel:     TrustL0,
		TimeoutSeconds: 15,
		MCPTools:       []string{"verify_mission_snapshot", "get_connectivity_matrix", "get_cluster_summary", "get_ops_context"},
	}, TriggerManual, "", func(ev string) { steps = append(steps, ev) })

	if out.Result != ResultSuccess {
		t.Fatalf("want success, got %+v", out)
	}
	if !strings.Contains(out.Evidence, "runtime: platform-api local probe") {
		t.Fatalf("evidence missing local runtime:\n%s", out.Evidence)
	}
	if !strings.Contains(out.Evidence, "**PROBE_DRIFT**") {
		t.Fatalf("expected rolled-up PROBE_DRIFT verdict:\n%s", out.Evidence)
	}
	if len(steps) < 4 {
		t.Fatalf("expected streamed progress steps, got %d", len(steps))
	}
}

func TestLocalDispatcherClusterDegradedIsProbeDrift(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mission/verify-snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"payload_overall": "ok",
				"payload_verification": map[string]any{
					"summary": map[string]any{"overall": "NOMINAL"},
				},
			})
		case "/api/v1/matrix":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"matrices": []any{
					map[string]any{
						"environment": "dev",
						"targets":     []any{map[string]any{"id": "nginx-spa", "reachability": "ok"}},
					},
				},
			})
		case "/api/v1/cluster/", "/api/v1/cluster":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"api_reachability": "ok",
				"reachability":     "degraded",
				"failing_pods":     1,
				"failing_pod_details": []any{
					map[string]any{
						"namespace": "monitoring",
						"name":      "promtail-7zs4l",
						"reason":    "CrashLoopBackOff",
					},
				},
			})
		case "/api/v1/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{"version": "test"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := &localDispatcher{client: srv.Client(), base: srv.URL}
	out := d.Dispatch(context.Background(), PatrolSkill{
		ID:             "fleet-drift-scan",
		Name:           "Fleet Drift Scan",
		TrustLevel:     TrustL0,
		TimeoutSeconds: 15,
		MCPTools:       []string{"verify_mission_snapshot", "get_connectivity_matrix", "get_cluster_summary", "get_ops_context"},
	}, TriggerManual, "", nil)
	if out.Result != ResultSuccess {
		t.Fatalf("scan should succeed (probes ran), got %+v", out)
	}
	if !strings.Contains(out.Evidence, "**PROBE_DRIFT**") {
		t.Fatalf("cluster degraded must lift verdict off NOMINAL:\n%s", out.Evidence)
	}
	if !strings.Contains(out.Evidence, "### Compare") || !strings.Contains(out.Evidence, "promtail-7zs4l") {
		t.Fatalf("expected compare notes with failing pod:\n%s", out.Evidence)
	}
}

func TestLocalDispatcherToolHTTPFailIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	d := &localDispatcher{client: srv.Client(), base: srv.URL}
	out := d.Dispatch(context.Background(), PatrolSkill{
		ID:             "fleet-drift-scan",
		Name:           "Fleet Drift Scan",
		TrustLevel:     TrustL0,
		TimeoutSeconds: 10,
		MCPTools:       []string{"get_ops_context"},
	}, TriggerManual, "", nil)
	if out.Result != ResultFailure {
		t.Fatalf("%+v", out)
	}
}

func TestHybridDispatcherRoutesL0ToLocal(t *testing.T) {
	var localHits, cloudHits int
	h := hybridDispatcher{
		local: dispatchFunc(func(context.Context, PatrolSkill, Trigger, string) dispatchOutcome {
			localHits++
			return dispatchOutcome{Result: ResultSuccess, Status: StatusCompleted, Evidence: "local"}
		}),
		cloud: dispatchFunc(func(context.Context, PatrolSkill, Trigger, string) dispatchOutcome {
			cloudHits++
			return dispatchOutcome{Result: ResultSuccess, Status: StatusCompleted, Evidence: "cloud"}
		}),
	}
	out := h.Dispatch(context.Background(), PatrolSkill{TrustLevel: TrustL0}, TriggerManual, "", nil)
	if out.Evidence != "local" || localHits != 1 || cloudHits != 0 {
		t.Fatalf("L0 should stay local: %+v local=%d cloud=%d", out, localHits, cloudHits)
	}
	// L1 without confirm → cloud
	out = h.Dispatch(context.Background(), PatrolSkill{TrustLevel: TrustL1}, TriggerManual, "", nil)
	if out.Evidence != "cloud" || cloudHits != 1 {
		t.Fatalf("L1 escalate should use cloud: %+v cloud=%d", out, cloudHits)
	}
	// L1 with confirm → local (deterministic actuation)
	out = h.Dispatch(context.Background(), PatrolSkill{TrustLevel: TrustL1, CronActuation: CronActuationConfirm}, TriggerCron, "", nil)
	if out.Evidence != "local" || localHits != 2 {
		t.Fatalf("L1 confirm should use local: %+v local=%d", out, localHits)
	}
}

func TestLocalDispatcherChainCleanupsTerminalPods(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/mission/verify-snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"payload_overall": "ok",
				"payload_verification": map[string]any{"summary": map[string]any{"overall": "NOMINAL"}},
			})
		case r.URL.Path == "/api/v1/matrix":
			_ = json.NewEncoder(w).Encode(map[string]any{"matrices": []any{}})
		case r.URL.Path == "/api/v1/cluster/" || r.URL.Path == "/api/v1/cluster":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"api_reachability": "ok",
				"reachability":     "degraded",
				"failing_pods":     3,
				"failing_pod_details": []any{
					map[string]any{"namespace": "bifrost-dev", "name": "api-monitor-old-abc12", "phase": "Succeeded", "reason": "Completed"},
					map[string]any{"namespace": "bifrost-dev", "name": "celery-worker-xyz99", "phase": "Failed", "reason": "OOMKilled"},
					map[string]any{"namespace": "bifrost-dev", "name": "daemon-fsm-abc", "phase": "Failed", "reason": "Error"},
				},
			})
		case r.URL.Path == "/api/v1/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"meta": "ok"})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/cluster/workloads/pods/"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/cluster/workloads/pods/"), "/")
			if len(parts) == 2 {
				deleted = append(deleted, parts[0]+"/"+parts[1])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"action": "delete_pod", "message": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := &localDispatcher{client: srv.Client(), base: srv.URL}
	out := d.Dispatch(context.Background(), PatrolSkill{
		ID:             "fleet-drift-scan",
		Name:           "Fleet Drift Scan",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 30,
		MCPTools:       []string{"verify_mission_snapshot", "get_connectivity_matrix", "get_cluster_summary", "get_ops_context", "delete_pod"},
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("chain run should succeed: %+v", out)
	}
	if !strings.Contains(out.Evidence, "### Chain Cleanup") {
		t.Fatalf("expected chain cleanup section:\n%s", out.Evidence)
	}
	// api-monitor-old-abc12 (Succeeded) → deleted
	// celery-worker-xyz99 (Failed) → deleted
	// daemon-fsm-abc (Failed, prefix "daemon") → SKIPPED (guardrail)
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deletions, got %d: %v\nevidence:\n%s", len(deleted), deleted, out.Evidence)
	}
	if !strings.Contains(out.Evidence, "SKIP bifrost-dev/daemon-fsm-abc") {
		t.Fatalf("daemon pod should be skipped by guardrail:\n%s", out.Evidence)
	}
	if !strings.Contains(out.Evidence, "2 pods cleaned") {
		t.Fatalf("expected cleanup count:\n%s", out.Evidence)
	}
}

func TestLocalDispatcherNoCleanupWhenNominal(t *testing.T) {
	var deleted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			deleted++
			return
		}
		switch r.URL.Path {
		case "/api/v1/mission/verify-snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"payload_overall":      "ok",
				"payload_verification": map[string]any{"summary": map[string]any{"overall": "NOMINAL"}},
			})
		case "/api/v1/matrix":
			_ = json.NewEncoder(w).Encode(map[string]any{"matrices": []any{}})
		case "/api/v1/cluster/", "/api/v1/cluster":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"api_reachability": "ok",
				"reachability":     "ok",
				"failing_pods":     0,
			})
		case "/api/v1/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"meta": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := &localDispatcher{client: srv.Client(), base: srv.URL}
	out := d.Dispatch(context.Background(), PatrolSkill{
		ID:             "fleet-drift-scan",
		Name:           "Fleet Drift Scan",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
		MCPTools:       []string{"verify_mission_snapshot", "get_connectivity_matrix", "get_cluster_summary", "get_ops_context", "delete_pod"},
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("nominal run should succeed: %+v", out)
	}
	if deleted != 0 {
		t.Fatal("no pods should be deleted when cluster is healthy")
	}
	if strings.Contains(out.Evidence, "Chain Cleanup") {
		t.Fatalf("chain cleanup should not appear for healthy cluster:\n%s", out.Evidence)
	}
	if !strings.Contains(out.Evidence, "**NOMINAL**") {
		t.Fatalf("expected NOMINAL verdict:\n%s", out.Evidence)
	}
}
