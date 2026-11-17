package externalwork

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
)

// The writes' ports: what registering a work and posting its events need of
// the plan tree (the plan domain's), declared here as small function types
// over pgx and plain values. The plan domain's package (planstore) provides
// them; store, which may import both, binds them at start (store's
// portsbind.go). This package names the port and not the plan's package,
// which is what keeps it a package of the lineage domain alone.
//
// A port nobody bound refuses: the write gets ErrPortUnbound and nothing is
// written. It never answers as if the tree were unlocked or the node bound.

// ErrPortUnbound: a write of external work needed the plan tree and nothing
// was bound to provide it, so the write is refused.
var ErrPortUnbound = errors.New("store: the plan tree port of the external work's writes is not bound")

// PlanTreePorts is what the writes need of the plan tree.
type PlanTreePorts struct {
	// LockTree takes the plan tree's lock of root for the transaction.
	LockTree func(ctx context.Context, tx pgx.Tx, root string) error
	// RootOf is the root of the tree node belongs to.
	RootOf func(ctx context.Context, tx pgx.Tx, node string) (string, error)
	// DevTree refuses a root whose tree binds a mission of a tenant other
	// than dev.
	DevTree func(ctx context.Context, tx pgx.Tx, root string) error
	// ViewTree is the recursive CTE "tree" of every node under the root $1,
	// which a query of the writes starts with.
	ViewTree func() string
}

var planTreePorts atomic.Pointer[PlanTreePorts]

// BindPlanTree binds the plan tree's side. It is called once, from the init
// that wires the providers (store's portsbind.go).
func BindPlanTree(p PlanTreePorts) { planTreePorts.Store(&p) }

// planTree is the bound plan tree, or the refusal of a write when it is not
// bound whole.
func planTree() (*PlanTreePorts, error) { return planTreeVia(planTreePorts.Load()) }

func planTreeVia(p *PlanTreePorts) (*PlanTreePorts, error) {
	if p == nil || p.LockTree == nil || p.RootOf == nil || p.DevTree == nil || p.ViewTree == nil {
		return nil, fmt.Errorf("%w: the plan tree (BindPlanTree was not called); the write is refused", ErrPortUnbound)
	}
	return p, nil
}
