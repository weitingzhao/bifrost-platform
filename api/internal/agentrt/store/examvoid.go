package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Voiding a paper no run was handed in for (TD-407; the Owner 2026-10-20:
// 「加一个只有我能用的『作废一题没答的卷』，只改平台、不碰题目和评分」). An
// Agent host that refuses a paper before it runs anything tells the platform
// nothing, so the paper holds its executor for its whole life
// (ExamPaperLife) and no other paper is handed out. The Owner voids it
// through an approval of the action void_exam_paper (tier D, approved on the
// Console only); the platform's executor of that approval calls
// VoidExamPaper with the approval's id.
//
// A void is a note on the exams stream caused by the paper's issue, its body
// the approval (voided) and the reason (text): no migration, no new event
// type. A voided paper is settled: it is not graded, records no score and no
// failure, takes no answer, and holds nothing, so its executor may be handed
// a paper at once. A paper with any answer, one already graded, and one that
// has lapsed are refused: the grading of answers stays as it is.

// approvalIDRE is an approval's id as the approvals service makes it
// (approvals.newID).
var approvalIDRE = regexp.MustCompile(`^appr_[0-9a-f]{16}$`)

// maxVoidReason bounds the reason a void records.
const maxVoidReason = 500

// VoidExamPaper voids the paper of runID, which no run was handed in for, by
// the approval of approvalID, for reason.
func VoidExamPaper(ctx context.Context, db DB, runID, reason, approvalID string) (map[string]any, error) {
	return voidExamPaper(ctx, db, runID, reason, approvalID, 0)
}

func voidExamPaper(ctx context.Context, db DB, runID, reason, approvalID string, later time.Duration) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case !sha256RE.MatchString(runID):
		return nil, invalid("run_id is the paper's sha256, as the paper was handed out")
	case !approvalIDRE.MatchString(approvalID):
		return nil, invalid("a paper is voided by an approval (appr_<16 hex>), not %q", approvalID)
	case reason == "" || len(reason) > maxVoidReason || strings.ContainsAny(reason, "\r\n"):
		return nil, invalid("the reason is one line of 1 to %d bytes", maxVoidReason)
	}
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		now, err := examClock(ctx, tx, later)
		if err != nil {
			return err
		}
		p, err := findPaper(ctx, tx, runID)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("no paper of run %s was handed out", runID)
		}
		if err != nil {
			return err
		}
		voided, err := voidOf(ctx, tx, p.event)
		if err != nil {
			return err
		}
		if voided != nil {
			if voided["approval"] != approvalID {
				return fmt.Errorf("%w: run %s was voided by %s", ErrState, runID, voided["approval"])
			}
			// The same approval again (its executor retried): what it did.
			out = map[string]any{"run_id": runID, "executor": p.Executor, "host": p.Host, "voided_by": approvalID,
				"event_id": voided["event_id"], "duplicate": true}
			return nil
		}
		graded, err := gradedRun(ctx, tx, p.event)
		if err != nil {
			return err
		}
		had, err := takenAnswers(ctx, tx, p.event)
		if err != nil {
			return err
		}
		switch {
		case graded != nil:
			return fmt.Errorf("%w: run %s was graded; a graded paper is not voided", ErrState, runID)
		case len(had) > 0:
			return fmt.Errorf("%w: run %s has %d answers; only a paper no run was handed in for is voided", ErrState, runID, len(had))
		case !now.Before(p.ExpiresAt):
			return fmt.Errorf("%w: run %s lapsed at %s; it holds nothing", ErrState, runID, p.ExpiresAt.UTC().Format(time.RFC3339))
		}
		ev, err := Append(ctx, tx, Event{Stream: ExamsStream, Type: "note", Actor: "controller", Cause: p.event,
			Body: map[string]any{"text": "paper voided: " + reason, "voided": approvalID}})
		if err != nil {
			return err
		}
		out = map[string]any{"run_id": runID, "executor": p.Executor, "host": p.Host, "issued_at": p.IssuedAt.UTC().Format(time.RFC3339),
			"voided_by": approvalID, "event_id": ev.EventID}
		return nil
	})
	return out, err
}

// voidSQL finds the void of the paper issued by an event ($2) on the exams
// stream ($1).
const voidSQL = `SELECT event_id, body_json->>'voided' FROM agentrt.event WHERE event_stream_id = $1
  AND event_type = 'note' AND cause_event_id = $2 AND body_json ? 'voided'`

// voidOf is the void of the paper issued by event (its event and approval),
// or nil.
func voidOf(ctx context.Context, tx pgx.Tx, event int64) (map[string]any, error) {
	var id int64
	var approval string
	err := tx.QueryRow(ctx, voidSQL+` ORDER BY seq LIMIT 1`, ExamsStream, event).Scan(&id, &approval)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"event_id": id, "approval": approval}, nil
}
