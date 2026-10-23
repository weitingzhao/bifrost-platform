package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// An intake client's updates (get_updates, DESIGN-crew-intake §2.1, §3.3,
// §4.3): what the crew wrote back on the issues it sees, read on by the
// cross-stream cursor GET /events uses (EventsAfter, <txid>.<event_id>),
// filtered in the same statement, so none is skipped and none listed twice
// whatever commits out of order. Each read moves how far the client was
// served (MarkServed, no event, R2-A5); its acknowledgement of what it
// stored moves on with an event (AckIntake). At least once: a client keeps
// each update once by its update_id.

// MaxUpdates is the most updates one read gives (§2.1: 0 to 50; 0
// acknowledges only).
const MaxUpdates = 50

// UpdatesRequest is get_updates: where to read on from (nil: from the
// client's acknowledgement, else from the start), what it has stored
// (nil: nothing to acknowledge) and how many it takes.
type UpdatesRequest struct {
	Cursor, Ack *EventCursor
	Limit       int
}

// ErrBadCursor: a cursor the client was never handed (§2.1, review 524 B2):
// past how far it was served. Nothing is read, served or acknowledged.
var ErrBadCursor = errors.New("store: a cursor this client was not handed")

// IntakeUpdates acknowledges, then reads on. A cursor past how far the client
// was served is refused before anything else (review 524 B2: a caller's
// future cursor would otherwise be served and acknowledged, and its updates
// read past for good). The acknowledgement goes first and is held to what
// was served before this read; an older one changes nothing. How far it was
// served moves only to what the read returned or the horizon it computed.
func IntakeUpdates(ctx context.Context, db DB, w Writer, req UpdatesRequest) (map[string]any, error) {
	if req.Limit < 0 || req.Limit > MaxUpdates {
		return nil, invalid("limit is 0 to %d", MaxUpdates)
	}
	if kindOf(w.Actor) != "agent" {
		return nil, invalid("%s is no intake client", w.Actor)
	}
	if req.Cursor != nil {
		if err := intakeTx(ctx, db, w, func(_ pgx.Tx, c intakeClient) error { return handedCursor(c, *req.Cursor) }); err != nil {
			return nil, err
		}
	}
	out := map[string]any{"updates": []any{}}
	if req.Ack != nil {
		acked, err := AckIntake(ctx, db, w, *req.Ack)
		if err != nil {
			return nil, err
		}
		out["acked"] = acked["acked"]
	}
	err := intakeTx(ctx, db, w, func(tx pgx.Tx, c intakeClient) error {
		from := EventCursor{}
		switch {
		case req.Cursor != nil:
			if err := handedCursor(c, *req.Cursor); err != nil {
				return err
			}
			from = *req.Cursor
		case c.hasAcked:
			from = c.acked
		}
		if req.Limit == 0 {
			out["next_cursor"] = from.String()
			return nil
		}
		doc, next, err := EventsAfter(ctx, tx, from, req.Limit, EventFilter{IntakeClient: w.Actor})
		if err != nil {
			return err
		}
		var events []struct {
			EventID   int64          `json:"event_id"`
			Stream    string         `json:"event_stream_id"`
			Type      string         `json:"event_type"`
			At        string         `json:"at"`
			Body      map[string]any `json:"body_json"`
			MissionID *string        `json:"mission_id"`
		}
		if err := json.Unmarshal(doc, &events); err != nil {
			return err
		}
		texts, asks, err := updateTexts(ctx, tx, doc)
		if err != nil {
			return err
		}
		updates := make([]any, 0, len(events))
		for _, e := range events {
			u := map[string]any{"update_id": e.EventID, "at": e.At}
			issue := trimIssuePrefix(e.Stream)
			if issue != "" {
				u["issue_id"] = issue
			}
			text := func(k string) {
				if id, ok := e.Body[k].(float64); ok {
					if t, ok := texts[int64(id)]; ok {
						u["text"] = t
					}
				}
			}
			switch e.Type {
			case "note":
				u["type"], u["text"] = "guide_changed", "The intake guide changed: read it again with intake_guide."
			case "issue_submitted":
				u["type"], u["text"] = "received", fmt.Sprintf("%s was received. The crew writes a brief of it for the person it serves to confirm.", issue)
			case "issue_brief_drafted", "issue_asked":
				u["type"] = "confirm_brief"
				if p, ok := e.Body["purpose"].(string); ok {
					u["type"] = p
				}
				text("text_id")
				if id, ok := e.Body["ask_id"].(float64); ok {
					u["ask_id"] = int64(id)
					u["ask_user"] = asks[int64(id)]
				}
			case "issue_replied":
				u["type"] = "reply"
				text("text_id")
			case "issue_classified":
				u["type"], u["text"] = "classified", fmt.Sprintf("The crew has looked at %s and sorted it.", issue)
			case "issue_taken":
				u["type"], u["text"] = "taken", fmt.Sprintf("The crew has taken %s on.", issue)
			case "issue_merged":
				u["type"] = "merged"
				if into, ok := e.Body["into"].(string); ok {
					u["text"], u["into"] = fmt.Sprintf("%s was merged into %s, of the same person: read on there.", issue, into), into
				} else if from, ok := e.Body["merged_from"].(string); ok {
					u["text"] = fmt.Sprintf("%s was merged into %s.", from, issue)
				}
			case "issue_state_changed":
				if e.Body["change"] == "reopen" {
					u["type"], u["text"] = "reopened", fmt.Sprintf("%s is open again.", issue)
				} else {
					u["type"], u["conclusion"] = "closed", e.Body["conclusion"]
					u["text"] = fmt.Sprintf("%s is closed: %v.", issue, e.Body["conclusion"])
				}
			}
			updates = append(updates, u)
		}
		out["updates"], out["next_cursor"] = updates, next.String()
		if next == from {
			return nil // nothing read and no horizon passed: served stays where it is
		}
		return MarkServed(ctx, tx, w.Actor, next)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// handedCursor checks that a cursor is one the client was handed: at most
// how far it was served (the start, before it was served anything).
func handedCursor(c intakeClient, cur EventCursor) error {
	if (!c.hasServed && cur != EventCursor{}) || (c.hasServed && c.served.Less(cur)) {
		return fmt.Errorf("%w: %s is past how far %s was served (%s)", ErrBadCursor, cur, c.id, servedText(c))
	}
	return nil
}

// updateTexts are the texts the page's events name that the crew wrote for
// the side that brought the issue (to_requester), by id, and whether each
// ask they open is still open (ask_user: ask the person). Nothing else of
// an issue's texts goes out.
func updateTexts(ctx context.Context, tx pgx.Tx, page []byte) (map[int64]string, map[int64]bool, error) {
	texts, asks := map[int64]string{}, map[int64]bool{}
	rows, err := tx.Query(ctx, `SELECT t.issue_text_id, t.body FROM jsonb_array_elements($1::jsonb) e
	  JOIN agentrt.issue_text t ON t.issue_text_id = (e->'body_json'->>'text_id')::bigint
	    AND 'issue:' || t.issue_id = e->>'event_stream_id' AND t.to_requester
	  WHERE e->'body_json' ? 'text_id'`, string(page))
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		var body string
		if err := rows.Scan(&id, &body); err != nil {
			rows.Close()
			return nil, nil, err
		}
		texts[id] = body
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = tx.Query(ctx, `SELECT k.issue_ask_id, k.verdict IS NULL AND k.superseded_at IS NULL FROM jsonb_array_elements($1::jsonb) e
	  JOIN agentrt.issue_ask k ON k.issue_ask_id = (e->'body_json'->>'ask_id')::bigint AND 'issue:' || k.issue_id = e->>'event_stream_id'
	  WHERE e->'body_json' ? 'ask_id'`, string(page))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var open bool
		if err := rows.Scan(&id, &open); err != nil {
			return nil, nil, err
		}
		asks[id] = open
	}
	return texts, asks, rows.Err()
}
