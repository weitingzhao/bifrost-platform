package patrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
)

// newTestAutopilot wires up an autopilotDispatcher against a test HTTP server.
func newTestAutopilot(t *testing.T, srv *httptest.Server) *autopilotDispatcher {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	throttle, err := NewRestartThrottle(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return &autopilotDispatcher{
		client:   srv.Client(),
		base:     srv.URL,
		throttle: throttle,
	}
}

func signalsJSON(signals []checklist.ItemSignal) []byte {
	resp := checklist.SignalsResponse{Signals: signals}
	raw, _ := json.Marshal(resp)
	return raw
}

func TestAutopilotFixesRedItems(t *testing.T) {
	var restartedDeployments []string
	var deletedPods []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/v1/checklist/signals" && r.Method == http.MethodGet:
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "redis", Signal: "fail", Detail: "connection refused"},
				{ItemID: "nginx-edge", Signal: "degraded", Detail: "502 errors"},
				{ItemID: "ib-feed", Signal: "fail", Detail: "disconnected"},
			}))

		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart" && r.Method == http.MethodPost:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["kind"] != "Deployment" || body["name"] == "" {
				http.Error(w, "missing kind/name", http.StatusBadRequest)
				return
			}
			restartedDeployments = append(restartedDeployments, body["namespace"]+"/"+body["name"])
			_ = json.NewEncoder(w).Encode(map[string]any{"action": "rollout_restart", "message": "ok"})

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/cluster/workloads/pods/"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/cluster/workloads/pods/"), "/")
			if len(parts) == 2 {
				deletedPods = append(deletedPods, parts[0]+"/"+parts[1])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})

		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 30,
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("expected success, got %+v\nevidence:\n%s", out, out.Evidence)
	}

	// redis (full_auto) and nginx-edge (full_auto) should be restarted
	if len(restartedDeployments) != 2 {
		t.Fatalf("expected 2 rollout restarts, got %d: %v", len(restartedDeployments), restartedDeployments)
	}

	// ib-feed should be blocked (safe boundary)
	if !strings.Contains(out.Evidence, "BLOCKED") || !strings.Contains(out.Evidence, "D10") {
		t.Fatalf("ib-feed should be blocked by safe boundary:\n%s", out.Evidence)
	}
}

func TestAutopilotAllOkNoFixes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/checklist/signals" {
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "redis", Signal: "ok"},
				{ItemID: "nginx-edge", Signal: "ok"},
				{ItemID: "platform-api", Signal: "ok"},
			}))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("all-ok should succeed: %+v", out)
	}
	if !strings.Contains(out.Evidence, "ALL_OK") {
		t.Fatalf("expected ALL_OK verdict:\n%s", out.Evidence)
	}
}

func TestAutopilotFailedFixIsNotRetried(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "redis", Signal: "fail", Detail: "down"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			callCount++
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	// Should complete even though fix failed (1 attempt, no retry)
	if out.Status != StatusCompleted {
		t.Fatalf("should be completed: %+v", out)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 fix attempt (no retry), got %d", callCount)
	}
	if !strings.Contains(out.Evidence, "1 failed") {
		t.Fatalf("evidence should report 1 failed fix:\n%s", out.Evidence)
	}
}

func TestAutopilotSafeBoundaryBlocksDBAndD10(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/checklist/signals" {
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "ib-feed", Signal: "fail", Detail: "disconnected"},
			}))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if !strings.Contains(out.Evidence, "BLOCKED") {
		t.Fatalf("ib-feed should be blocked:\n%s", out.Evidence)
	}
	if !strings.Contains(out.Evidence, "D10") {
		t.Fatalf("evidence should mention D10:\n%s", out.Evidence)
	}
}

func TestAutopilotThrottlePreventsReRestart(t *testing.T) {
	restartCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "redis", Signal: "fail", Detail: "down"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			restartCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	throttle, _ := NewRestartThrottle(dir, func() time.Time { return now })
	ap := &autopilotDispatcher{
		client:   srv.Client(),
		base:     srv.URL,
		throttle: throttle,
	}
	skill := PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}

	// First run: should fix
	out := ap.Dispatch(context.Background(), skill, TriggerCron, "", nil)
	if restartCount != 1 {
		t.Fatalf("first run should restart, got count=%d\nevidence:\n%s", restartCount, out.Evidence)
	}

	// Second run: throttled (same 15-min window, well within 24h)
	out = ap.Dispatch(context.Background(), skill, TriggerCron, "", nil)
	if restartCount != 1 {
		t.Fatalf("second run should be throttled, restart count should stay 1, got %d\nevidence:\n%s", restartCount, out.Evidence)
	}
	if !strings.Contains(out.Evidence, "THROTTLED") {
		t.Fatalf("evidence should show throttled:\n%s", out.Evidence)
	}
}

