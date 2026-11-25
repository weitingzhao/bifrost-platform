package unitwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// MainEvidence keeps main's unit evidence current: when main moves, the units
// of its head that the ledger has no verdict on are started (tier "merge"), as
// POST /units/evidence would for an agent asking. Before it, main's evidence
// came from the daily run and from agents asking by hand, and main moves a
// dozen times a day (Foundation's reading of unit evidence, 73/165 tasks of a
// head, 2026-11-25).
//
// A head is advanced once; the receiving pass (unitexec.ReceiveUnits) takes it
// further while its runs come and go, starting what was put off for want of
// room. A head none of whose runs could start has no run to be continued by:
// the pass answers an error, so the watch tries again after its retry.
type MainEvidence struct {
	Head func(ctx context.Context, repo string) (string, error)
	// Advance starts the commit's units without a verdict.
	Advance func(ctx context.Context, repo, commit string) (unitexec.Advanced, error)

	mu   sync.Mutex
	done string // the head advanced last, with something started or nothing to start
}

// ErrAllDeferred is a head none of whose units could start yet.
var ErrAllDeferred = errors.New("main's units were all put off for want of room")

// Pass advances main's head once. An error leaves the head to the next pass.
func (m *MainEvidence) Pass(ctx context.Context) error {
	if m == nil || m.Head == nil || m.Advance == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	head, err := m.Head(ctx, watchedRepo)
	if err != nil {
		return fmt.Errorf("main evidence: main: %w", err)
	}
	if head == "" || head == m.done {
		return nil
	}
	out, err := m.Advance(ctx, watchedRepo, head)
	if err != nil {
		return fmt.Errorf("main evidence of %s: %w", head, err)
	}
	if len(out.Created) == 0 && len(out.Retried) == 0 && out.Present == 0 && out.Deferred > 0 {
		return fmt.Errorf("main evidence of %s: %w (%d)", head, ErrAllDeferred, out.Deferred)
	}
	m.done = head
	if len(out.Created) > 0 || out.Deferred > 0 {
		slog.Info("units: main evidence", "commit", head, "to_run", out.ToRun, "created", len(out.Created), "deferred", out.Deferred)
	}
	return nil
}

// MainEvidenceSource is MainEvidence on the daily run's dependencies: nothing
// while the ledger is away or dispatch is off; no node for the units is a
// wait, answered as an error so the watch tries again.
func MainEvidenceSource(deps DailyDeps) func(context.Context) error {
	m := &MainEvidence{Head: deps.Head, Advance: func(ctx context.Context, repo, commit string) (unitexec.Advanced, error) {
		cfg, err := deps.Placed(ctx)
		if err != nil {
			return unitexec.Advanced{}, err
		}
		return unitexec.Advance(ctx, deps.DB(), deps.Trees(), deps.Cluster(), cfg, repo, commit, "merge")
	}}
	return func(ctx context.Context) error {
		if deps.DB() == nil || unitexec.ConfigFromEnv(deps.Namespace()).Mode == "off" {
			return nil
		}
		return m.Pass(ctx)
	}
}
