package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

// PostConclusion is the platform's own conclusion, once per record/state.
// It reuses the idempotency table with a permanent receipt (ordinary request
// keys expire after seven days). Receipt, message, artifact, event and outbox
// signal commit together. There is no HTTP route that accepts this sender.
func PostConclusion(ctx context.Context, db DB, key, to, body string) error {
	if to == "" || threadinbox.Recipient(to) != to {
		return nil
	}
	if !strings.HasPrefix(key, "approval:") && !strings.HasPrefix(key, "question:") {
		return invalid("a conclusion names its approval or question")
	}
	id := "thread-conclusion|" + key
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742103, hashtext($1))`, id); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.idempotency_key WHERE idempotency_key_id = $1)`, id).Scan(&exists); err != nil || exists {
			return err
		}
		kind, err := ActorKind(ctx, tx, to)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && kind != "thread" {
			return nil // never register or revive a recipient on someone else's behalf
		}
		if err != nil {
			return err
		}
		msg, err := PostMessage(ctx, tx, "controller", to, "note", []byte(body))
		if err != nil {
			return err
		}
		response, err := canon.Encode(map[string]any{"message_id": msg})
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(key))
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.idempotency_key
		  (idempotency_key_id, route, request_sha256, response_json, expires_at)
		  VALUES ($1, 'thread-conclusion', $2, $3::jsonb, 'infinity')`, id, hex.EncodeToString(sum[:]), string(response))
		return err
	})
}
