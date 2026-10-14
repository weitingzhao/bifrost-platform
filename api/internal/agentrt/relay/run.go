package relay

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
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
	_, gerr := r.Redis.Get(ctx, r.key("meta:epoch")).Result()
	switch {
	case errors.Is(gerr, redis.Nil):
		epoch, err := lease.Rebuild(ctx)
		if err != nil {
			return err
		}
		slog.Info("agentrt relay: rebuilt redis-agents from PostgreSQL", "epoch", epoch)
	case gerr != nil:
		return gerr
	}
	conn, err := r.PG.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `LISTEN agrt_outbox`); err != nil {
		return err
	}
	renewEvery := r.ttl() / 3
	nextRenew := time.Now().Add(renewEvery)
	for ctx.Err() == nil {
		for {
			n, err := lease.DeliverOnce(ctx)
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
		}
		if time.Now().After(nextRenew) {
			if err := lease.Renew(ctx); err != nil {
				return err
			}
			nextRenew = time.Now().Add(renewEvery)
		}
		wait := time.Until(nextRenew)
		if wait > 2*time.Second {
			wait = 2 * time.Second
		}
		wctx, cancel := context.WithTimeout(ctx, wait)
		_, err := conn.Conn().WaitForNotification(wctx)
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return err
		}
	}
	return ctx.Err()
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
