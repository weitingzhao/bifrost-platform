package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// mountVerification registers the verification domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountVerification(r chi.Router) {
	// Code-health ratchet readings (agent-config/scripts/code-health/scan.sh).
	// Read is viewer-level; writing is operator-gated so a reading cannot be
	// overwritten by anything that can reach the port.
	r.Route("/code-health", func(r chi.Router) {
		r.Get("/", s.codehealth.HandleGet)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/report", s.codehealth.HandleReport)
			r.Post("/rescan", s.codehealth.HandleRescan)
		})
	})
	// "Is this commit green", from the evidence ledger: read-only; it names
	// repositories, commits and CI runs, so viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/evidence/commit", s.handleEvidenceCommit)
	// The ledger's answers beside the release policy's CI reading, for
	// every commit that reading knows: read-only, viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/evidence/compare", s.handleEvidenceCompare)
	// The ledger's "did CI pass" for one commit, for the release scripts
	// and the in-cluster gates: no token, as /releases beside it.
	r.Get("/evidence/ci", s.handleEvidenceCI)
	// CI's plan of one commit: its units, their keys, the ledger's word on
	// each. Read-only; viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/units/plan", s.handleUnitPlan)
	// Which modules of a commit run again against a base, from the two
	// declarations. Read-only; no token, as /evidence/ci (units_scope.go).
	r.Get("/units/scope", s.handleUnitScope)
	// How CI is set up for one project, from its own declaration (the
	// configuration view). Read-only; viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/units/config", s.handleUnitConfig)
	// The daily full CI's notes: the runs started, and for each red one
	// what failed and which landings left a module out. Read-only; viewer
	// and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/units/full-ci", s.handleUnitFullCI)
	// What the reconciliation of a red full run would name, for two
	// commits given; it writes nothing. Read-only; viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/units/full-ci/check", s.handleUnitFullCICheck)
	// The newest CI runs of one project, for the run view. Read-only;
	// viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/units/runs", s.handleUnitRuns)
	// How each repository uses CI and how CI serves them, scored from
	// the retained runs (ci_scorecard.go). Read-only.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/ci/scorecard", s.handleCIScorecard)
	// The CI page's two reads about domains: who imports whom at main's
	// head, and which domains each commit changed (ci_domains.go;
	// contracts, version 1). Read-only.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/ci/domains", s.handleCIDomains)
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/ci/changes", s.handleCIChanges)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		// Run the unit tasks of a commit the ledger has no verdict on
		// (units_dispatch.go); refused while UNIT_DISPATCH is off.
		r.Post("/units/evidence", s.guard("request_unit_evidence", s.handleUnitEvidenceRequest))
	})
}
