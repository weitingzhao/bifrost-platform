package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The review of a pinned task (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.8). It goes to
// PinnedReviewer alone (route.go), and its time limit follows the size of
// the commit it reviews. A review no longer asks the trust chain's
// questions: the Owner's core landing card decides a core change
// (R5:5.7-9).

// Bounds of a pinned review's time (§2.8): an hour, and half an hour more
// for every 300 lines the commit adds or deletes, at most three hours.
const (
	pinnedReviewMin   = time.Hour
	pinnedReviewMax   = 3 * time.Hour
	pinnedReviewStep  = 30 * time.Minute
	pinnedReviewLines = 300
)

// PinnedReviewLimit is the time limit of a pinned commit's review for its
// changed lines: min(3h, max(1h, 1h + ceil(lines/300) x 30m)). The budget
// rule bounds it again when it is leased.
func PinnedReviewLimit(lines int) time.Duration {
	steps := (max(lines, 0) + pinnedReviewLines - 1) / pinnedReviewLines
	return min(pinnedReviewMax, max(pinnedReviewMin, pinnedReviewMin+time.Duration(steps)*pinnedReviewStep))
}

// pinnedReview is the time limit of a pinned subject's review. It returns
// zero for any other check (the default limit). The size is what the
// result's check recorded (pinresult.go); a pinned result without it is
// reviewed as the largest.
func pinnedReview(ctx context.Context, tx pgx.Tx, subject string, spec *checkSpec) (time.Duration, error) {
	if spec.check != "review" {
		return 0, nil
	}
	var pin string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(executor_pin, '') FROM agentrt.task WHERE task_id = $1`, subject).Scan(&pin); err != nil {
		return 0, err
	}
	if pin == "" {
		return 0, nil
	}
	var lines int
	err := tx.QueryRow(ctx, `SELECT (body_json->>'changed_lines')::int FROM agentrt.event
	  WHERE attempt_id = $1 AND event_type IN ('attempt_persisted','attempt_salvaged') AND body_json ? 'changed_lines'
	  AND body_json->>'pushed_commit' = $2
	  ORDER BY event_id DESC LIMIT 1`, spec.attempt, spec.commit).Scan(&lines)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		lines = 1 << 30
	case err != nil:
		return 0, err
	}
	return PinnedReviewLimit(lines), nil
}
