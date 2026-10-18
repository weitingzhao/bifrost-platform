// Package relay carries committed outbox rows from PostgreSQL to
// redis-agents and rebuilds redis-agents from PostgreSQL (design v5 §5.1.4).
//
// PostgreSQL is the record; Redis only signals that something changed. A
// trimmed, duplicated or stale signal costs a reader one extra query, never a
// fact. One relay at a time holds the lock; every write it makes, including
// deleting the lock, first checks in Lua that the lock still holds its token,
// so a relay that lost the lock cannot write after a newer one took over.
package relay

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Stream lengths (approximate trimming); trimming drops signals only.
const (
	maxWake   = 10000
	maxMbox   = 1000
	maxStop   = 1000
	maxEvents = 50000
	group     = "g"
)

var (
	// ErrHeld means another relay holds the lock.
	ErrHeld = errors.New("relay: the lock is held by another relay")
	// ErrFenced means this relay's lock is gone; it must stop writing.
	ErrFenced = errors.New("relay: lock lost; this relay no longer writes")
)

// Relay is one relay instance.
type Relay struct {
	PG      *pgxpool.Pool
	Redis   *redis.Client
	Prefix  string        // "agrt:" in PROD and STG, "agrt-dev:" on DEV's shared Redis
	LockTTL time.Duration // default 30s; renewed every LockTTL/3
	Batch   int           // outbox rows per pass, default 100

	// afterXAdd, when set, runs after a signal is in Redis and before its
	// outbox row is marked delivered; tests use it to crash in between (T31).
	afterXAdd func(outboxID int64) error
	// afterSnapshot, when set, runs between reading PostgreSQL and writing
	// Redis during a rebuild; tests use it to write concurrently (T15a).
	afterSnapshot func()
	// beforeEpoch, when set, runs just before a rebuild writes the epoch;
	// tests take the lock away there (review 144 B2, T15a).
	beforeEpoch func()
}

func (r *Relay) key(s string) string { return r.Prefix + s }

func (r *Relay) ttl() time.Duration {
	if r.LockTTL <= 0 {
		return 30 * time.Second
	}
	return r.LockTTL
}

// Lease is a held lock: the token is this holder's fencing value.
type Lease struct {
	r     *Relay
	token string
}

// Token returns the fencing value of the lease.
func (l *Lease) Token() string { return l.token }

// Acquire takes the relay lock with a fresh fencing token.
func (r *Relay) Acquire(ctx context.Context) (*Lease, error) {
	n, err := r.Redis.Incr(ctx, r.key("relay:fence")).Result()
	if err != nil {
		return nil, err
	}
	token := strconv.FormatInt(n, 10)
	ok, err := r.Redis.SetNX(ctx, r.key("relay:lock"), token, r.ttl()).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrHeld
	}
	return &Lease{r: r, token: token}, nil
}

// fenced runs one write only while the lock holds this lease's token.
var fenced = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return redis.error_reply('FENCED')
end
local op = ARGV[2]
if op == 'XADD' then
  return redis.call('XADD', KEYS[2], 'MAXLEN', '~', ARGV[3], '*', unpack(ARGV, 4))
elseif op == 'DEL' then
  return redis.call('DEL', KEYS[2])
elseif op == 'GROUP' then
  local ok, e = pcall(redis.call, 'XGROUP', 'CREATE', KEYS[2], ARGV[3], '0', 'MKSTREAM')
  if not ok and not string.find(tostring(e), 'BUSYGROUP') then
    return redis.error_reply(tostring(e))
  end
  return 1
elseif op == 'SETPX' then
  return redis.call('SET', KEYS[2], ARGV[3], 'PX', ARGV[4])
elseif op == 'SET' then
  return redis.call('SET', KEYS[2], ARGV[3])
elseif op == 'RENEW' then
  return redis.call('PEXPIRE', KEYS[1], ARGV[3])
elseif op == 'RELEASE' then
  return redis.call('DEL', KEYS[1])
