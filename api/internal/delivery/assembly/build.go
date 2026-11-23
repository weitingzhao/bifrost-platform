package assembly

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasetable"
)

// Built holds the delivery handlers the platform builds here and reads by
// name: Argo CD's (GitOps) and the ops context's (Promote). Until
// 2026-11-23 server built them itself and imported gitops and promote, which
// delivery does not make public (E5: assembly reaches a domain through its
// public packages).
type Built struct {
	GitOps  *gitops.Handler
	Promote *promote.Handler
}

// Build builds them as server.New did; kube is the cluster's adapter Argo
// CD's handler reads through.
func Build(cfg *config.Config, audit *actuation.AuditLog, kube gitops.Kube) *Built {
	return &Built{GitOps: gitops.NewHandler(cfg, audit, kube), Promote: promote.NewHandler(cfg)}
}

// ArgoApp is one Argo CD application as the platform's self-health reads it.
type ArgoApp struct{ Name, SyncStatus, HealthStatus string }

// ArgoApps reads Argo CD's applications; nil without a handler.
func (b *Built) ArgoApps() func(ctx context.Context) []ArgoApp {
	if b == nil || b.GitOps == nil || b.GitOps.Service() == nil {
		return nil
	}
	svc := b.GitOps.Service()
	return func(ctx context.Context) []ArgoApp {
		resp := svc.Apps(ctx)
		out := make([]ArgoApp, 0, len(resp.Apps))
		for _, app := range resp.Apps {
			out = append(out, ArgoApp{Name: app.Name, SyncStatus: app.SyncStatus, HealthStatus: app.HealthStatus})
		}
		return out
	}
}

// BindReleaseTable has the release records written to and read from the
// agentrt table on pool (releasetable), as server's agentrt start did.
func BindReleaseTable(svc *releases.Service, pool *pgxpool.Pool) {
	svc.SetTable(releasetable.Writer(pool))
	svc.SetTableReader(releasetable.Rows{Pool: pool})
}

// HandleSyncApp and HandleRollbackApp are Argo CD's sync and rollback, the
// catalogued actions' executors. A nil Built is safe to take them from (the
// executors are bound before anything is served).
func (b *Built) HandleSyncApp(w http.ResponseWriter, r *http.Request) { b.GitOps.HandleSyncApp(w, r) }

// HandleRollbackApp: see HandleSyncApp.
func (b *Built) HandleRollbackApp(w http.ResponseWriter, r *http.Request) {
	b.GitOps.HandleRollbackApp(w, r)
}
