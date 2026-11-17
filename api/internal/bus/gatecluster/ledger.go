package gatecluster

import (
	"context"
	"errors"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Actor is who writes gate-cluster's events: the gate, a service of the platform.
const Actor = "service:gate-cluster"

// RunStream is the ledger stream of one PipelineRun (RULES.md §7: a stream
// per key; runs are not ordered, so each run is its own stream).
func RunStream(run string) string { return "ci:" + run }

// WindowStream is the ledger stream of the release window.
const WindowStream = "window:" + WindowKey

// Decide is asked, under the stream's lock, with the type and body of the
// stream's last event ("" and nil when the stream is empty): the body to
// append, or false to write nothing (the ledger already says it).
type Decide func(lastType string, lastBody map[string]any) (map[string]any, bool)

// Ledger is where the gate writes. PGLedger is the event log; the tests hand
// in a memory one.
type Ledger interface {
	Append(ctx context.Context, stream, typ string, decide Decide) (bool, error)
}

// PGLedger appends to agentrt's event log. DB is nil while the ledger is not
// connected.
type PGLedger struct {
	DB func() eventlog.DB
}

var errNoLedger = errors.New("bus gate-cluster: the ledger is not connected")

// Append locks the stream's head, reads its last event, and appends what
// decide returns, in one transaction: two writers of one stream take turns,
// and a fact the stream already holds is not written again.
func (l *PGLedger) Append(ctx context.Context, stream, typ string, decide Decide) (bool, error) {
	var db eventlog.DB
	if l.DB != nil {
		db = l.DB()
	}
	if db == nil {
		return false, errNoLedger
	}
	if err := ensureActor(ctx, db); err != nil {
		return false, err
	}
	appended := false
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		appended = false
		if err := eventlog.LockStream(ctx, tx, stream); err != nil {
			return err
		}
		last, _, err := eventlog.Last(ctx, tx, stream)
		if err != nil {
			return err
		}
		body, ok := decide(last.Type, last.Body)
		if !ok {
			return nil
		}
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: typ, Actor: Actor, Body: body}); err != nil {
			return err
		}
		appended = true
		return nil
	})
	return appended, err
}

// ensureActor registers service:gate-cluster once, as gate-git registers
// its own (bus/hook): under the registration lock, with an actor_registered
// event written by the controller.
func ensureActor(ctx context.Context, db eventlog.DB) error {
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		return actor.RegisterController(ctx, tx, Actor, "Bus gate-cluster", "the bus's gate for the cluster, registered at its first write")
	})
}