func TestAutopilotSelfRestartIsLast(t *testing.T) {
	var actionOrder []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "platform-api", Signal: "fail", Detail: "unhealthy"},
				{ItemID: "redis", Signal: "fail", Detail: "down"},
				{ItemID: "platform-console", Signal: "degraded", Detail: "503"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			actionOrder = append(actionOrder, body["name"])
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case strings.HasPrefix(r.URL.Path, "/api/v1/dev-sessions/") && strings.HasSuffix(r.URL.Path, "/control"):
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/dev-sessions/")
			name = strings.TrimSuffix(name, "/control")
			actionOrder = append(actionOrder, name)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 30,
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\nevidence:\n%s", out, out.Evidence)
	}

	// redis should be restarted BEFORE platform-api and platform-console
	if len(actionOrder) < 3 {
		t.Fatalf("expected 3 restarts, got %d: %v", len(actionOrder), actionOrder)
	}
	// First action should be redis (non-self-restart)
	if actionOrder[0] != "redis" {
		t.Fatalf("redis should be first, got order: %v", actionOrder)
	}
	// platform-api and platform-console should be last two (via dev-session or rollout-restart)
	lastTwo := actionOrder[len(actionOrder)-2:]
	hasPlatformAPI := false
	hasPlatformConsole := false
	for _, a := range lastTwo {
		if a == "platform-api" {
			hasPlatformAPI = true
		}
		if a == "platform-console" {
			hasPlatformConsole = true
		}
	}
	if !hasPlatformAPI || !hasPlatformConsole {
		t.Fatalf("platform-api and platform-console should be last, got order: %v", actionOrder)
	}

	// Evidence should contain pre-self-restart summary section
	if !strings.Contains(out.Evidence, "pre-self-restart") {
		t.Fatalf("should have pre-self-restart summary:\n%s", out.Evidence)
	}
}

func TestAutopilotSelfRestartInClusterUsesRollout(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.43.0.1")
	t.Setenv("OPS_VIEWER_ENV", "")
	var rolloutCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "platform-api", Signal: "fail", Detail: "unhealthy"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			rolloutCalls = append(rolloutCalls, body["namespace"]+"/"+body["name"])
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\nevidence:\n%s", out, out.Evidence)
	}
	if len(rolloutCalls) != 1 || rolloutCalls[0] != "bifrost-platform-stg/platform-api" {
		t.Fatalf("in-cluster self-restart should use rollout-restart, got: %v", rolloutCalls)
	}
}

func TestAutopilotSelfRestartLocalUsesDevSession(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	var devSessionCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "platform-api", Signal: "fail", Detail: "unhealthy"},
			}))
		case strings.HasPrefix(r.URL.Path, "/api/v1/dev-sessions/") && strings.HasSuffix(r.URL.Path, "/control"):
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/dev-sessions/")
			name = strings.TrimSuffix(name, "/control")
			devSessionCalls = append(devSessionCalls, name)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\nevidence:\n%s", out, out.Evidence)
	}
	if len(devSessionCalls) != 1 || devSessionCalls[0] != "platform-api" {
		t.Fatalf("local self-restart should use dev-session, got: %v", devSessionCalls)
	}
}

func TestAutopilotObserveManualSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/checklist/signals" {
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "mac-probe-bridge", Signal: "fail", Detail: "unreachable"},
			}))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if !strings.Contains(out.Evidence, "observe/manual") {
		t.Fatalf("manual items should be logged as skipped:\n%s", out.Evidence)
	}
}

func TestAutopilotSignalFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if out.Result != ResultFailure {
		t.Fatalf("should fail when signals unavailable: %+v", out)
	}
	if !strings.Contains(out.Error, "failed to fetch checklist signals") {
		t.Fatalf("error should mention signal fetch failure: %s", out.Error)
	}
}

func TestAutopilotSemiAutoFixedDirectly(t *testing.T) {
	var syncedApps []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "argo-apps", Signal: "fail", Detail: "app bifrost-dev OutOfSync"},
			}))
		case strings.HasPrefix(r.URL.Path, "/api/v1/gitops/apps/") && strings.HasSuffix(r.URL.Path, "/sync"):
			app := strings.TrimPrefix(r.URL.Path, "/api/v1/gitops/apps/")
			app = strings.TrimSuffix(app, "/sync")
			syncedApps = append(syncedApps, app)
			_ = json.NewEncoder(w).Encode(map[string]any{"synced": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\nevidence:\n%s", out, out.Evidence)
	}
	// argo-apps is semi_auto — should be fixed directly (not queued)
	if len(syncedApps) != 1 || syncedApps[0] != "bifrost-dev" {
		t.Fatalf("argo-apps semi_auto should be synced directly, got: %v\nevidence:\n%s", syncedApps, out.Evidence)
	}
}

