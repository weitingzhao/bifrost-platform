package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// mountFoundation registers the foundation domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountFoundation(r chi.Router) {
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/ops/facts/system", s.handleOpsSystemFacts)
	r.Get("/environments", s.handleEnvironments)
	r.Get("/matrix", s.handleMatrix)
	r.Get("/telemetry/overview", s.telemetry.HandleOverview)
	r.Get("/telemetry/query", s.telemetry.HandleQuery)
	r.Get("/telemetry/promql", s.telemetry.HandlePromQL)
	r.Get("/telemetry/alerts", s.telemetry.HandleAlerts)
	r.Get("/telemetry/targets", s.telemetry.HandleTargets)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/telemetry/attention-mute", s.telemetry.HandleAttentionMute)
	})
	r.Get("/mission/verify-payload", s.handleVerifyPayload)
	r.Get("/mission/verify-snapshot", s.handleVerifyMissionSnapshot)
	r.Get("/self-health", s.selfhealth.HandleSelfHealth)
	r.Get("/context", s.handleContext)
	r.Get("/auth/capabilities", s.auth.Capabilities)
	r.Get("/audit", s.audit.HandleList)
	r.Get("/jobs", s.jobs.HandleList)
	r.Get("/mcp/tools", s.mcp.HandleTools)
	r.Get("/mcp/status", s.mcp.HandleStatus)
	r.Get("/network/status", s.network.HandleStatus)
	r.Get("/network/zones", s.network.HandleZones)
	r.Get("/network/policies", s.network.HandlePolicies)
	r.Get("/network/audit", s.network.HandleAudit)
	r.Get("/network/devices", s.network.HandleDevices)
	r.Get("/network/clients", s.network.HandleClients)
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/network/clients/{mac}", s.network.HandleClientByMAC)
	r.Get("/network/health", s.network.HandleHealth)
	r.Get("/network/bandwidth", s.network.HandleBandwidth)
	r.Get("/network/anomalies", s.network.HandleAnomalies)
	r.Get("/network/sla", s.network.HandleSLA)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/audit/append", s.audit.HandleAppend)
	})
	r.Get("/checklist/signals", s.checklist.HandleGetSignals)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleReporter))
		// Diagnostics and audit only. Reporter or above: Alertmanager must
		// not hold the operator token (LANE-B4).
		r.Post("/ops-agent/alertmanager", s.opsagent.HandleAlertmanager)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/network/clients/fixed-ip", s.guard("unifi_client_fixed_ip", s.network.HandleClientFixedIP))
		r.Post("/network/firewall/apply", s.guard("unifi_firewall_apply", s.network.HandleFirewallApply))
	})
}
