package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatorplane"
	"github.com/weitingzhao/bifrost-platform/api/internal/patrol"
)

// mountFoundation registers the foundation domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountFoundation(r chi.Router) {
	// The probe's reads and the ops context are foundation's handlers;
	// assembly only registers them (law 72: assembly only wires).
	pr := s.probeRoutes()
	r.Get("/environments", pr.HandleEnvironments)
	r.Get("/matrix", pr.HandleMatrix)
	r.Get("/telemetry/overview", s.telemetry.HandleOverview)
	r.Get("/telemetry/promql", s.telemetry.HandlePromQL)
	r.Get("/telemetry/alerts", s.telemetry.HandleAlerts)
	r.Get("/telemetry/targets", s.telemetry.HandleTargets)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/telemetry/attention-mute", s.telemetry.HandleAttentionMute)
	})
	r.Get("/mission/verify-payload", pr.HandleVerifyPayload)
	r.Get("/mission/verify-snapshot", pr.HandleVerifyMissionSnapshot)
	r.Get("/self-health", s.selfhealth.HandleSelfHealth)
	r.Get("/context", s.promote.HandleOpsContext(s.cfg.OpsContext))
	r.Get("/auth/capabilities", s.auth.Capabilities)
	r.Get("/audit", s.audit.HandleList)
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

// newOperatorPlane is the L-1 surface: served in this process, or proxied to
// the out-of-band plane when OPERATOR_PLANE_URL is set; patrol is mounted
// beside it either way. Lines of New until 2026-11-16, unchanged.
func newOperatorPlane(cfg *config.Config, auth *actuation.AuthService, audit *actuation.AuditLog, role config.Role) (plane *operatorplane.Plane, planeURL string, mountPlane func(chi.Router), err error) {
	// L-1 lives out of band once OPERATOR_PLANE_URL is set: forward those routes.
	// /patrol/* is never forwarded. This process serves it from the shared
	// statefile and does not start the autopilot — that loop runs on workers.
	planeURL = strings.TrimSpace(os.Getenv("OPERATOR_PLANE_URL"))
	var patrolH *patrol.Handler
	if planeURL == "" {
		plane, err = operatorplane.New(operatorplane.Deps{
			Auth:      auth,
			Audit:     audit,
			ConfigDir: cfg.ConfigDir(),
		})
		if err != nil {
			return nil, "", nil, fmt.Errorf("operator plane: %w", err)
		}
		if role.RunsWorkers() {
			plane.StartBackground(context.Background())
		}
		patrolH = plane.Patrol()
		mountPlane = func(r chi.Router) {
			plane.Mount(r)
			operatorplane.MountPatrol(auth, patrolH, r)
		}
	} else {
		mountPlane, err = operatorplane.NewProxyMount(auth, planeURL)
		if err != nil {
			return nil, "", nil, fmt.Errorf("operator plane proxy: %w", err)
		}
		patrolH, err = patrol.NewHandler(cfg.ConfigDir())
		if err != nil {
			return nil, "", nil, fmt.Errorf("patrol: %w", err)
		}
		proxy := mountPlane
		mountPlane = func(r chi.Router) {
			proxy(r)
			operatorplane.MountPatrol(auth, patrolH, r)
		}
		slog.Info("operator plane is out of band", "url", planeURL, "note", "L-1 routes proxied; patrol stays local; no patrol autopilot in this process")
	}
	return plane, planeURL, mountPlane, nil
}