end
return redis.error_reply('unknown op ' .. op)
`)

func (l *Lease) do(ctx context.Context, op string, key string, args ...any) error {
	keys := []string{l.r.key("relay:lock")}
	if key != "" {
		keys = append(keys, key)
	}
	argv := append([]any{l.token, op}, args...)
	err := fenced.Run(ctx, l.r.Redis, keys, argv...).Err()
	// Redis 7 returns the reply as given; Redis 8 prefixes it with "ERR ".
	if err != nil && (err.Error() == "FENCED" || err.Error() == "ERR FENCED") {
		return ErrFenced
	}
	return err
}

// Renew extends the lock; ErrFenced means it was lost.
func (l *Lease) Renew(ctx context.Context) error {
	return l.do(ctx, "RENEW", "", l.r.ttl().Milliseconds())
}

// Release drops the lock if it is still this lease's.
func (l *Lease) Release(ctx context.Context) error {
	return l.do(ctx, "RELEASE", "")
}

func (l *Lease) xadd(ctx context.Context, stream string, maxLen int, fields ...string) error {
	args := []any{maxLen}
	for _, f := range fields {
		args = append(args, f)
	}
	return l.do(ctx, "XADD", stream, args...)
}

type outboxRow struct {
	id      int64
	eventID int64
	topic   string
	payload string
}

// DeliverOnce moves up to Batch undelivered outbox rows, oldest first. A
// crash after XADD and before the row is marked sends that signal again;
// readers de-duplicate by event_id.
func (l *Lease) DeliverOnce(ctx context.Context) (int, error) {
	batch := l.r.Batch
	if batch <= 0 {
		batch = 100
	}
	rows, err := l.r.PG.Query(ctx, `SELECT outbox_id, event_id, topic, payload_json::text FROM agentrt.outbox
	  WHERE delivered_at IS NULL ORDER BY outbox_id LIMIT $1`, batch)
	if err != nil {
		return 0, err
	}
	pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (outboxRow, error) {
		var o outboxRow
		scanErr := row.Scan(&o.id, &o.eventID, &o.topic, &o.payload)
		return o, scanErr
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range pending {
		if err := l.send(ctx, o); err != nil {
			return n, err
		}
		if l.r.afterXAdd != nil {
			if err := l.r.afterXAdd(o.id); err != nil {
				return n, err
			}
		}
		if _, err := l.r.PG.Exec(ctx, `UPDATE agentrt.outbox SET delivered_at = clock_timestamp(), tries = tries + 1
		  WHERE outbox_id = $1 AND delivered_at IS NULL`, o.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (l *Lease) send(ctx context.Context, o outboxRow) error {
	eid := strconv.FormatInt(o.eventID, 10)
	switch {
	case o.topic == "wake":
		return l.xadd(ctx, l.r.key("wake"), maxWake, "event_id", eid, "p", o.payload)
	case o.topic == "stop":
		return l.xadd(ctx, l.r.key("stop"), maxStop, "event_id", eid, "p", o.payload)
	case o.topic == "events":
		return l.xadd(ctx, l.r.key("events"), maxEvents, "event_id", eid, "p", o.payload)
	case o.topic == "cooldown":
		return l.cooldown(ctx, o.payload)
	case len(o.topic) > 5 && o.topic[:5] == "mbox:":
		stream := l.r.key(o.topic)
		if err := l.do(ctx, "GROUP", stream, group); err != nil {
			return err
		}
		return l.xadd(ctx, stream, maxMbox, "event_id", eid, "p", o.payload)
	}
	return fmt.Errorf("relay: unknown outbox topic %q", o.topic)
}

func (l *Lease) cooldown(ctx context.Context, payload string) error {
	v, err := canon.Parse([]byte(payload))
	if err != nil {
		return err
	}
	m, _ := v.(map[string]any)
	pool, _ := m["vendor_pool_id"].(string)
	untilText, _ := m["until"].(string)
	until, err := time.Parse(time.RFC3339Nano, untilText)
	if pool == "" || err != nil {
		return fmt.Errorf("relay: bad cooldown payload %s", payload)
	}
	left := time.Until(until)
	if left <= 0 {
		// a cooldown lifted early (the Owner's reading, 2.4 slice ①)
		return l.do(ctx, "DEL", l.r.key("cooldown:"+pool))
	}
	return l.do(ctx, "SETPX", l.r.key("cooldown:"+pool), payload, left.Milliseconds())
}
