// Package assembly is the delivery domain's assembly: it mounts the
// delivery's routes under /api/v1 (the release policy, Argo CD, the
// pipelines and their runs, the release window, the release records, the
// supply chain, the agentrt migrations). Moved out of server
// (mount_delivery.go) on 2026-11-19, unchanged: the same paths, gates,
// handlers and order (OWNER_LAW #95: a domain's routes sit in its own
// package, so the platform's assembly root wires domains instead of holding
// them).
//
// The pipelines' handler (package delivery) is recorded as shared with
// coordination, so this package names what it uses of it as an interface
// (Pipelines). The release policy's handlers (release_policy_wire.go,
// trust-anchored), "is my commit released", the release context and the
// migration run still live in server and are lent here with the gates.
package assembly

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migraterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// Pipelines is what the delivery routes use of the pipelines' handler
// (delivery.Handler has all of it).
type Pipelines interface {
	HandlePipelines(http.ResponseWriter, *http.Request)
	HandleSupplyChain(http.ResponseWriter, *http.Request)
	HandleRevisions(http.ResponseWriter, *http.Request)
	HandleCompare(http.ResponseWriter, *http.Request)
	HandleStgSmoke(http.ResponseWriter, *http.Request)
	HandlePipelineRuns(http.ResponseWriter, *http.Request)
	HandlePipelineTimings(http.ResponseWriter, *http.Request)
	HandleGetReleaseWindow(http.ResponseWriter, *http.Request)
	HandleRunLogs(http.ResponseWriter, *http.Request)
	HandleRunSteps(http.ResponseWriter, *http.Request)
	HandleStartPipelineRun(http.ResponseWriter, *http.Request)
	HandlePutReleaseWindow(http.ResponseWriter, *http.Request)
	HandleDeleteReleaseWindow(http.ResponseWriter, *http.Request)
	HandleSyncMirrors(http.ResponseWriter, *http.Request)
	HandleMirrorSync(http.ResponseWriter, *http.Request)
	HandleRefreshDockerfileCMs(http.ResponseWriter, *http.Request)
	HandleDeletePipelineRun(http.ResponseWriter, *http.Request)
}

// Gates are what the platform lends the delivery's routes: the viewer,
// operator and admin roles' gates and the catalogued action guard.
type Gates struct {
	Viewer   func(http.Handler) http.Handler
	Operator func(http.Handler) http.Handler
	Admin    func(http.Handler) http.Handler
	Guard    func(id string, next http.HandlerFunc) http.HandlerFunc
}

// Handlers are the delivery's handlers: the pipelines', Argo CD's and the
// release records', and the answers server holds.
type Handlers struct {
	Pipelines            Pipelines
	GitOps               *gitops.Handler
	Releases             *releases.Handler
	ReleasePolicy        http.HandlerFunc
	ReleasePolicyCheck   http.HandlerFunc
	ReleasePolicyInstall http.HandlerFunc
	ReleasePolicyStage   http.HandlerFunc
	ReleaseFreeze        http.HandlerFunc
	ReleaseUnfreeze      http.HandlerFunc
	Released             http.HandlerFunc
	ReleaseContext       http.HandlerFunc
	AgentrtMigration     http.HandlerFunc
	// ProdJudgements is the PROD head-release loop's judgements (headrelease.ProdJudgementsRoute).
	ProdJudgements http.Handler
}

// Mount registers the delivery domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func Mount(r chi.Router, g Gates, h Handlers) {
	d := h.Pipelines
	r.With(g.Viewer).Get("/release-policy", h.ReleasePolicy)
	r.Get("/gitops/apps", h.GitOps.HandleApps)
	r.Get("/delivery/pipelines", d.HandlePipelines)
	r.Get("/delivery/supply-chain", d.HandleSupplyChain)
	r.Get("/delivery/revisions", d.HandleRevisions)
	r.Get("/delivery/compare", d.HandleCompare)
	r.Get("/releases", h.Releases.HandleList)
	r.Get("/releases/running-images", h.Releases.HandleRunningImages)
	// A trial of the release policy on one run request: read-only, but its
	// reasons name the window holder and the records, so viewer and above.
	r.With(g.Viewer).Get("/delivery/pipelines/{name}/policy-check", h.ReleasePolicyCheck)
	// "Is my commit released": what each environment runs and whether it
	// has the commit (head_release.go). Read-only; viewer and above.
	r.With(g.Viewer).Get("/delivery/released", h.Released)
	r.Get("/delivery/stg/smoke", d.HandleStgSmoke)
	r.Get("/delivery/pipelines/{name}/runs", d.HandlePipelineRuns)
	r.With(g.Viewer).Get("/delivery/pipelines/{name}/timings", d.HandlePipelineTimings)
	r.With(g.Viewer).Get("/delivery/release-window", d.HandleGetReleaseWindow)
	// One read of a repository's release situation (release_context_wire.go):
	// read-only, viewer and above.
	r.With(g.Viewer).Get("/delivery/context", h.ReleaseContext)
	// Why PROD was or was not released (headrelease.ProdJudgementsRoute, 2026-11-22):
	// read-only, viewer and above.
	r.With(g.Viewer).Method(http.MethodGet, "/delivery/prod-judgements", h.ProdJudgements)
	r.Get("/delivery/runs/{id}/logs", d.HandleRunLogs)
	r.Get("/delivery/runs/{id}/steps", d.HandleRunSteps)
	r.Group(func(r chi.Router) {
		r.Use(g.Operator)
		r.Post("/gitops/apps/{name}/sync", g.Guard("gitops_sync_app", h.GitOps.HandleSyncApp))
		r.Post("/delivery/pipelines/{name}/runs", g.Guard("start_pipeline_run", d.HandleStartPipelineRun))
		r.Put("/delivery/release-window", g.Guard("release_window_hold", d.HandlePutReleaseWindow))
		r.Delete("/delivery/release-window", g.Guard("release_window_release", d.HandleDeleteReleaseWindow))
		r.Put("/release-policy", g.Guard("release_policy_install", h.ReleasePolicyInstall))
		r.Put("/release-policy/candidate", g.Guard("release_policy_stage", h.ReleasePolicyStage))
		r.Post("/release-policy/freeze", g.Guard("release_freeze", h.ReleaseFreeze))
		r.Post("/release-policy/unfreeze", g.Guard("release_unfreeze", h.ReleaseUnfreeze))
		r.Post("/delivery/mirrors/sync", g.Guard("sync_mirrors", d.HandleSyncMirrors))
		// Run this environment's pending agentrt migrations of a commit
		// (agentrt_migration.go): tier C, on the signed policy or a card.
		r.Post("/agentrt/migrations/run", g.Guard(migraterun.ActionID, h.AgentrtMigration))
		r.Post("/delivery/supply-chain/mirror-sync", d.HandleMirrorSync)
		r.Post("/delivery/supply-chain/dockerfile-configmaps/refresh", d.HandleRefreshDockerfileCMs)
		r.Delete("/delivery/runs/{id}", g.Guard("delete_pipeline_run", d.HandleDeletePipelineRun))
	})
	r.Group(func(r chi.Router) {
		r.Use(g.Admin)
		r.Post("/gitops/apps/{name}/rollback", g.Guard("gitops_rollback_app", h.GitOps.HandleRollbackApp))
	})
}
