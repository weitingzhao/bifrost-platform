package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type seedActor struct {
	id, kind, tool, name string
}

var seedActors = []seedActor{
	{"controller", "controller", "", "Runtime controller"},
	{"owner", "owner", "", "Owner"},
	{"exec:claude", "executor", "claude", "Claude Code"},
	{"exec:codex", "executor", "codex", "Codex CLI"},
	{"exec:cursor", "executor", "cursor", "Cursor CLI"},
	{"exec:deterministic", "executor", "deterministic", "Deterministic checks"},
	{"seat:chief_of_staff", "seat", "", "Chief of staff seat"},
}

// Seed writes the first event and the rows that hang on it, in one
// transaction, without disabling any trigger (review 142 R1): the
// actor_registered event names actor controller before that row exists; the
// deferred foreign key checks it at commit. It does nothing on a database
// that already has the controller.
func Seed(ctx context.Context, conn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}) (bool, error) {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Two migrators starting on an empty database: the second waits here and
	// then sees the controller (review 144 B5).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7421000002)`); err != nil {
		return false, err
	}
	if err := LockCapabilities(ctx, tx); err != nil {
		return false, err
	}
	var have bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.actor WHERE actor_id = 'controller')`).Scan(&have); err != nil {
		return false, err
	}
	if have {
		return false, nil
	}
	ids := make([]any, 0, len(seedActors))
	for _, a := range seedActors {
		ids = append(ids, a.id)
	}
	if _, err := Append(ctx, tx, Event{
		Stream: "system",
		Type:   "actor_registered",
		Actor:  "controller",
		Body:   map[string]any{"actors": ids, "note": "seed"},
	}); err != nil {
		return false, err
	}
	for _, a := range seedActors {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, tool, display_name) VALUES ($1,$2,$3,$4)`,
			a.id, a.kind, nullable(a.tool), a.name); err != nil {
			return false, fmt.Errorf("store: seed actor %s: %w", a.id, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
