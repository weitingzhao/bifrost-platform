package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// mountTenant registers the tenant domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountTenant(r chi.Router) {
	r.Get("/satellite/bus-deep", s.satellite.HandleBusDeep)
	r.Get("/topology", s.handleTopology)
	r.Get("/plugins/ib-gateway/status", s.ibgateway.HandleStatus)
	r.Get("/plugins/ib-gateway/self-heal", s.ibgateway.HandleSelfHeal)
	r.Get("/plugins/market-data/status", s.marketdata.HandleStatus)
	r.Get("/plugins/flex-query/status", s.flexquery.HandleStatus)
	r.Get("/plugins/analytics/status", s.research.HandleLegacyAnalyticsRedirect)
	r.Get("/research/status", s.research.HandleStatus)
	r.Get("/plugins/research/status", s.research.HandleStatus)
	r.Get("/data-husbandry", s.datahusbandry.HandleSnapshot)
	r.Get("/watchlist/union", s.marketdata.HandleWatchlistUnion)
	// Read-only Plugin API proxy (coverage / analytics / ingest list / JSON probes).
	r.Get("/plugins/market-data/api/*", s.marketdata.HandleAPIProxy)
	r.Get("/plugins/flex-query/api/*", s.flexquery.HandleAPIProxy)
	r.Get("/plugins/analytics/api/*", s.research.HandleLegacyAnalyticsRedirect)
	// Research API (:8795) — preferred /research/* + plugin-style alias.
	r.Get("/research/*", s.research.HandleAPIProxy)
	r.Get("/plugins/research/api/*", s.research.HandleAPIProxy)
	r.Get("/trade-agent/domains", s.tradeagent.HandleDomains)
	r.Get("/trade-agent/catalog", s.tradeagent.HandleCatalog)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/plugins/ib-gateway/control/{action}", s.guardIB(s.ibgateway.HandleControl))
		// Ingest enqueue (and other Plugin API writes) — operator auth.
		// Proxy rewrites Authorization to MARKET_DATA_WRITE_TOKEN (not the operator token).
		r.Post("/plugins/market-data/api/*", s.marketdata.HandleAPIProxy)
		r.Delete("/plugins/market-data/api/*", s.guard("market_data_delete", s.marketdata.HandleAPIProxy))
		r.Post("/plugins/flex-query/api/*", s.flexquery.HandleAPIProxy)
	})
}
