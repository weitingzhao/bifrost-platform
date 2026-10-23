package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Idempotency keys: a write's key and request, answered once and the same
// way on every retry (attempt.go and the proposal routes use them).

// idempotent runs op at most once per key (v5 §5.2 「写入时的隔离检查」 ②).
// The key is looked up first: the same request gets the stored answer, with
// no lease check and no pre call, so a retry whose first answer was lost gets
// that answer even after the attempt stopped being live; a different request
// gets ErrConflict. Otherwise pre runs outside any transaction (a read of
// another system), then op runs in a transaction that holds a lock on the key
// and looks it up again, so concurrent retries of one request run one at a
// time and the later ones find the first one's answer. A failed op stores
// nothing.
func idempotent(ctx context.Context, db DB, actor, route, key string, request any,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	sum, err := canon.Sum(request)
	if err != nil {
		return nil, err
	}
	return keyed(ctx, db, actor+"|"+route+"|"+key, route, sum, nil, pre, op)
}

// idempotentAs is idempotent for a writer's request: a thread's first
// answer is given again only to the token its session is held to (TD-370),
// checked in each transaction that reads the key; the write itself checks it
// again (ensureWriter).
func idempotentAs(ctx context.Context, db DB, w Writer, route, key string, request any,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	sum, err := canon.Sum(request)
	if err != nil {
		return nil, err
	}
	return keyed(ctx, db, w.Actor+"|"+route+"|"+key, route, sum, func(tx pgx.Tx) error { return holdsSessionTx(ctx, tx, w) }, pre, op)
}

// idempotentHeld is idempotentAs with one more check, made first in every
// transaction that reads the key, before a stored answer is given again: an
// intake client's scope (its products, the issues it sees; DESIGN-crew-intake
// §2.1: a replay passes the identity checks a write does).
func idempotentHeld(ctx context.Context, db DB, w Writer, route, key string, request any, held func(pgx.Tx) error,
	op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	sum, err := canon.Sum(request)
	if err != nil {
		return nil, err
	}
	return keyed(ctx, db, w.Actor+"|"+route+"|"+key, route, sum, func(tx pgx.Tx) error {
		if err := holdsSessionTx(ctx, tx, w); err != nil {
			return err
		}
		return held(tx)
	}, nil, op)
}

func keyed(ctx context.Context, db DB, id, route, sum string, holds func(pgx.Tx) error,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	lookup := func(tx pgx.Tx) (map[string]any, bool, error) {
		if holds != nil {
			if err := holds(tx); err != nil {
				return nil, false, err
			}
		}
		var storedSum, stored string
		err := tx.QueryRow(ctx, `SELECT request_sha256, response_json::text FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
			id).Scan(&storedSum, &stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if storedSum != sum {
			return nil, true, ErrConflict
		}
		out, err := response([]byte(stored))
		return out, true, err
	}
	var out map[string]any
	var found bool
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, found, err = lookup(tx)
		return err
	}); err != nil || found {
		return out, err
	}
	hook("missed", id)
	if pre != nil {
		if err := pre(ctx); err != nil {
			return nil, err
		}
	}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742103, hashtext($1))`, id); err != nil {
			return err
		}
		var err error
		if out, found, err = lookup(tx); err != nil || found {
			return err
		}
		resp, err := op(tx)
		if err != nil {
			return err
		}
		rb, err := canon.Encode(resp)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.idempotency_key (idempotency_key_id, route, request_sha256, response_json, expires_at)
		  VALUES ($1, $2, $3, $4::jsonb, clock_timestamp() + interval '7 days')`, id, route, sum, string(rb)); err != nil {
			return err
		}
		out, err = response(rb)
		return err
	})
	return out, err
}

// response decodes a stored answer the same way on the first call and on a
// retry, so both return identical values.
func response(b []byte) (map[string]any, error) {
	v, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("store: a stored response is not an object")
	}
	return m, nil
}
