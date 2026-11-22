package evalreadings

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

// askRow is an ask between seats and the first letter its recipient sent back
// to its sender after it (nil: none yet, as of the read's end).
type askRow struct {
	At      time.Time
	Replied *time.Time
}

// askLookBack is how far before the window's end an ask still counts as
// waiting (the ask watch's own look-back).
const askLookBack = 7 * 24 * time.Hour

// readAsks reads the asks sent in [from, to) and, for each, the first letter
// from its recipient to its sender after it and before to. Letters to or from
// the status board are not conversation. Same judgement as the Bus page's
// "waiting for an answer" panel and coord's ask watch (coord.openAsks).
func readAsks(ctx context.Context, tx pgx.Tx, from, to time.Time) ([]askRow, error) {
	rows, err := tx.Query(ctx, `SELECT m.created_at,
	    (SELECT min(r.created_at) FROM agentrt.message r WHERE r.from_actor_id = m.to_actor_id AND r.to_actor_id = m.from_actor_id
	       AND r.created_at > m.created_at AND r.created_at < $2)
	  FROM agentrt.message m
	  WHERE m.message_kind = 'ask' AND m.created_at >= $1 AND m.created_at < $2
	    AND m.to_actor_id <> $3 AND m.from_actor_id <> $3`, from, to, coord.StatusBoard)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []askRow
	for rows.Next() {
		var a askRow
		if err := rows.Scan(&a.At, &a.Replied); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// askWaitReadings: how long an ask between seats waits for its answer.
// Origin: the communication line, 2026-11-22: seat.ask.overdue (wake the
// coordinator after 4 hours) needs its return measured before and after it
// ships, by the same rule the Bus page uses. ask_answer_p50_seconds and
// ask_answer_max_seconds are over the asks sent in the window and answered
// before its end; ask_open_over_4h counts, at the window's end, the asks of
// the 7 days before it still unanswered and older than 4 hours. The letters
// are kept with no retention, so past windows are read back the same way.
func (s ledgerSource) askWaitReadings(w Window) ([]Reading, error) {
	const src = "agentrt.message, message_kind ask, neither side the status board; answered = first letter from recipient to sender after it"
	if s.asks == nil {
		var out []Reading
		for _, n := range []string{"ask_answer_p50_seconds", "ask_answer_max_seconds", "ask_open_over_4h"} {
			out = append(out, finish(Reading{Name: n, Window: w, Note: "the messages are not readable on this platform"}, false))
		}
		return out, nil
	}
	rows, err := s.asks(w.To.Add(-askLookBack), w.To)
	if err != nil {
		return nil, fmt.Errorf("asks: %w", err)
	}
	var waits []float64
	open := 0
	for _, a := range rows {
		if a.Replied == nil {
			if w.To.Sub(a.At) > coord.AskOverdueAfter {
				open++
			}
			continue
		}
		if in(a.At, w) {
			waits = append(waits, a.Replied.Sub(a.At).Seconds())
		}
	}
	var longest *float64
	for _, x := range waits {
		if longest == nil || x > *longest {
			v := x
			longest = &v
		}
	}
	openR := finish(Reading{Name: "ask_open_over_4h", Value: count(open), Unit: "count", Samples: open, Window: w,
		Definition: def(src, "at the window's end: asks of the 7 days before it with no answer, older than 4 hours")}, false)
	measuredZero(&openR)
	maxR := finish(Reading{Name: "ask_answer_max_seconds", Value: longest, Unit: "seconds", Samples: len(waits), Window: w,
		Definition: def(src, "longest answer time of the asks sent in the window and answered before its end")}, false)
	return []Reading{
		finish(Reading{Name: "ask_answer_p50_seconds", Value: median(waits), Unit: "seconds", Samples: len(waits), Window: w,
			Definition: def(src, "median answer time of the asks sent in the window and answered before its end")}, true),
		maxR,
		openR,
	}, nil
}
