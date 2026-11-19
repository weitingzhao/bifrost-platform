//bifrost:owner coordination

package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	ctx := ctxOf(t)
	var n int
	ok(t, w.rt.QueryRow(ctx, q, args...).Scan(&n))
	return n
}

// footprint is what a write could leave: actors, events, streams and their
// heads, outbox rows, idempotency keys, missions, tasks, dependencies,
// controls, artifacts and their bodies.
func (w *world) footprint(t *testing.T) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT concat_ws(' ',
	  (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event), (SELECT count(*) FROM agentrt.event_stream),
	  (SELECT string_agg(event_stream_id || '=' || last_seq || ':' || last_sha256, ',' ORDER BY event_stream_id) FROM agentrt.event_stream),
	  (SELECT count(*) FROM agentrt.idempotency_key), (SELECT count(*) FROM agentrt.mission),
	  (SELECT count(*) FROM agentrt.control),
	  (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob))`).Scan(&s))
	return s
}

// threadsPerDay is the default daily bound on registered threads.
const threadsPerDay = 200

// wr is a writer as the routes make one: the actor and the token.
func wr(id string) actor.Writer { return actor.Writer{Actor: id, Token: "operator"} }

// legacy writes rows with the import event, which may write any table: the
// fixture for actors the routes do not create (retired ones, odd kinds).
func (w *world) legacy(t *testing.T, stmts ...string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

// Review 176 part 2 A1: a full day of registrations does not keep a new
// session from stopping work; it still keeps it from proposing.
func TestControlsPastThreadQuota(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
	  SELECT 'thread:bulk' || i, 'thread', 'bulk' FROM generate_series(1, %d) i`, threadsPerDay))
	_, err := coord.RequestControl(ctx, w.rt, wr("thread:late"), "k1", "global", "", "stop", "stop everything")
	ok(t, err)
	_, err = proposeForSession(ctx, w.rt, wr("thread:later"), "k1")
	is(t, err, eventlog.ErrInvalid)
}

// Retired missions no longer register sessions. Exercise the same actor and
// idempotency guards through the live rule-proposal route (Relay A, 2026-11-15).
func proposeForSession(ctx context.Context, db eventlog.DB, w actor.Writer, key string) (map[string]any, error) {
	text := ruleText("tuning", 1, "initial", since, until, "values: {assess_per_pass: 5}\n")
	return store.ProposeRule(ctx, db, w, key, "tuning", []byte(text), "session guard fixture")
}
