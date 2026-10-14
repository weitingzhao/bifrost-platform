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

type seedFact struct {
	id, executor, kind, subject, scope, sourceKind, source string
}

// The initial capability facts of design v5 §5.1.2. Facts of kind owner take
// effect only once the Owner signs them into the capability-facts rule (2.2);
// until then routing reads them as proposals.
var seedFacts = []seedFact{
	{"codex-sandbox-no-listen", "exec:codex", "cannot", "listen_port_tests", `{}`, "measured",
		"WORK.md W-66, Owner 2026-10-12 13:24; round 1 step 6, second fix"},
	{"codex-sandbox-no-commit", "exec:codex", "cannot", "git_commit", `{}`, "measured",
		"RUNTIME-PLACEMENT-2026-10-11.md section 10 row 1; WORK.md W-66"},
	{"cursor-no-fix-review-findings", "exec:cursor", "must_not", "fix_review_findings", `{}`, "owner",
		"WORK.md W-66, Owner 2026-10-12 13:24"},
	{"core-author-claude", "exec:claude", "only", "author_core", `{"core":true}`, "owner",
		"WORK.md W-31, Owner 2026-10-13 23:16"},
	{"core-review-codex-readonly", "exec:codex", "only", "review_core", `{"core":true,"sandbox":"read-only"}`, "owner",
		"WORK.md W-31, Owner 2026-10-13 23:16"},
	{"review-other-provider", "exec:deterministic", "must_not", "review_own_provider", `{}`, "owner",
		"ADR section 12.2 item 3, section 12.5"},
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
	facts := make([]any, 0, len(seedFacts))
	for _, f := range seedFacts {
		facts = append(facts, f.id)
	}
	if _, err := Append(ctx, tx, Event{
		Stream: "system",
		Type:   "actor_registered",
		Actor:  "controller",
		Body:   map[string]any{"actors": ids, "capability_facts": facts, "note": "seed"},
	}); err != nil {
		return false, err
	}
	for _, a := range seedActors {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, tool, display_name) VALUES ($1,$2,$3,$4)`,
			a.id, a.kind, nullable(a.tool), a.name); err != nil {
			return false, fmt.Errorf("store: seed actor %s: %w", a.id, err)
		}
	}
	for _, f := range seedFacts {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.capability_fact
		  (capability_fact_id, executor_actor_id, fact_kind, subject, scope_json, source_kind, source)
		  VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7)`,
			f.id, f.executor, f.kind, f.subject, f.scope, f.sourceKind, f.source); err != nil {
			return false, fmt.Errorf("store: seed fact %s: %w", f.id, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
