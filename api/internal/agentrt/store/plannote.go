package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// Notes on the architecture table (W-31 2.5 G2 A, CUT-round2-2.5-interface
// §2): what a row of the table actually is now, in words, is a note event on
// the row, on its plan's stream; GET /progress shows the latest one beside
// the row's tick, which its done rule decides. Notes are only appended:
// a newer note says what is true now, the older ones stay in the stream.

// MaxNoteText is the longest note in bytes: as a message body, text and not
// a document (that is an artifact).
const MaxNoteText = MaxMessageBody

// checkNoteText checks a note's text: UTF-8 text of 1 to MaxNoteText bytes
// that is not only white space, without NUL, and held to the rules an
// agent's text is held to: no local path (H8, localpath.Find) and no
// reference to a task's workspace (T11, localpath.WorkspaceRef).
func checkNoteText(text string) error {
	switch {
	case strings.TrimSpace(text) == "" || len(text) > MaxNoteText || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0:
		return invalid("a note is UTF-8 text of 1 to %d bytes, not only white space, without NUL", MaxNoteText)
	}
	if p, found := localpath.Find([]byte(text)); found {
		return invalid("a local path in the note: %s", p)
	}
	if ref, found := localpath.WorkspaceRef([]byte(text)); found {
		return invalid("the note refers to a task's workspace: %s", ref)
	}
	return nil
}

// PostPlanNote records a writer's note on an architecture row (POST
// /plan/{id}/notes): a note event naming the row, on the stream of the
// row's root. A node there is not is pgx.ErrNoRows; any other kind of node
// is refused. The writer is registered as for proposals (a thread on its
// first write) and the key is the writer's, as for proposals: a retry gets
// the first answer, the same key with another note is a conflict.
func PostPlanNote(ctx context.Context, db DB, w Writer, key, nodeID, text string) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if err := checkNoteText(text); err != nil {
		return nil, err
	}
	req := map[string]any{"plan_node_id": nodeID, "text": text}
	return idempotentAs(ctx, db, w, "plan/notes", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		var kind string
		var parent *string
		err := tx.QueryRow(ctx, `SELECT node_kind, parent_plan_node_id FROM agentrt.plan_node WHERE plan_node_id = $1`, nodeID).Scan(&kind, &parent)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("store: no plan node %s: %w", nodeID, err)
		case err != nil:
			return nil, err
		case kind != "arch_row" || parent == nil:
			return nil, invalid("%s is a %s: only an architecture row takes a note", nodeID, kind)
		}
		ev, err := Append(ctx, tx, Event{Stream: PlanStream(*parent), Type: "note", Actor: w.Actor, PlanNode: nodeID,
			Body: map[string]any{"text": text}})
		if err != nil {
			return nil, err
		}
		// A note that says the Owner approved is recorded as a spec's or a
		// message's is (T04; review 489 B3), and grants nothing: the row's
		// tick is its gates'.
		if err = alertClaim(ctx, tx, Event{Stream: PlanStream(*parent), PlanNode: nodeID}, ev.EventID, "plan_note", "", []byte(text)); err != nil {
			return nil, err
		}
		return map[string]any{"event_id": ev.EventID, "plan_node_id": nodeID, "by": w.Actor,
			"at": ev.At.UTC().Format(time.RFC3339Nano)}, nil
	})
}
