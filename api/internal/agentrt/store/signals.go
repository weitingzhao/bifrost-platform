package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Outbox topics. A mailbox topic is "mbox:" + the receiving actor.
const (
	TopicWake     = "wake"
	TopicStop     = "stop"
	TopicCooldown = "cooldown"
	TopicEvents   = "events"
)

// MailboxTopic is the outbox topic of an actor's mailbox.
func MailboxTopic(actor string) string { return "mbox:" + actor }

// Enqueue writes an outbox row for an event of this transaction; the relay
// turns it into a Redis signal after commit.
func Enqueue(ctx context.Context, tx pgx.Tx, eventID int64, topic string, payload map[string]any) error {
	b, err := canon.Encode(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.outbox (event_id, topic, payload_json) VALUES ($1,$2,$3::jsonb)`,
		eventID, topic, string(b))
	return err
}

// MarkReady moves a task to ready (one of the controller's moves) and wakes
// the hosts.
func MarkReady(ctx context.Context, tx pgx.Tx, actor, taskID string) error {
	return MoveTask(ctx, tx, actor, taskID, "ready", "")
}

// PostMessage stores a message (its body as an artifact, data only) and
// signals the receiver's mailbox.
func PostMessage(ctx context.Context, tx pgx.Tx, from, to, kind string, body []byte) (string, error) {
	art, err := PutArtifact(ctx, tx, from, "message", "text/plain; charset=utf-8", body)
	if err != nil {
		return "", err
	}
	id := NewMessageID()
	ev, err := Append(ctx, tx, Event{Stream: "mbox:" + to, Type: "message_posted", Actor: from,
		Body: map[string]any{"message_id": id, "to": to, "kind": kind}})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.message (message_id, from_actor_id, to_actor_id, message_kind, body_artifact_id, event_id)
	  VALUES ($1,$2,$3,$4,$5,$6)`, id, from, to, kind, art, ev.EventID); err != nil {
		return "", err
	}
	return id, Enqueue(ctx, tx, ev.EventID, MailboxTopic(to),
		map[string]any{"message_id": id, "event_id": ev.EventID, "message_kind": kind})
}

// AckMessage records that the receiver handled a message. The ack lives in
// PostgreSQL only; Redis is never the record of it.
func AckMessage(ctx context.Context, tx pgx.Tx, actor, messageID string) error {
	var to string
	if err := tx.QueryRow(ctx, `SELECT to_actor_id FROM agentrt.message WHERE message_id = $1 FOR UPDATE`, messageID).Scan(&to); err != nil {
		return err
	}
	if _, err := Append(ctx, tx, Event{Stream: "mbox:" + to, Type: "message_acked", Actor: actor,
		Body: map[string]any{"message_id": messageID}}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.message SET acked_at = clock_timestamp() WHERE message_id = $1 AND acked_at IS NULL`, messageID)
	return err
}

// SetControl records a stop, pause or drain and broadcasts it. It first takes
// the lock a claim takes for the same scope (the global advisory lock, the
// mission, task or host row), so a claim either commits before the control
// or sees it (review 148 A2).
func SetControl(ctx context.Context, tx pgx.Tx, actor, controlID, scope, scopeRef, kind, reason string) error {
	lock := map[string]string{
		"global":  `SELECT pg_advisory_xact_lock(742105) WHERE $1::text IS NOT NULL`,
		"mission": `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`,
		"task":    `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR UPDATE`,
		"host":    `SELECT 1 FROM agentrt.host WHERE host_id = $1 FOR NO KEY UPDATE`,
	}[scope]
	if lock != "" {
		if _, err := tx.Exec(ctx, lock, scopeRef); err != nil {
			return err
		}
	}
	body := map[string]any{"control_id": controlID, "scope": scope, "kind": kind, "reason": reason}
	if scopeRef != "" {
		body["scope_ref"] = scopeRef
	}
	ev, err := Append(ctx, tx, Event{Stream: "control", Type: "control_set", Actor: actor, Body: body})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.control (control_id, scope, scope_ref, control_kind, reason, set_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5,$6)`, controlID, scope, nullable(scopeRef), kind, reason, actor); err != nil {
		return err
	}
	return Enqueue(ctx, tx, ev.EventID, TopicStop, map[string]any{
		"control_id": controlID, "scope": scope, "scope_ref": nullable(scopeRef), "control_kind": kind})
}

// ClearControl lifts a control. Who may clear is the kernel's rule (2.2);
// the store only records it.
func ClearControl(ctx context.Context, tx pgx.Tx, actor, controlID string) error {
	if _, err := Append(ctx, tx, Event{Stream: "control", Type: "control_cleared", Actor: actor,
		Body: map[string]any{"control_id": controlID}}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.control SET cleared_at = clock_timestamp(), cleared_by_actor_id = $2
	  WHERE control_id = $1 AND cleared_at IS NULL`, controlID, actor)
	return err
}

// SetCooldown records that a vendor pool is cooling down until a time, and
// creates the pool row on first use.
func SetCooldown(ctx context.Context, tx pgx.Tx, actor, poolID, tool, window string, until time.Time, reason string) error {
	until = until.UTC().Truncate(time.Microsecond)
	ev, err := Append(ctx, tx, Event{Stream: "pool:" + poolID, Type: "pool_cooldown_set", Actor: actor,
		Body: map[string]any{"vendor_pool_id": poolID, "until": until.Format(timeLayout), "reason": reason}})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.vendor_pool (vendor_pool_id, tool, window_kind, cooldown_until)
	  VALUES ($1,$2,$3,$4)
	  ON CONFLICT (vendor_pool_id) DO UPDATE SET cooldown_until = EXCLUDED.cooldown_until, version = agentrt.vendor_pool.version + 1`,
		poolID, tool, window, until); err != nil {
		return err
	}
	return Enqueue(ctx, tx, ev.EventID, TopicCooldown, map[string]any{
		"vendor_pool_id": poolID, "until": until.Format(timeLayout), "reason": reason, "event_id": ev.EventID})
}
