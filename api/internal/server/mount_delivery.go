package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// mountDelivery registers the delivery domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountDelivery(r chi.Router) {
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/release-policy", s.handleReleasePolicy)
	r.Get("/gitops/apps", s.gitops.HandleApps)
	r.Get("/delivery/pipelines", s.delivery.HandlePipelines)
	r.Get("/delivery/supply-chain", s.delivery.HandleSupplyChain)
	r.Get("/delivery/revisions", s.delivery.HandleRevisions)
	r.Get("/delivery/compare", s.delivery.HandleCompare)
	r.Get("/releases", s.releases.HandleList)
	r.Get("/releases/running-images", s.releases.HandleRunningImages)
	r.Get("/delivery/pipelines/{name}/preflight", s.delivery.HandlePipelinePreflight)
	// A trial of the release policy on one run request: read-only, but its
	// reasons name the window holder and the records, so viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/delivery/pipelines/{name}/policy-check", s.handleReleasePolicyCheck)
	// "Is my commit released": what each environment runs and whether it
	// has the commit (head_release.go). Read-only; viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/delivery/released", s.handleReleased)
	r.Get("/delivery/stg/smoke", s.delivery.HandleStgSmoke)
	r.Get("/delivery/pipelines/{name}/runs", s.delivery.HandlePipelineRuns)
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/delivery/pipelines/{name}/timings", s.delivery.HandlePipelineTimings)
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/delivery/release-window", s.delivery.HandleGetReleaseWindow)
	// One read of a repository's release situation (release_context_wire.go):
	// read-only, viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/delivery/context", s.handleReleaseContext)
	r.Get("/delivery/runs/{id}/logs", s.delivery.HandleRunLogs)
	r.Get("/delivery/runs/{id}/steps", s.delivery.HandleRunSteps)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/gitops/apps/{name}/sync", s.guard("gitops_sync_app", s.gitops.HandleSyncApp))
		r.Post("/delivery/pipelines/{name}/runs", s.guard("start_pipeline_run", s.delivery.HandleStartPipelineRun))
		r.Put("/delivery/release-window", s.guard("release_window_hold", s.delivery.HandlePutReleaseWindow))
		r.Delete("/delivery/release-window", s.guard("release_window_release", s.delivery.HandleDeleteReleaseWindow))
		r.Put("/release-policy", s.guard("release_policy_install", s.handleReleasePolicyInstall))
		r.Put("/release-policy/candidate", s.guard("release_policy_stage", s.handleReleasePolicyStage))
		r.Post("/release-policy/freeze", s.guard("release_freeze", s.handleReleaseFreeze))
		r.Post("/release-policy/unfreeze", s.guard("release_unfreeze", s.handleReleaseUnfreeze))
		r.Post("/delivery/mirrors/sync", s.guard("sync_mirrors", s.delivery.HandleSyncMirrors))
		// Run this environment's pending agentrt migrations of a commit
		// (agentrt_migration.go): tier C, on the signed policy or a card.
		r.Post("/agentrt/migrations/run", s.guard(migrationActionID, s.handleAgentrtMigration))
		r.Post("/delivery/supply-chain/mirror-sync", s.delivery.HandleMirrorSync)
		r.Post("/delivery/supply-chain/dockerfile-configmaps/refresh", s.delivery.HandleRefreshDockerfileCMs)
		r.Delete("/delivery/runs/{id}", s.guard("delete_pipeline_run", s.delivery.HandleDeletePipelineRun))
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleAdmin))
		r.Post("/gitops/apps/{name}/rollback", s.guard("gitops_rollback_app", s.gitops.HandleRollbackApp))
	})
}
