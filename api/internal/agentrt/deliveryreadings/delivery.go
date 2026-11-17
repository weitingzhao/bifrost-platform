package deliveryreadings

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// ReadDelivery: one read of a commit's delivery stages. The commit is one an
// external work named (a heartbeat or its submitted event); its stages come
// from the recorded pipeline runs of that exact commit. Reads by task, and of
// commits only a landing operation of the retired dispatch lane named, ended
// with that lane (LANE-BIZ-ALIGN, 2026-11-13).
//
// Which work named the commit is the lineage domain's to say: this package
// declares the CommitNaming port and the wiring binds it to
// externalwork.NamingOfCommit (moved here from the store, 2026-11-17).

// DeliveryQuery names a change by Commit, a 7 to 40 character lowercase hex
// prefix.
type DeliveryQuery struct {
	Commit string
	// Production is supplied by the server's trusted release reader, never URL input.
	Production *deliveryview.DeliveryProduction
}

// CommitNaming is the external work that named a commit, as the lineage
// domain reports it (externalwork.CommitNamed).
type CommitNaming struct {
	Work, Registrant, Commit string
	At                       time.Time
	Task, Node, Repo         string
	RepoUnread               bool
}

// LineagePorts are what this read needs from the lineage domain. The wiring
// binds them; unbound, ReadDelivery fails with ErrPortUnbound before it reads.
type LineagePorts struct {
	// CommitNaming finds the work naming a commit starting with prefix, in the
	// caller's transaction: pgx.ErrNoRows for none, eventlog.ErrInvalid for an
	// ambiguous prefix.
	CommitNaming func(ctx context.Context, tx pgx.Tx, prefix string) (CommitNaming, error)
}

var lineage atomic.Pointer[LineagePorts]

// BindLineage binds the lineage ports of ReadDelivery.
func BindLineage(p LineagePorts) { lineage.Store(&p) }

// ErrPortUnbound: ReadDelivery was called before the wiring bound its ports.
var ErrPortUnbound = errors.New("deliveryreadings: the lineage port of the delivery read is not bound")

// DB is the transaction source ReadDelivery reads in.
type DB interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// deliveryRepo is the repository whose PROD release q.Production describes.
const deliveryRepo = "bifrost-platform"

var deliveryCommitPrefix = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// ReadDelivery reads the delivery stages of q's commit. pgx.ErrNoRows: no
// external work names it.
func ReadDelivery(ctx context.Context, db DB, q DeliveryQuery) (map[string]any, error) {
	if !deliveryCommitPrefix.MatchString(q.Commit) {
		return nil, fmt.Errorf("%w: commit must be 7 to 40 lowercase hex characters", eventlog.ErrInvalid)
	}
	ports := lineage.Load()
	if ports == nil || ports.CommitNaming == nil {
		return nil, ErrPortUnbound
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	m, err := ports.CommitNaming(ctx, tx, q.Commit)
	if err != nil {
		return nil, err
	}
	warnings := []string{"stages are matched by this exact commit; a later revision that carries it is not recognised"}
	if m.RepoUnread {
		warnings = append(warnings, "the work's description could not be read: its repository is unknown")
	}
	in := ExternalDelivery{Work: m.Work, Registrant: m.Registrant, Repo: m.Repo, Commit: m.Commit, NamedAt: m.At}
	if in.Runs, err = Runs(ctx, tx, m.Commit); err != nil {
		return nil, err
	}
	// A recorded run names the repository by its label; the description's is
	// free text.
	for _, r := range in.Runs {
		if r.Repo != "" {
			in.Repo = r.Repo
			break
		}
	}
	if in.Repo == deliveryRepo {
		in.Production = q.Production
	}
	out := map[string]any{"schema": "agentrt.delivery_response/v1", "commit": m.Commit, "task_id": m.Task, "attempt_id": "",
		"plan_node_id": m.Node, "repo": in.Repo, "operation": "", "external_work_id": m.Work,
		"stages": FoldExternalDelivery(in), "warnings": warnings, "as_of": now.UTC().Truncate(time.Microsecond).Format(eventlog.TimeLayout)}
	return out, tx.Commit(ctx)
}

// ExternalDelivery is everything FoldExternalDelivery reads.
type ExternalDelivery struct {
	// Work is the external work, Registrant who registered it, Repo the
	// repository its description names.
	Work, Registrant, Repo, Commit string
	// NamedAt is when the work's earliest event naming Commit was recorded:
	// a heartbeat, or the submitted event.
	NamedAt time.Time
	// Runs are the recorded pipeline runs: nil when they were not read, an
	// empty non-nil slice when they were read and hold none.
	Runs       []Run
	Production *deliveryview.DeliveryProduction
}

// FoldExternalDelivery adapts recorded runs to the pure delivery view.
func FoldExternalDelivery(in ExternalDelivery) []deliveryview.DeliveryStage {
	var runs []deliveryview.Run
	// nil means unread; a non-nil empty slice means read and no runs found.
	if in.Runs != nil {
		runs = make([]deliveryview.Run, len(in.Runs))
		for i, r := range in.Runs {
			runs[i] = deliveryview.Run{Run: r.Run, Pipeline: r.Pipeline, Kind: r.Kind, Result: r.Result, Revision: r.Revision,
				CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
		}
	}
	return deliveryview.FoldExternalDelivery(deliveryview.ExternalDelivery{
		Work: in.Work, Registrant: in.Registrant, Repo: in.Repo, Commit: in.Commit, NamedAt: in.NamedAt,
		Runs: runs, Production: in.Production,
	})
}
