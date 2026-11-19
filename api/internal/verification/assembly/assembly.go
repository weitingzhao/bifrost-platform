// Package assembly is the verification domain's assembly: it mounts the
// verification's routes under /api/v1 (the evidence ledger's reads, the CI
// plan, scope, configuration, runs and full-CI notes, the scorecard, the
// domain graph, and the request for unit evidence). Moved out of server
// (mount_verification.go) on 2026-11-19, unchanged: the same paths, gates,
// handlers and order (OWNER_LAW #28: a domain's routes sit in its own
// package, so the platform's assembly root wires domains instead of holding
// them).
//
// The handlers still live in server (ci_read_wire.go, units_*.go), which
// binds them to delivery's Gitea access and other domains' reads; they are
// lent here with the gates.
package assembly

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/versionedread"
)

// Gates are what the platform lends the verification's routes: the viewer
// and operator roles' gates and the catalogued action guard.
type Gates struct {
	Viewer   func(http.Handler) http.Handler
	Operator func(http.Handler) http.Handler
	Guard    func(id string, next http.HandlerFunc) http.HandlerFunc
}

// Handlers are the verification's answers server holds.
type Handlers struct {
	EvidenceCommit      http.HandlerFunc
	EvidenceCI          http.HandlerFunc
	UnitPlan            http.HandlerFunc
	UnitScope           http.HandlerFunc
	UnitConfig          http.HandlerFunc
	UnitFullCI          http.HandlerFunc
	UnitFullCICheck     http.HandlerFunc
	UnitRuns            http.HandlerFunc
	CIScorecard         http.HandlerFunc
	CIDomains           http.HandlerFunc
	CIChanges           http.HandlerFunc
	UnitEvidenceRequest http.HandlerFunc
}

// Mount registers the verification domain's routes under /api/v1. They were
// lines of Router until 2026-11-16, in one list ordered by who may call them;
// each route keeps its path, its handler and the same gate.
func Mount(r chi.Router, g Gates, h Handlers) {
	// "Is this commit green", from the evidence ledger: read-only; it names
	// repositories, commits and CI runs, so viewer and above.
	r.With(g.Viewer).Get("/evidence/commit", h.EvidenceCommit)
	// The ledger's "did CI pass" for one commit, for the release scripts
	// and the in-cluster gates: no token, as /releases beside it.
	r.With(versionedread.Middleware).Get("/evidence/ci", h.EvidenceCI)
	// CI's plan of one commit: its units, their keys, the ledger's word on
	// each. Read-only; viewer and above.
	r.With(g.Viewer).Get("/units/plan", h.UnitPlan)
	// Which modules of a commit run again against a base, from the two
	// declarations. Read-only; no token, as /evidence/ci (units_scope.go).
	r.With(versionedread.Middleware).Get("/units/scope", h.UnitScope)
	// How CI is set up for one project, from its own declaration (the
	// configuration view). Read-only; viewer and above.
	r.With(g.Viewer).Get("/units/config", h.UnitConfig)
	// The daily full CI's notes: the runs started, and for each red one
	// what failed and which landings left a module out. Read-only; viewer
	// and above.
	r.With(g.Viewer).Get("/units/full-ci", h.UnitFullCI)
	// What the reconciliation of a red full run would name, for two
	// commits given; it writes nothing. Read-only; viewer and above.
	r.With(g.Viewer).Get("/units/full-ci/check", h.UnitFullCICheck)
	// The newest CI runs of one project, for the run view. Read-only;
	// viewer and above.
	r.With(g.Viewer).Get("/units/runs", h.UnitRuns)
	// How each repository uses CI and how CI serves them, scored from
	// the retained runs (ci_scorecard.go). Read-only.
	r.With(g.Viewer).Get("/ci/scorecard", h.CIScorecard)
	// The CI page's two reads about domains: who imports whom at main's
	// head, and which domains each commit changed (ci_domains.go;
	// contracts, version 1). Read-only.
	r.With(g.Viewer).Get("/ci/domains", h.CIDomains)
	r.With(g.Viewer).Get("/ci/changes", h.CIChanges)
	r.Group(func(r chi.Router) {
		r.Use(g.Operator)
		// Run the unit tasks of a commit the ledger has no verdict on
		// (units_dispatch.go); refused while UNIT_DISPATCH is off.
		r.Post("/units/evidence", g.Guard("request_unit_evidence", h.UnitEvidenceRequest))
	})
}
