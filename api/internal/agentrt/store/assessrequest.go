package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The opt-in record has exactly the item kind and id, not caller-supplied
// assessment content or state. The note vocabulary checks this before Append.
func badAssessmentRequest(v any) string {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 2 {
		return "needs exactly kind and id"
	}
	kind, _ := m["kind"].(string)
	id, _ := m["id"].(string)
	if (kind != AssessCard && kind != AssessApproval) || strings.TrimSpace(id) == "" {
		return "needs a card or approval kind and a non-empty string id"
	}
	return ""
}

// RequestAssessment records the Owner's opt-in, without reading evidence or
// creating a mission. The existing item stream serializes repeated clicks and
// dispatch. check reads the current item; even a replay must reject a closed item.
func RequestAssessment(ctx context.Context, db DB, w Writer, kind, id string, check func(context.Context) error) error {
	if w.Actor != "owner" {
		return ErrNotOwner
	}
	if (kind != AssessCard && kind != AssessApproval) || strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: an assessment needs a card or approval id", ErrState)
	}
	if check == nil {
		return fmt.Errorf("%w: assessment item cannot be checked", ErrState)
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return err
		}
		stream := AssessStream(kind, id)
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		if err := check(ctx); err != nil {
			return err
		}
		st, err := assessStateOf(ctx, tx, stream)
		if err != nil || st.requested || st.asked || st.done {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: w.Actor,
			Body: map[string]any{"text": "the Owner requested an AI assessment; advice only",
				"assessment_requested": map[string]any{"kind": kind, "id": id}}})
		return err
	})
}

// ReadAssessment reads only the lasting record, including pre-opt-in results.
// Pages include this in their ordinary item read; opening a page does no work.
func ReadAssessment(ctx context.Context, db DB, kind, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		  'event_type', event_type, 'body_json', body_json, 'at', at) ORDER BY seq), '[]')
		  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, AssessStream(kind, id)).Scan(&raw)
	})
	return raw, err
}

// endUnstartedAssessments prevents a requested item left out of the open-item
// list (closed, expired, or skipped by STG's rule) from waiting indefinitely.
// No task exists yet; the same timeout bounds this queue wait separately.
func endUnstartedAssessments(ctx context.Context, db DB) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT s.event_stream_id FROM agentrt.event_stream s
          WHERE s.event_stream_id LIKE 'assess:%'
          AND EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = s.event_stream_id
            AND e.event_type = 'note' AND e.body_json ? 'assessment_requested'
            AND e.at <= clock_timestamp() - $1 * interval '1 second')
          AND NOT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = s.event_stream_id
            AND (e.body_json ? 'ask' OR e.body_json ? 'assessment' OR e.body_json ? 'unavailable'))
          ORDER BY s.event_stream_id LIMIT 100`, int(AssessTimeout.Seconds()))
		if err != nil {
			return err
		}
		streams, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, stream := range streams {
			if err := lockStream(ctx, tx, stream); err != nil {
				return err
			}
			st, err := assessStateOf(ctx, tx, stream)
			if err != nil {
				return err
			}
			if st.asked || st.done {
				continue
			}
			why := "not dispatched within " + timeoutText() + "; the item may have closed or become ineligible, or the assessment worker is unavailable"
			if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
				Body: map[string]any{"text": "AI assessment unavailable: " + why, "unavailable": why}}); err != nil {
				return err
			}
		}
		return nil
	})
}
