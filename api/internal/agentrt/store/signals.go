package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Outbox topics and Enqueue are the event log's (eventlog/outbox.go).
const (
	TopicWake     = eventlog.TopicWake
	TopicStop     = eventlog.TopicStop
	TopicCooldown = eventlog.TopicCooldown
	TopicEvents   = eventlog.TopicEvents
)

// Enqueue writes an outbox row for an event of this transaction.
func Enqueue(ctx context.Context, tx pgx.Tx, eventID int64, topic string, payload map[string]any) error {
	return eventlog.Enqueue(ctx, tx, eventID, topic, payload)
}

// SetCooldown records that a vendor pool is cooling down until a time, and
// creates the pool row on first use.
func SetCooldown(ctx context.Context, tx pgx.Tx, actor, poolID, tool, window string, until time.Time, reason string) error {
	if err := lockPools(ctx, tx, poolID); err != nil {
		return err
	}
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
