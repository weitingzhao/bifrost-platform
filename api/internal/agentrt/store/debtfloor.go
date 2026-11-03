package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The crew's writes run under the writer's key; the numbering floor is
// raised only by the Owner or the runtime, on a stream of its own (§2.4).

// debtOp runs a crew write under the writer's key: the writer checked (a
// thread registered on its first write), then op.
func debtOp(ctx context.Context, db DB, w Writer, route, key string, request map[string]any,
	op func(tx pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	if err := debtWriter(w); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	return idempotentAs(ctx, db, w, route, key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		return op(tx)
	})
}

// RaiseDebtFloor raises the numbering floor (debt_floor_raised): only the
// Owner or the runtime, only upwards (§2.4). The import raises it to the
// largest number found anywhere (§2.4); the next number is above it.
func RaiseDebtFloor(ctx context.Context, db DB, w Writer, key string, floor int) (map[string]any, error) {
	if k := kindOf(w.Actor); k != "owner" && k != "controller" {
		return nil, invalid("%s does not raise the debt floor: the Owner or the runtime does", w.Actor)
	}
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	return idempotentAs(ctx, db, w, "debt-floor", key, map[string]any{"floor": floor}, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, debtNumberLock); err != nil {
			return nil, err
		}
		var was int
		if err := tx.QueryRow(ctx, `SELECT floor FROM agentrt.debt_counter WHERE debt_counter_id = 1 FOR UPDATE`).Scan(&was); err != nil {
			return nil, err
		}
		if floor <= was {
			return nil, fmt.Errorf("%w: the debt floor is %d; it only rises, and %d does not", ErrState, was, floor)
		}
		if _, err := Append(ctx, tx, Event{Stream: DebtCounterStream, Type: "debt_floor_raised", Actor: w.Actor,
			Body: map[string]any{"floor": floor, "from": was}}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.debt_counter SET floor = $1, version = version + 1 WHERE debt_counter_id = 1`, floor); err != nil {
			return nil, err
		}
		return map[string]any{"floor": floor, "from": was}, nil
	})
}
