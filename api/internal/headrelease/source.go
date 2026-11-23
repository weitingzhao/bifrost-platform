package headrelease

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
)

// Hands are what the head-release loop reaches outside delivery, lent by the
// platform's assembly. The loop does its own work with them: it builds the
// PROD shadow, records each decision with the commits it brings, and releases
// PROD under its own principal. Until 2026-11-23 the server did that work
// itself (server/head_release.go, prod_batch_shadow.go, prod_batch_release.go)
// and so used this package's insides (E5: assembly reaches a domain only
// through what it offers); now the server fills Hands and nothing else.
type Hands struct {
	// WorldOf builds the world of one pass, or nil (PassDeps.WorldOf).
	WorldOf func() *Live
	// Git reaches the repositories, for the commits a decision brings.
	Git func(context.Context) (gitread.Access, error)
	// Audit writes one audit record, as the operator role.
	Audit func(principal, action, target, result, detail string)
	// Trace writes one system action to the action trace.
	Trace func(ctx context.Context, action, mode, principal, why string, params map[string]any, decision, result any)
	// Start starts a delivery as principal if no other holds the way: the
	// run's name, or who holds it.
	Start func(ctx context.Context, principal, pipeline, revision string, params map[string]string) (run, heldBy string, err error)
	// ShadowReads supplies the PROD shadow's reads (ProdShadowHands.Reads).
	ShadowReads func() ProdReads
	// FullCI starts the full CI of a revision (ProdShadowHands.FullCI).
	FullCI func(ctx context.Context, revision string) (string, error)
	// DB is the ledger, or nil; the shadow keeps its reasons there.
	DB func() eventlog.DB
	// Zone is the PROD release hours' zone; nil sets no hours.
	Zone *time.Location
}

var (
	errNoFullCI = errors.New("the daily full CI is not wired in this process")
	errNoStart  = errors.New("head release: no delivery to start in this process")
)

// Enabled says whether the loop runs in any mode: head release on, or the
// PROD shadow (shadow or on). Its wake is consumed only then.
func Enabled() bool { return On() || ProdShadowOn() }

// Source is the head-release loop's pass, or nil: neither mode is on, or the
// table does not load (nothing is released, and that is logged).
func Source(h Hands) func(context.Context) error {
	if !Enabled() {
		return nil
	}
	table, path, err := LoadTable()
	if err != nil {
		slog.Error("head release: the table does not load; nothing is released", "path", path, "error", err)
		return nil
	}
	releasing := On()
	shadow := h.shadow(table)
	slog.Info("head release: on", "path", path, "deliverables", len(table.Deliverables), "releasing", releasing, "prod_shadow", shadow != nil)
	return Pass(PassDeps{Table: table, Releasing: releasing, WorldOf: h.WorldOf, Shadow: shadow, Record: h.Record})
}

// Record says an outcome where it outlives the process: the log, the audit
// log and the action trace, with the commits the release brings.
func (h Hands) Record(ctx context.Context, d Deliverable, out Outcome) {
	rec := RecordOf(ctx, d, out, h.brought)
	detail, err := json.Marshal(rec)
	if err != nil {
		detail = []byte(out.Action + ": " + out.Why)
	}
	slog.Info("head release", "deliverable", out.Deliverable, "action", out.Action, "why", out.Why, "run", out.Run, "record", string(detail))
	if h.Audit != nil {
		h.Audit(Principal, "head_release", "deliverable/"+out.Deliverable, out.Action, string(detail))
	}
	if h.Trace != nil {
		h.Trace(ctx, "start_pipeline_run", "system_head", Principal, out.Why,
			map[string]any{"deliverable": out.Deliverable, "commits": out.Commits}, rec, map[string]any{"action": out.Action, "run": out.Run})
	}
}

func (h Hands) brought(ctx context.Context, repo, from, to string) ([]BroughtCommit, int, string) {
	return Brought(ctx, h.Git, repo, from, to)
}

// shadow builds the PROD shadow from the hands, or nil when it is off.
func (h Hands) shadow(table Table) *ProdShadowLoop {
	if h.ShadowReads == nil {
		return nil
	}
	return NewProdShadow(table, ProdShadowHands{
		Reads: h.ShadowReads,
		Batch: func(ctx context.Context, repo, from, to string) ProdBatch {
			return ProdBatchFrom(h.brought(ctx, repo, from, to))
		},
		Audit: func(target, result, detail string) {
			if h.Audit != nil {
				h.Audit(ProdShadowPrincipal, ProdRecordAction, target, result, detail)
			}
			// The ledger keeps the reasons the audit event does not (migration 0043).
			if h.DB != nil {
				RecordProdJudged(h.DB(), result, detail)
			}
		},
		Release: h.prodRelease,
		FullCI: func(ctx context.Context, revision string) (string, error) {
			if h.FullCI == nil {
				return "", errNoFullCI
			}
			return h.FullCI(ctx, revision)
		},
		Zone: h.Zone,
	})
}

// prodRelease is mode on's one act: start the PROD delivery as the PROD
// release principal; a way held by another is a wait, not an error.
func (h Hands) prodRelease(ctx context.Context, pipeline, revision string, params map[string]string) (string, string, error) {
	if h.Start == nil {
		return "", "", errNoStart
	}
	run, heldBy, err := h.Start(ctx, ProdReleasePrincipal, pipeline, revision, params)
	if err != nil {
		return "", "", err
	}
	if heldBy != "" {
		return "", "not started: " + heldBy, nil
	}
	return run, "", nil
}
