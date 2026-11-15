package eventlog

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Outbox topics. A mailbox topic is "mbox:" + the receiving actor (coord).
const (
	TopicWake     = "wake"
	TopicStop     = "stop"
	TopicCooldown = "cooldown"
	TopicEvents   = "events"
)

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
