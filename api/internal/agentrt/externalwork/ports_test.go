package externalwork

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// fakeTree is a plan tree whose every port is set; its ViewTree names it.
func fakeTree() *PlanTreePorts {
	return &PlanTreePorts{
		LockTree: func(context.Context, pgx.Tx, string) error { return nil },
		RootOf:   func(context.Context, pgx.Tx, string) (string, error) { return "R", nil },
		DevTree:  func(context.Context, pgx.Tx, string) error { return nil },
		ViewTree: func() string { return "fake tree " },
	}
}

// Origin: 2026-11-17, plan core wall step 6 (design duty/from-biz-2026-11-17-plan-core-wall.md): the writes of external work moved out of store and reach the plan tree through ports; a port nobody bound, or bound in part, refuses the write with ErrPortUnbound instead of writing as if the tree were locked. The whole fake is the positive control.
func TestUnboundPlanTreeRefuses(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*PlanTreePorts{
		"nil": nil, "empty": {},
		"no lock":      {RootOf: fakeTree().RootOf, DevTree: fakeTree().DevTree, ViewTree: fakeTree().ViewTree},
		"no root":      {LockTree: fakeTree().LockTree, DevTree: fakeTree().DevTree, ViewTree: fakeTree().ViewTree},
		"no dev check": {LockTree: fakeTree().LockTree, RootOf: fakeTree().RootOf, ViewTree: fakeTree().ViewTree},
		"no view tree": {LockTree: fakeTree().LockTree, RootOf: fakeTree().RootOf, DevTree: fakeTree().DevTree},
	} {
		if got, err := planTreeVia(p); got != nil || !errors.Is(err, ErrPortUnbound) {
			t.Fatalf("%s: %v, %v; want ErrPortUnbound", name, got, err)
		}
	}
	whole := fakeTree()
	if got, err := planTreeVia(whole); err != nil || got != whole {
		t.Fatalf("a whole plan tree is refused: %v, %v", got, err)
	}
}

// Origin: 2026-11-17, plan core wall step 6 (as above): in a binary where nothing binds the plan tree (this package's tests; store binds it in production), both writes refuse before they read the request or touch the database (a nil one here); once a plan tree is bound, the same calls get past the port and are refused for their request instead, and the bound ports are the ones given (through a fake: this package's tests may not import store or planstore).
func TestWritesRefuseUntilThePlanTreeIsBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := actor.Writer{Actor: "thread:s1"}
	if _, err := RegisterExternalWork(ctx, nil, w, ExternalRegistration{}); !errors.Is(err, ErrPortUnbound) {
		t.Fatalf("register with nothing bound: %v", err)
	}
	if _, err := PostExternalEvent(ctx, nil, w, "ew_x", ExternalEvent{}); !errors.Is(err, ErrPortUnbound) {
		t.Fatalf("post with nothing bound: %v", err)
	}
	BindPlanTree(*fakeTree())
	defer planTreePorts.Store(nil)
	if p, err := planTree(); err != nil || p.ViewTree() != "fake tree " {
		t.Fatalf("the bound plan tree: %v, %v", p, err)
	}
	if _, err := RegisterExternalWork(ctx, nil, w, ExternalRegistration{}); !errors.Is(err, eventlog.ErrInvalid) || errors.Is(err, ErrPortUnbound) {
		t.Fatalf("register with the tree bound: %v; want the request refused", err)
	}
	if _, err := PostExternalEvent(ctx, nil, w, "ew_x", ExternalEvent{}); !errors.Is(err, eventlog.ErrInvalid) || errors.Is(err, ErrPortUnbound) {
		t.Fatalf("post with the tree bound: %v; want the request refused", err)
	}
}