func TestAutopilotLocalDispatcherDelegation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/checklist/signals" {
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "redis", Signal: "ok"},
			}))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	d := &localDispatcher{client: srv.Client(), base: srv.URL}
	out := d.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)

	// Should be delegated to autopilot (ALL_OK because all signals ok)
	if out.Result != ResultSuccess {
		t.Fatalf("delegation should succeed: %+v", out)
	}
	if !strings.Contains(out.Evidence, "Ops Autopilot") {
		t.Fatalf("evidence should indicate autopilot ran:\n%s", out.Evidence)
	}
}

func TestResolveNamespacesByViewerEnv(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "")
	if got := resolveTradeNamespace(); got != "bifrost-dev" {
		t.Fatalf("default trade ns = %s", got)
	}
	if got := resolvePlatformNamespace(); got != "bifrost-platform-stg" {
		t.Fatalf("default platform ns = %s", got)
	}
	if got := resolveMarketDataNamespace(); got != "plugin-market-data" {
		t.Fatalf("default market-data ns = %s", got)
	}

	t.Setenv("OPS_VIEWER_ENV", "stg")
	if got := resolveTradeNamespace(); got != "bifrost-stg" {
		t.Fatalf("stg trade ns = %s", got)
	}
	if got := resolveMarketDataNamespace(); got != "plugin-market-data-stg" {
		t.Fatalf("stg market-data ns = %s", got)
	}

	t.Setenv("OPS_VIEWER_ENV", "prod")
	if got := resolveTradeNamespace(); got != "bifrost-prod" {
		t.Fatalf("prod trade ns = %s", got)
	}
	if got := resolvePlatformNamespace(); got != "bifrost-platform-prod" {
		t.Fatalf("prod platform ns = %s", got)
	}
	if got := resolveMarketDataNamespace(); got != "plugin-market-data-prod" {
		t.Fatalf("prod market-data ns = %s", got)
	}
}

func TestAutopilotRolloutRestartSendsKindAndName(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "dev")
	var bodies []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "nginx-edge", Signal: "fail", Detail: "502"},
				{ItemID: "massive-polygon", Signal: "fail", Detail: "ws down"},
				{ItemID: "postgres", Signal: "fail", Detail: "CNPG degraded"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)
	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\n%s", out, out.Evidence)
	}
	if len(bodies) != 2 {
		t.Fatalf("nginx + massive should restart (postgres observe-only), got %d bodies: %v\n%s", len(bodies), bodies, out.Evidence)
	}
	nginxOK, massiveOK := false, false
	for _, b := range bodies {
		if b["kind"] != "Deployment" {
			t.Fatalf("kind must be Deployment, got %#v", b)
		}
		target := b["namespace"] + "/" + b["name"]
		if target == "bifrost-dev/nginx" {
			nginxOK = true
		}
		if target == "plugin-market-data/polygon-worker-stocks" {
			massiveOK = true
		}
	}
	if !nginxOK || !massiveOK {
		t.Fatalf("unexpected rollout targets: %v", bodies)
	}
	if !strings.Contains(out.Evidence, "OBSERVE-ONLY") {
		t.Fatalf("postgres should be OBSERVE-ONLY skipped:\n%s", out.Evidence)
	}
	if !strings.Contains(out.Evidence, "1 skipped") {
		t.Fatalf("postgres observe-only should tally as skipped, not failed:\n%s", out.Evidence)
	}
	if strings.Contains(out.Evidence, "1 failed") || strings.Contains(out.Error, "failed") {
		t.Fatalf("observe-only must not count as failed:\n%s\nerr=%s", out.Evidence, out.Error)
	}
}

