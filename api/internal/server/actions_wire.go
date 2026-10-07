package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// guard lets B and X calls through. X stays on the existing handler so the
// TD-222 D10 daemon scale-up refusal is unchanged. C and D return 403 until an
// executed approval carries the same params hash.
func (s *Server) guard(id string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := readAndRestore(r)
		if err != nil {
			next(w, r)
			return
		}
		act, ok := actions.ByID(id)
		if !ok {
			next(w, r)
			return
		}
		params, err := act.Extract(r, raw)
		if err != nil || act.Missing(params) != "" {
			next(w, r)
			return
		}
		tier := act.TierOf(r.Context(), params)
		if !tier.NeedsApproval() {
			next(w, r)
			return
		}
		hash, err := actions.ParamsHash(params)
		if err != nil || s.approvals == nil || !s.approvals.HasExecuted(id, hash) {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":  "approval required",
				"action": id,
			})
			return
		}
		next(w, r)
	}
}

// guardIB dispatches the shared control route onto the catalogued action.
func (s *Server) guardIB(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimSpace(chi.URLParam(r, "action")) {
		case "reconnect":
			s.guard("ib_reconnect", next)(w, r)
		case "mode":
			s.guard("ib_mode", next)(w, r)
		case "maintenance":
			s.guard("ib_maintenance", next)(w, r)
		case "self-heal":
			s.guard("ib_self_heal", next)(w, r)
		default:
			next(w, r)
		}
	}
}

func (s *Server) lookupDaemonReplicas(ctx context.Context, namespace string) (int32, bool) {
	if s.cluster == nil {
		return 0, false
	}
	resp := s.cluster.Service().Workloads(ctx, namespace)
	if resp.Reachability != probe.ReachOK {
		return 0, false
	}
	for _, w := range resp.Workloads {
		if w.Kind == "Deployment" && w.Name == "daemon" {
			return w.DesiredReplicas, true
		}
	}
	return 0, false
}

