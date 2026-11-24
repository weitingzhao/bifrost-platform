// Package assembly is the tenant domain's assembly: it builds the tenant's
// handlers (the plugins, research, the data clone, data husbandry, the
// satellite and the trade agent catalog) and mounts the tenant's routes
// under /api/v1. Moved out of server (server.go's construction lines and
// mount_tenant.go) on 2026-11-19, unchanged: the same paths, gates, handlers
// and order (OWNER_LAW #95: a domain's routes sit in its own package, so the
// platform's assembly root wires domains instead of holding them).
//
// What the tenant reaches outside itself is lent to it: the cluster's
// Kubernetes and PostgreSQL adapters (Kube), the gates (Gates), and the
// topology answer, which reads the prober and the telemetry (server keeps
// that glue).
package assembly

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/dataclone"
	"github.com/weitingzhao/bifrost-platform/api/internal/flexquery"
	"github.com/weitingzhao/bifrost-platform/api/internal/ibgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/marketdata"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/research"
	"github.com/weitingzhao/bifrost-platform/api/internal/satellite"
	husbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
	"github.com/weitingzhao/bifrost-platform/api/internal/tradeagent"
)

// Kube is what the tenant's handlers use of the cluster: each package's own
// Kube, and the data clone's PostgreSQL adapter. The cluster service has all.
type Kube interface {
	marketdata.Kube
	flexquery.Kube
	research.Kube
	ibgateway.Kube
	dataclone.Postgres
}

// Tenant holds the tenant's handlers. The platform's action wiring and its
// ops events name some of them (IBGateway, MarketData, DataClone), so they
// stay readable.
type Tenant struct {
	IBGateway  *ibgateway.Handler
	MarketData *marketdata.Handler
	FlexQuery  *flexquery.Handler
	Research   *research.Handler
	DataClone  *dataclone.Handler
	Husbandry  *husbandry.Handler
	Satellite  *satellite.Handler
	TradeAgent *tradeagent.Handler
}

// New builds the tenant's handlers as server.New did. Starting background
// loops stays with the caller, which knows the process's role.
func New(cfg *config.Config, entry *config.ClusterEntry, audit *actuation.AuditLog, kube Kube) *Tenant {
	t := &Tenant{
		DataClone:  dataclone.NewHandler(entry, audit, kube),
		MarketData: marketdata.NewHandler(kube),
		IBGateway:  ibgateway.NewHandler(kube, audit),
		FlexQuery:  flexquery.NewHandler(kube),
		Research:   research.NewHandler(kube),
		Satellite:  satellite.NewHandler(cfg),
		TradeAgent: tradeagent.NewHandler(),
	}
	t.Husbandry = husbandry.NewHandler(husbandry.NewService(t.MarketData.Service(), t.FlexQuery.Service(), t.Research.Service()))
	return t
}

// PluginProbes are the plugins' health probes for /metrics, in the order
// server listed them.
func (t *Tenant) PluginProbes() []func(context.Context) probe.PluginHealth {
	return []func(context.Context) probe.PluginHealth{
		t.MarketData.PluginHealth,
		t.FlexQuery.PluginHealth,
		t.IBGateway.PluginHealth,
		t.Research.PluginHealth,
	}
}

// Gates are what the platform lends the tenant's routes: the operator role's
// gate, the catalogued action guard, the IB control guard, and the topology
// answer.
type Gates struct {
	Operator func(http.Handler) http.Handler
	Guard    func(id string, next http.HandlerFunc) http.HandlerFunc
	GuardIB  func(next http.HandlerFunc) http.HandlerFunc
	Topology http.HandlerFunc
}

// Mount registers the tenant domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (t *Tenant) Mount(r chi.Router, g Gates) {
	r.Get("/satellite/bus-deep", t.Satellite.HandleBusDeep)
	r.Get("/topology", g.Topology)
	r.Get("/plugins/ib-gateway/status", t.IBGateway.HandleStatus)
	r.Get("/plugins/ib-gateway/self-heal", t.IBGateway.HandleSelfHeal)
	r.Get("/plugins/market-data/status", t.MarketData.HandleStatus)
	r.Get("/plugins/flex-query/status", t.FlexQuery.HandleStatus)
	r.Get("/research/status", t.Research.HandleStatus)
	r.Get("/data-husbandry", t.Husbandry.HandleSnapshot)
	r.Get("/watchlist/union", t.MarketData.HandleWatchlistUnion)
	// Read-only Plugin API proxy (coverage / analytics / ingest list / JSON probes).
	r.Get("/plugins/market-data/api/*", t.MarketData.HandleAPIProxy)
	r.Get("/plugins/flex-query/api/*", t.FlexQuery.HandleAPIProxy)
	// Research API (:8795) — preferred /research/* + plugin-style alias.
	r.Get("/research/*", t.Research.HandleAPIProxy)
	r.Get("/plugins/research/api/*", t.Research.HandleAPIProxy)
	r.Get("/trade-agent/catalog", t.TradeAgent.HandleCatalog)
	r.Group(func(r chi.Router) {
		r.Use(g.Operator)
		r.Post("/plugins/ib-gateway/control/{action}", g.GuardIB(t.IBGateway.HandleControl))
		// Ingest enqueue (and other Plugin API writes) — operator auth.
		// Proxy rewrites Authorization to MARKET_DATA_WRITE_TOKEN (not the operator token).
		r.Post("/plugins/market-data/api/*", t.MarketData.HandleAPIProxy)
		r.Delete("/plugins/market-data/api/*", g.Guard("market_data_delete", t.MarketData.HandleAPIProxy))
		r.Post("/plugins/flex-query/api/*", t.FlexQuery.HandleAPIProxy)
	})
}
