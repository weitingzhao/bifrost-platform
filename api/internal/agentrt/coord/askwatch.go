package coord

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The asks nobody answers, said on the bus (2026-11-21, the main line "seen
// and felt", law 93: who waits on whom was on the Bus page's panel, seen only
// by whoever looked, and chasing it is the coordinator's job). An ask is open
// until any letter comes back from its recipient to its sender after it, as
// the Bus page counts it. An open ask older than AskOverdueAfter, or one
// whose recipient waits on its sender too (a cycle, at once), is said once as
// seat.ask.overdue; the subscription table decides who is woken by it.

// AskOverdueType is the registered event (bus/registry rocket.yaml).
const AskOverdueType = "seat.ask.overdue"

// AskOverdueAfter is how long an ask may stay unanswered (2026-11-21 05:5xZ
// reading: 7 of 8 waiting links were past 60 minutes, mostly one seat's
// broadcast asks, so 60 minutes would have rung 7 letters at once).
const AskOverdueAfter = 4 * time.Hour

// askWatchEvery is how often the asks are read; askLookBack how far back.
const (
	askWatchEvery = 10 * time.Minute
	askLookBack   = 7 * 24 * time.Hour
)

// OpenAsk is an ask no letter answered.
type OpenAsk struct {
	ID, From, To string
	At           time.Time
}

// openAsks reads the asks since a time that no later letter from their
// recipient to their sender answered; letters to the status board and from
// the controller are not conversation.
func openAsks(ctx context.Context, tx pgx.Tx, since time.Time) ([]OpenAsk, error) {
	rows, err := tx.Query(ctx, `SELECT m.message_id, m.from_actor_id, m.to_actor_id, m.created_at FROM agentrt.message m
	  WHERE m.message_kind = 'ask' AND m.created_at >= $1 AND m.to_actor_id <> $2 AND m.from_actor_id <> $2
	    AND NOT EXISTS (SELECT 1 FROM agentrt.message r WHERE r.from_actor_id = m.to_actor_id AND r.to_actor_id = m.from_actor_id
	      AND r.created_at > m.created_at)
	  ORDER BY m.created_at`, since, StatusBoard)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenAsk
	for rows.Next() {
		var a OpenAsk
		if err := rows.Scan(&a.ID, &a.From, &a.To, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// OverdueAsk is an open ask to say, and why: overdue or cycle.
type OverdueAsk struct {
	OpenAsk
	Reason string
}

// Overdue picks the open asks to say: those older than after, and, at once,
// those whose recipient has an open ask to their sender (they wait on each
// other).
func Overdue(asks []OpenAsk, now time.Time, after time.Duration) []OverdueAsk {
	waits := map[[2]string]bool{}
	for _, a := range asks {
		waits[[2]string{a.From, a.To}] = true
	}
	var out []OverdueAsk
	for _, a := range asks {
		switch {
		case waits[[2]string{a.To, a.From}]:
			out = append(out, OverdueAsk{a, "cycle"})
		case now.Sub(a.At) > after:
			out = append(out, OverdueAsk{a, "overdue"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// AskOverdueBody is the event body of an overdue ask.
func AskOverdueBody(a OverdueAsk) map[string]any {
	return map[string]any{"message_id": a.ID, "asker": a.From, "asked": a.To, "asked_at": a.At.UTC().Format(time.RFC3339),
		"reason": a.Reason, "subject": a.ID, "ref": "/api/v1/bus/observe", "change": a.Reason}
}

// FlagOverdueAsks writes seat.ask.overdue once for each ask Overdue picks
// (a stream per ask, askwatch:<message id>), as the controller; it returns
// how many it wrote.
func FlagOverdueAsks(ctx context.Context, db eventlog.DB, now time.Time) (int, error) {
	n := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		asks, err := openAsks(ctx, tx, now.Add(-askLookBack))
		if err != nil {
			return err
		}
		for _, a := range Overdue(asks, now, AskOverdueAfter) {
			stream := "askwatch:" + a.ID
			if err := eventlog.LockStream(ctx, tx, stream); err != nil {
				return err
			}
			if _, said, err := eventlog.Last(ctx, tx, stream); err != nil {
				return err
			} else if said {
				continue
			}
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: AskOverdueType, Actor: StatusBoard, Body: AskOverdueBody(a)}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// StartAskWatch reads the open asks every askWatchEvery until ctx ends (the
// workers: one writer of the ledger's ask streams).
func StartAskWatch(ctx context.Context, db func() eventlog.DB) {
	safego.Go("bus-ask-watch", func() {
		tick := time.NewTicker(askWatchEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			d := db()
			if d == nil {
				continue
			}
			if n, err := FlagOverdueAsks(ctx, d, time.Now()); err != nil {
				slog.Warn("bus ask watch: not read", "error", err)
			} else if n > 0 {
				slog.Info("bus ask watch: asks said overdue", "count", n)
			}
		}
	})
}