func TestAutopilotObserveOnlyIsSkippedNotFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/checklist/signals" {
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "postgres", Signal: "fail", Detail: "CNPG degraded"},
				{ItemID: "deliver-pipeline", Signal: "fail", Detail: "stale"},
				{ItemID: "stg-smoke", Signal: "degraded", Detail: "1 target"},
				{ItemID: "runners-ha", Signal: "fail", Detail: "1/2"},
				{ItemID: "hermes-tooling", Signal: "fail", Detail: "down"},
			}))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)
	if out.Result != ResultSuccess {
		t.Fatalf("observe-only reds should still succeed: %+v\n%s", out, out.Evidence)
	}
	if out.Error != "" {
		t.Fatalf("no real fix failures expected, got err=%q\n%s", out.Error, out.Evidence)
	}
	if !strings.Contains(out.Evidence, "5 skipped") {
		t.Fatalf("all 5 observe-only items should be skipped:\n%s", out.Evidence)
	}
	if strings.Contains(out.Evidence, "FIX ERROR") {
		t.Fatalf("observe-only must not log FIX ERROR:\n%s", out.Evidence)
	}
	if strings.Count(out.Evidence, "OBSERVE-ONLY") != 5 {
		t.Fatalf("expected 5 OBSERVE-ONLY markers:\n%s", out.Evidence)
	}
}

func TestAutopilotGitBridgeInClusterUsesRollout(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.43.0.1")
	t.Setenv("OPS_VIEWER_ENV", "stg")
	var rolloutCalls []string
	var devSessionCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "git-bridge", Signal: "fail", Detail: "unhealthy"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			rolloutCalls = append(rolloutCalls, body["namespace"]+"/"+body["name"])
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case strings.HasPrefix(r.URL.Path, "/api/v1/dev-sessions/"):
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/dev-sessions/")
			name = strings.TrimSuffix(name, "/control")
			devSessionCalls = append(devSessionCalls, name)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)
	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\n%s", out, out.Evidence)
	}
	if len(devSessionCalls) != 0 {
		t.Fatalf("in-cluster git-bridge must not use bdev, got %v", devSessionCalls)
	}
	if len(rolloutCalls) != 1 || rolloutCalls[0] != "bifrost-platform-stg/git-bridge" {
		t.Fatalf("in-cluster git-bridge should rollout-restart platform NS, got %v", rolloutCalls)
	}
}

func TestAutopilotTriggersCnpgBackupWhenStale(t *testing.T) {
	var repairPosts int
	var backupPosts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "redis", Signal: "ok"},
			}))
		case r.URL.Path == "/api/v1/cluster/postgres/backup-status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fresh": false, "signal": "fail", "detail": "last completed 60h ago",
			})
		case r.URL.Path == "/api/v1/cluster/postgres/wal-store/repair" && r.Method == http.MethodPost:
			repairPosts++
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "action": "repair_cnpg_wal_store"})
		case r.URL.Path == "/api/v1/cluster/postgres/backup" && r.Method == http.MethodPost:
			backupPosts++
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "action": "trigger_cnpg_backup"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)
	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\n%s", out, out.Evidence)
	}
	if repairPosts != 1 {
		t.Fatalf("stale backup should repair WAL store once, got %d\n%s", repairPosts, out.Evidence)
	}
	if backupPosts != 0 {
		t.Fatalf("Autopilot should call wal-store/repair (includes trigger), not backup directly, got %d\n%s", backupPosts, out.Evidence)
	}
	if !strings.Contains(out.Evidence, "repair_cnpg_wal_store") {
		t.Fatalf("evidence should mention repair_cnpg_wal_store:\n%s", out.Evidence)
	}
}

func TestAutopilotGitBridgeLocalUsesDevSession(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	var rolloutCalls int
	var devSessionCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/checklist/signals":
			w.Write(signalsJSON([]checklist.ItemSignal{
				{ItemID: "git-bridge", Signal: "fail", Detail: "unhealthy"},
			}))
		case r.URL.Path == "/api/v1/cluster/workloads/rollout-restart":
			rolloutCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case strings.HasPrefix(r.URL.Path, "/api/v1/dev-sessions/") && strings.HasSuffix(r.URL.Path, "/control"):
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/dev-sessions/")
			name = strings.TrimSuffix(name, "/control")
			devSessionCalls = append(devSessionCalls, name)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)

	ap := newTestAutopilot(t, srv)
	out := ap.Dispatch(context.Background(), PatrolSkill{
		ID:             "ops-autopilot",
		Name:           "Ops Autopilot",
		TrustLevel:     TrustL1,
		CronActuation:  CronActuationConfirm,
		TimeoutSeconds: 15,
	}, TriggerCron, "", nil)
	if out.Result != ResultSuccess {
		t.Fatalf("expected success: %+v\n%s", out, out.Evidence)
	}
	if rolloutCalls != 0 {
		t.Fatalf("local git-bridge must not rollout-restart, calls=%d", rolloutCalls)
	}
	if len(devSessionCalls) != 1 || devSessionCalls[0] != "git-bridge" {
		t.Fatalf("local git-bridge should use dev-session, got %v", devSessionCalls)
	}
}