func (s *Server) bindActionExecutors() {
	reg := func(id, method string, urlOf func(map[string]any) string, routeOf func(map[string]any) map[string]string, bodyOf func(map[string]any) any, h http.HandlerFunc) {
		actions.RegisterExecutor(id, func(ctx context.Context, params map[string]any) (any, error) {
			var route map[string]string
			if routeOf != nil {
				route = routeOf(params)
			}
			var body any
			if bodyOf != nil {
				body = bodyOf(params)
			}
			return invokeAction(ctx, h, method, urlOf(params), route, body)
		})
	}
	path := func(keys ...string) func(map[string]any) map[string]string {
		return func(p map[string]any) map[string]string {
			out := make(map[string]string, len(keys))
			for _, k := range keys {
				out[k] = text(p[k])
			}
			return out
		}
	}
	fields := func(keys ...string) func(map[string]any) any {
		return func(p map[string]any) any {
			m := map[string]any{}
			for _, k := range keys {
				if v, ok := p[k]; ok {
					m[k] = v
				}
			}
			if len(m) == 0 {
				return nil
			}
			return m
		}
	}
	fixed := func(k, v string) func(map[string]any) map[string]string {
		return func(map[string]any) map[string]string { return map[string]string{k: v} }
	}
	all := func(p map[string]any) any {
		if len(p) == 0 {
			return nil
		}
		return p
	}

	reg("start_pipeline_run", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/delivery/pipelines/" + url.PathEscape(text(p["name"])) + "/runs"
		},
		path("name"), fields("revision", "tag", "who"), s.delivery.HandleStartPipelineRun)

	reg("delete_pipeline_run", http.MethodDelete,
		func(p map[string]any) string {
			u := "/api/v1/delivery/runs/" + url.PathEscape(text(p["id"]))
			if ns := text(p["ns"]); ns != "" {
				u += "?ns=" + url.QueryEscape(ns)
			}
			return u
		},
		path("id"), nil, s.delivery.HandleDeletePipelineRun)

	reg("gitops_sync_app", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/gitops/apps/" + url.PathEscape(text(p["name"])) + "/sync"
		},
		path("name"), nil, s.gitops.HandleSyncApp)

	reg("gitops_rollback_app", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/gitops/apps/" + url.PathEscape(text(p["name"])) + "/rollback"
		},
		path("name"), fields("revision"), s.gitops.HandleRollbackApp)

	reg("rollout_restart_deployment", http.MethodPost,
		func(map[string]any) string { return "/api/v1/cluster/workloads/rollout-restart" },
		nil, fields("namespace", "kind", "name"), s.cluster.HandleRolloutRestart)

	reg("scale_deployment", http.MethodPost,
		func(map[string]any) string { return "/api/v1/cluster/workloads/scale" },
		nil, fields("namespace", "kind", "name", "replicas"), s.cluster.HandleScale)

	reg("delete_pod", http.MethodDelete,
		func(p map[string]any) string {
			return "/api/v1/cluster/workloads/pods/" + url.PathEscape(text(p["namespace"])) + "/" + url.PathEscape(text(p["name"]))
		},
		path("namespace", "name"), nil, s.cluster.HandleDeletePod)

	reg("cordon_node", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/cluster/nodes/" + url.PathEscape(text(p["name"])) + "/cordon"
		},
		path("name"), nil, s.cluster.HandleCordonNode)
	reg("uncordon_node", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/cluster/nodes/" + url.PathEscape(text(p["name"])) + "/uncordon"
		},
		path("name"), nil, s.cluster.HandleUncordonNode)
	reg("drain_node", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/cluster/nodes/" + url.PathEscape(text(p["name"])) + "/drain"
		},
		path("name"), fields("force", "delete_local_data", "grace_period_seconds"), s.cluster.HandleDrainNode)
	reg("poweroff_compute_node", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/cluster/nodes/" + url.PathEscape(text(p["name"])) + "/poweroff"
		},
		path("name"), nil, s.cluster.HandlePowerOffNode)
	reg("wake_compute_node", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/cluster/nodes/" + url.PathEscape(text(p["name"])) + "/wake"
		},
		path("name"), nil, s.cluster.HandleWakeNode)
	reg("join_cluster_node", http.MethodPost,
		func(map[string]any) string { return "/api/v1/cluster/nodes/join" },
		nil, fields("profile"), s.cluster.HandleJoinNode)
	reg("trigger_cnpg_backup", http.MethodPost,
		func(map[string]any) string { return "/api/v1/cluster/postgres/backup" },
		nil, nil, s.cluster.HandleTriggerPostgresBackup)
	reg("repair_cnpg_wal_store", http.MethodPost,
		func(map[string]any) string { return "/api/v1/cluster/postgres/wal-store/repair" },
		nil, nil, s.cluster.HandleRepairPostgresWalStore)
	reg("trigger_data_clone", http.MethodPost,
		func(map[string]any) string { return "/api/v1/cluster/data-clone" },
		nil, all, s.cluster.HandleDataClone)
	reg("market_data_heal", http.MethodPost,
		func(map[string]any) string { return "/api/v1/plugins/market-data/api/market/doctor/heal" },
		fixed("*", "market/doctor/heal"), all, s.marketdata.HandleAPIProxy)
	reg("ib_reconnect", http.MethodPost,
		func(map[string]any) string { return "/api/v1/plugins/ib-gateway/control/reconnect" },
		fixed("action", "reconnect"), nil, s.ibgateway.HandleControl)
	reg("ib_mode", http.MethodPost,
		func(map[string]any) string { return "/api/v1/plugins/ib-gateway/control/mode" },
		fixed("action", "mode"), fields("mode"), s.ibgateway.HandleControl)
	reg("ib_maintenance", http.MethodPost,
		func(map[string]any) string { return "/api/v1/plugins/ib-gateway/control/maintenance" },
		fixed("action", "maintenance"), all, s.ibgateway.HandleControl)
	reg("ib_self_heal", http.MethodPost,
		func(map[string]any) string { return "/api/v1/plugins/ib-gateway/control/self-heal" },
		fixed("action", "self-heal"), all, s.ibgateway.HandleControl)
	reg("unifi_firewall_apply", http.MethodPost,
		func(map[string]any) string { return "/api/v1/network/firewall/apply" },
		nil, fields("include_default_deny"), s.network.HandleFirewallApply)
	reg("stack_install_addon", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/stack/addons/" + url.PathEscape(text(p["name"])) + "/install"
		},
		path("name"), nil, s.stack.HandleInstallAddon)
	reg("stack_upgrade_addon", http.MethodPost,
		func(p map[string]any) string {
			return "/api/v1/stack/addons/" + url.PathEscape(text(p["name"])) + "/upgrade"
		},
		path("name"), nil, s.stack.HandleUpgradeAddon)
}

func invokeAction(ctx context.Context, h http.HandlerFunc, method, urlPath string, routeParams map[string]string, body any) (any, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://platform.local"+urlPath, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rctx := chi.NewRouteContext()
	for k, v := range routeParams {
		rctx.URLParams.Add(k, v)
	}
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h(rec, req)
	return interpretAction(rec.Code, rec.Body.Bytes())
}

func interpretAction(code int, raw []byte) (any, error) {
	raw = bytes.TrimSpace(raw)
	var parsed any
	if len(raw) > 0 {
		if json.Unmarshal(raw, &parsed) != nil {
			parsed = string(raw)
		}
	}
	if code >= 200 && code < 300 {
		return parsed, nil
	}
	return nil, fmt.Errorf("%s", actionErrText(parsed, raw, code))
}

func actionErrText(parsed any, raw []byte, code int) string {
	if m, ok := parsed.(map[string]any); ok {
		for _, k := range []string{"error", "message"} {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	if len(raw) > 0 && len(raw) < 500 {
		return string(raw)
	}
	return fmt.Sprintf("action failed: HTTP %d", code)
}

func readAndRestore(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

func text(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
