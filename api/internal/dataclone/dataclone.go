// Package dataclone is the tenant's data clone, data freshness and data
// probe: it copies bifrost_prod into bifrost_dev / bifrost_stg on the CNPG
// primary, says how far each database lags the clone source, and reads what
// each environment's application publishes about its own data
// (GET <env gateway>/api/monitor/ops/data-probe).
//
// Moved out of api/internal/cluster on 2026-11-17 unchanged (routes, JSON,
// SQL, audit records, action names, the scheduler's timing). It runs its
// commands in the PostgreSQL pod through Postgres, which cluster's Kubernetes
// adapter implements and the server binds: this package does not import
// cluster, which the ownership table records as several domains' package.
package dataclone

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// Postgres is what the data clone needs of the cluster: the CNPG primary's
// pod name and a command run in its postgres container. *cluster.Service
// implements it (ResolveCNPGPrimary, ExecOnPrimary); the server passes it to
// NewHandler.
type Postgres interface {
	ResolveCNPGPrimary(ctx context.Context) (string, error)
	ExecOnPrimary(ctx context.Context, primary string, command ...string) (string, error)
}

// errNoPostgres is the answer of every command when no Postgres was given:
// nothing is run.
var errNoPostgres = errors.New("data clone: no PostgreSQL adapter bound (dataclone.NewHandler)")

type Service struct {
	entry      *config.ClusterEntry
	pg         Postgres
	cloneJobs  *DataCloneJobStore
	cloneSched *DataCloneScheduleStore
	cloneLast  *DataCloneLastStore

	cloneFinished func(ctx context.Context, jobID string, ok bool)
}

// NewService reads the environments' gateways from entry and runs its
// commands through pg.
func NewService(entry *config.ClusterEntry, pg Postgres) *Service {
	return &Service{entry: entry, pg: pg}
}

func (s *Service) resolveCNPGPrimary(ctx context.Context) (string, error) {
	if s == nil || s.pg == nil {
		return "", errNoPostgres
	}
	return s.pg.ResolveCNPGPrimary(ctx)
}

func (s *Service) execOnPrimary(ctx context.Context, primary string, command ...string) (string, error) {
	if s == nil || s.pg == nil {
		return "", errNoPostgres
	}
	return s.pg.ExecOnPrimary(ctx, primary, command...)
}

// baseMeta is the part of cluster's baseMeta the freshness answer carries.
type baseMeta struct {
	ClusterID string
}

func (s *Service) baseMeta(_ time.Time) baseMeta {
	if s.entry == nil {
		return baseMeta{ClusterID: "unknown"}
	}
	return baseMeta{ClusterID: s.entry.ID}
}

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

// NewHandler serves the data clone and freshness routes for the cluster entry
// whose gateways the data probes use, recording its writes in audit.
func NewHandler(entry *config.ClusterEntry, audit *actuation.AuditLog, pg Postgres) *Handler {
	return &Handler{svc: NewService(entry, pg), audit: audit}
}

func (h *Handler) Service() *Service {
	return h.svc
}

func (h *Handler) recordAudit(r *http.Request, action, target, status, detail string) {
	if h.audit != nil {
		h.audit.Record(r, action, target, status, detail)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
