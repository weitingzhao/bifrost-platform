package relay

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

type snapshot struct {
	messages  []snapMessage
	controls  []map[string]any
	cooldowns []snapCooldown
	ready     []map[string]any
	epoch     int64
}

type snapMessage struct {
	to      string
	eventID int64
	payload map[string]any
}

type snapCooldown struct {
	pool  string
	until time.Time
}

// Rebuild rewrites every rebuildable key from PostgreSQL while holding the
// relay lock, which also pauses delivery. Writes made meanwhile wait in the
// outbox and are delivered afterwards; a signal can then exist twice
// (snapshot and outbox) and readers de-duplicate. Readers seeing
// meta:rebuilding go straight to PostgreSQL.
func (l *Lease) Rebuild(ctx context.Context) (int64, error) {
	if err := l.do(ctx, "SET", l.r.key("meta:rebuilding"), "1"); err != nil {
		return 0, err
	}
	snap, err := l.r.read(ctx)
	if err != nil {
		return 0, err
	}
	if l.r.afterSnapshot != nil {
		l.r.afterSnapshot()
	}
	if err := l.clearMailboxes(ctx); err != nil {
		return 0, err
	}
	for _, k := range []string{"wake", "stop"} {
		if err := l.do(ctx, "DEL", l.r.key(k)); err != nil {
			return 0, err
		}
	}
	for _, m := range snap.messages {
		stream := l.r.key("mbox:" + m.to)
		p, err := canon.Encode(m.payload)
		if err != nil {
			return 0, err
		}
		if err := l.xadd(ctx, stream, maxMbox, "event_id", strconv.FormatInt(m.eventID, 10), "p", string(p)); err != nil {
			return 0, err
		}
	}
	for _, m := range snap.messages {
		if err := l.do(ctx, "GROUP", l.r.key("mbox:"+m.to), group); err != nil {
			return 0, err
		}
	}
	for _, c := range snap.controls {
		p, err := canon.Encode(c)
		if err != nil {
			return 0, err
		}
		if err := l.xadd(ctx, l.r.key("stop"), maxStop, "event_id", "0", "p", string(p)); err != nil {
			return 0, err
		}
	}
	for _, c := range snap.cooldowns {
		left := time.Until(c.until)
		if left <= 0 {
			continue
		}
		p, err := canon.Encode(map[string]any{"vendor_pool_id": c.pool, "until": c.until.UTC().Format("2006-01-02T15:04:05.000000Z")})
		if err != nil {
			return 0, err
		}
		if err := l.do(ctx, "SETPX", l.r.key("cooldown:"+c.pool), string(p), left.Milliseconds()); err != nil {
			return 0, err
		}
	}
	for _, t := range snap.ready {
		p, err := canon.Encode(t)
		if err != nil {
			return 0, err
		}
		if err := l.xadd(ctx, l.r.key("wake"), maxWake, "event_id", "0", "p", string(p)); err != nil {
			return 0, err
		}
	}
	if err := l.do(ctx, "SET", l.r.key("meta:epoch"), strconv.FormatInt(snap.epoch, 10)); err != nil {
		return 0, err
	}
	if err := l.do(ctx, "DEL", l.r.key("meta:rebuilding")); err != nil {
		return 0, err
	}
	return snap.epoch, nil
}

// clearMailboxes deletes every mailbox stream under this prefix, so a
// mailbox whose messages were all acknowledged does not keep stale signals.
func (l *Lease) clearMailboxes(ctx context.Context) error {
	iter := l.r.Redis.Scan(ctx, 0, l.r.key("mbox:*"), 200).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	for _, k := range keys {
		if err := l.do(ctx, "DEL", k); err != nil {
			return err
		}
	}
	return nil
}

// read takes one consistent snapshot of the rebuild sources.
func (r *Relay) read(ctx context.Context) (snapshot, error) {
	var s snapshot
	if err := r.PG.QueryRow(ctx, `SELECT nextval('agentrt.redis_epoch_seq')`).Scan(&s.epoch); err != nil {
		return s, err
	}
	tx, err := r.PG.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return s, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	rows, err := tx.Query(ctx, `SELECT to_actor_id, event_id, message_id, message_kind FROM agentrt.message
	  WHERE acked_at IS NULL ORDER BY created_at, message_id`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var m snapMessage
		var id, kind string
		if err = rows.Scan(&m.to, &m.eventID, &id, &kind); err != nil {
			rows.Close()
			return s, err
		}
		m.payload = map[string]any{"message_id": id, "event_id": m.eventID, "message_kind": kind}
		s.messages = append(s.messages, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return s, err
	}

	rows, err = tx.Query(ctx, `SELECT control_id, scope, scope_ref, control_kind FROM agentrt.control
	  WHERE cleared_at IS NULL ORDER BY set_at, control_id`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var id, scope, kind string
		var ref *string
		if err = rows.Scan(&id, &scope, &ref, &kind); err != nil {
			rows.Close()
			return s, err
		}
		var refv any
		if ref != nil {
			refv = *ref
		}
		s.controls = append(s.controls, map[string]any{"control_id": id, "scope": scope, "scope_ref": refv, "control_kind": kind})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return s, err
	}

	rows, err = tx.Query(ctx, `SELECT vendor_pool_id, cooldown_until FROM agentrt.vendor_pool
	  WHERE cooldown_until > now() ORDER BY vendor_pool_id`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var c snapCooldown
		if err = rows.Scan(&c.pool, &c.until); err != nil {
			rows.Close()
			return s, err
		}
		s.cooldowns = append(s.cooldowns, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return s, err
	}

	rows, err = tx.Query(ctx, `SELECT task_id, role, task_class FROM agentrt.task WHERE state = 'ready' ORDER BY created_at, task_id`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var id, role, class string
		if err := rows.Scan(&id, &role, &class); err != nil {
			rows.Close()
			return s, err
		}
		s.ready = append(s.ready, map[string]any{"kind": "task_ready", "task_id": id, "role": role, "task_class": class})
	}
	rows.Close()
	return s, rows.Err()
}
