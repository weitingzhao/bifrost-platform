package relay

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Run holds the lock while it can and delivers until ctx ends. When the
// epoch key is missing (a new or wiped Redis) it rebuilds first. When the
// lock is lost it stops writing at once and tries to take it again.
func (r *Relay) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		lease, err := r.Acquire(ctx)
		if errors.Is(err, ErrHeld) {
			sleep(ctx, 2*time.Second)
			continue
		}
		if err != nil {
			slog.Warn("agentrt relay: acquire", "err", err)
			sleep(ctx, 2*time.Second)
			continue
		}
		if herr := r.hold(ctx, lease); herr != nil && ctx.Err() == nil {
			slog.Warn("agentrt relay: stopped holding", "err", herr)
		}
		_ = lease.Release(context.WithoutCancel(ctx))
	}
	return ctx.Err()
}

func (r *Relay) hold(ctx context.Context, lease *Lease) error {
	// The lease is renewed in the background for the whole time it is held,
	// rebuild included; if a renewal fails, everything under ctx stops
	// (review 144 B1).
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	safego.Go("agentrt.relay.keepalive", func() { lease.keepAlive(ctx, cancel) })

	// Rebuild when the epoch is missing (a new or wiped Redis) or a rebuild
	// was left unfinished (review 144 B2).
	_, gerr := r.Redis.Get(ctx, r.key("meta:epoch")).Result()
	flag, ferr := r.Redis.Exists(ctx, r.key("meta:rebuilding")).Result()
	switch {
	case gerr != nil && !errors.Is(gerr, redis.Nil):
		return gerr
	case ferr != nil:
		return ferr
	case errors.Is(gerr, redis.Nil) || flag == 1:
		epoch, err := lease.Rebuild(ctx)
		if err != nil {
			return errors.Join(err, context.Cause(ctx))
		}
		slog.Info("agentrt relay: rebuilt redis-agents from PostgreSQL", "epoch", epoch)
	}
	conn, err := r.PG.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `LISTEN agrt_outbox`); err != nil {
		return err
	}
	for ctx.Err() == nil {
		for ctx.Err() == nil {
			n, err := lease.DeliverOnce(ctx)
			if err != nil {
				return errors.Join(err, context.Cause(ctx))
			}
			if n == 0 {
				break
			}
		}
		wctx, wcancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := conn.Conn().WaitForNotification(wctx)
		wcancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return err
		}
	}
	return context.Cause(ctx)
}

// keepAlive renews the lease every third of its TTL until ctx ends; a failed
// renewal cancels ctx with the reason.
func (l *Lease) keepAlive(ctx context.Context, cancel context.CancelCauseFunc) {
	t := time.NewTicker(l.r.ttl() / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := l.Renew(ctx); err != nil {
				if ctx.Err() == nil {
					cancel(err)
				}
				return
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
