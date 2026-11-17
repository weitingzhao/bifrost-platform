package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// mountOwner registers the owner domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountOwner(r chi.Router) {
	r.Get("/agent/governance/trust-matrix", s.agentgovernance.HandleTrustMatrix)
	r.Get("/agent/governance/trust-overrides", s.agentgovernance.HandleTrustOverrides)
	// What the approval records say, counted by day and requester, each
	// count with the request numbers behind it. Read-only; viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/readings/approvals", s.handleApprovalReadings)
	// The action record (action_record.go): what was done, by whom, why and on
	// which law, newest first, by seat, law, action and time. Read-only.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/records", s.handleRecords)
	r.Get("/actuation/manifests/plans/{id}", s.handlePlanGet)
	// Recording a skill's own outcome is evidence, not actuation. It sits
	// below operator on purpose: the workload that reports must not also be
	// able to scale deployments or write its own trust override.
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleReporter))
		r.Post("/agent/governance/skill-runs", s.agentgovernance.HandleRecordSkillRun)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/actuation/manifests/plan", s.guard("plan_manifest", s.handlePlan))
		r.Post("/actuation/manifests/apply", s.guard("apply_manifest", s.handleApply))
		r.Post("/actuation/jobs/from-cronjob", s.guard("create_job_from_cronjob", s.handleCreateJob))
		r.Post("/actuation/jobs/delete-finished", s.guard("delete_finished_jobs", s.handleDeleteFinished))
		r.Post("/actuation/probes", s.guard("run_probe_pod", s.handleProbe))
	})
}
