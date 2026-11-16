package relay

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// PageSize is how many events the relay reads at once.
const PageSize = 100

// Relay publishes the ledger's events of registered types, in ledger order,
// to the events stream. It is a singleton of the workers (RULES.md §7).
type Relay struct {
	Ledger Ledger
	Out    Out
	Tenant registry.Tenant
	Env    string
	Logf   func(format string, args ...any)

	// PollEvery is the fallback when no notice wakes it (15 s);
	// ReconcileEvery how often the stream is compared with the ledger (5 min).
	PollEvery, ReconcileEvery time.Duration

	cursor Cursor
	// start is where this process began when the stream was empty: the
	// stream's record of what was published starts there.
	start Cursor
}

func (r *Relay) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// Cursor is the position published up to.
func (r *Relay) Cursor() Cursor { return r.cursor }

// Resume sets where to publish from: the cursor of the stream's last message
// (the stream is the record of what was published: if NATS lost messages
// after it, the earlier cursor publishes them again and subscribers drop
// what they saw by id); for an empty stream, the ledger's current horizon,
// without replaying history.
func (r *Relay) Resume(ctx context.Context) error {
	c, ok, err := r.Out.LastCursor(ctx)
	if err != nil {
		return err
	}
	if ok {
		r.cursor, r.start = c, c
		r.logf("bus relay: resuming after %s, the cursor of the stream's last message", c)
		return nil
	}
	h, err := r.Ledger.Horizon(ctx)
	if err != nil {
		return err
	}
	r.cursor, r.start = h, h
	r.logf("bus relay: the stream is empty; starting at the ledger's horizon %s (history is not replayed)", h)
	return nil
}

// Pass publishes every event of a registered type after the cursor, and
// moves the cursor past each only once the stream has acknowledged it. It
// returns how many it published.
func (r *Relay) Pass(ctx context.Context) (int, error) {
	types := r.Tenant.Types()
	n := 0
	for {
		rows, next, err := r.Ledger.After(ctx, r.cursor, PageSize, types)
		if err != nil {
			return n, err
		}
		for _, row := range rows {
			ch, ok := r.Tenant.Channels[row.Type]
			if !ok {
				r.cursor = row.Cursor()
				continue
			}
			env, data, err := Build(row, ch, r.Tenant.Name, r.Env)
			if err != nil {
				// An event no envelope can be made of would stop the relay
				// for good; it is said and passed over (it stays in the ledger).
				metricSkipped.Add(1)
				r.logf("bus relay: event %d passed over: %v", row.EventID, err)
				r.cursor = row.Cursor()
				continue
			}
			if err := r.Out.Publish(ctx, EventSubject(r.Tenant.Name, r.Env, row.Type), env.ID, row.Cursor(), data); err != nil {
				return n, fmt.Errorf("bus relay: publish %s: %w", env.ID, err)
			}
			r.cursor = row.Cursor()
			n++
			metricPublished.Add(1)
		}
		if len(rows) < PageSize {
			if r.cursor.Less(next) {
				r.cursor = next
			}
			return n, nil
		}
	}
}

// Reconcile compares the stream with the ledger (RULES.md §7): when the
// ledger holds an event of a registered type after the stream's last cursor
// that this relay has already passed, the stream lost it. The drift is said
// and counted, and the relay goes back to the stream's cursor to publish
// again. Run it between passes: nothing is in flight then.
func (r *Relay) Reconcile(ctx context.Context) (bool, error) {
	last, ok, err := r.Out.LastCursor(ctx)
	if err != nil {
		return false, err
	}
	if !ok {
		last = r.start
	}
	rows, _, err := r.Ledger.After(ctx, last, 1, r.Tenant.Types())
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if r.cursor.Less(rows[0].Cursor()) {
		return false, nil // not published yet: not a drift
	}
	metricDrift.Add(1)
	r.logf("bus relay: drift: the ledger has %s (%s) after the stream's last cursor %s, which the relay passed at %s; publishing again from %s",
		EventID(rows[0].EventID), rows[0].Type, last, r.cursor, last)
	r.cursor = last
	return true, nil
}

// Run resumes and publishes until ctx ends: on each wake, at least every
// PollEvery, and reconciles every ReconcileEvery.
func (r *Relay) Run(ctx context.Context, wake <-chan struct{}) {
	poll, reconcile := r.PollEvery, r.ReconcileEvery
	if poll <= 0 {
		poll = 15 * time.Second
	}
	if reconcile <= 0 {
		reconcile = 5 * time.Minute
	}
	for {
		if err := r.Resume(ctx); err == nil {
			break
		} else {
			r.logf("bus relay: cannot resume: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(poll):
		}
	}
	pollT, recT := time.NewTicker(poll), time.NewTicker(reconcile)
	defer pollT.Stop()
	defer recT.Stop()
	for {
		if _, err := r.Pass(ctx); err != nil && ctx.Err() == nil {
			metricErrors.Add(1)
			r.logf("bus relay: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-pollT.C:
		case <-recT.C:
			if _, err := r.Reconcile(ctx); err != nil && ctx.Err() == nil {
				r.logf("bus relay: reconcile: %v", err)
			}
		}
	}
}

// Listen wakes on each commit that wrote an event (eventlog.Append notifies
// eventlog.LedgerChannel), on one connection of the pool, held,
// and reconnects when that connection fails. It also wakes once each time
// the LISTEN is in place, for what committed while it was not. The channel
// holds at most one wake: notices that come while one waits are folded into it.
func Listen(ctx context.Context, pool *pgxpool.Pool, logf func(string, ...any)) <-chan struct{} {
	out := make(chan struct{}, 1)
	wake := func() {
		select {
		case out <- struct{}{}:
		default:
		}
	}
	safego.Go("bus-relay-listen", func() {
		for ctx.Err() == nil {
			err := listenOnce(ctx, pool, wake)
			if ctx.Err() != nil {
				return
			}
			if logf != nil {
				logf("bus: listening on %s failed, again in 5s: %v", eventlog.LedgerChannel, err)
			}
			wake() // a reader that missed notices while down reads anyway
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	})
	return out
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, wake func()) error {
	// The connection is one of the pool's, held for as long as it listens:
	// the process opens no connection beyond its pool's budget (the runtime
	// role's limit is shared by api and workers, agentrtMaxConns). When it
	// stops listening it is closed, so the pool drops it, not reuses it.
	pc, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	conn := pc.Conn()
	defer func() {
		_ = conn.Close(context.WithoutCancel(ctx))
		pc.Release()
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{eventlog.LedgerChannel}.Sanitize()); err != nil {
		return err
	}
	// Listening from here: whatever committed before is read once now.
	wake()
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return err
		}
		wake()
	}
}

var metricPublished, metricSkipped, metricDrift, metricErrors atomic.Int64

// WriteMetrics writes the relay's counters in the exposition format; a
// process that runs no relay writes zeros.
func WriteMetrics(b *strings.Builder) {
	for _, m := range []struct {
		name, help string
		v          int64
	}{
		{"bifrost_bus_relay_published_total", "Events the bus relay published and the stream acknowledged", metricPublished.Load()},
		{"bifrost_bus_relay_skipped_total", "Ledger events of registered types no envelope could be made of", metricSkipped.Load()},
		{"bifrost_bus_relay_drift_total", "Times the stream was found behind events the relay had passed (reconcile.drift.found, not yet an event)", metricDrift.Load()},
		{"bifrost_bus_relay_errors_total", "Relay passes that ended in an error", metricErrors.Load()},
	} {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", m.name, m.help, m.name, m.name, m.v)
	}
}
