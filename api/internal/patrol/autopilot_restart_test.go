package patrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
)

// Restart routing: the platform's own process and git-bridge restart last, by
// rollout in the cluster and by dev session on a laptop.

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
