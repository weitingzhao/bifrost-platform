package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The review of a pinned task (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.8, §12.2 item 3). It goes
// to PinnedReviewer alone (route.go); its time limit follows the size of the
// commit it reviews; and when the commit touches the trusted core (§4.2,
// rules.LandingHard) the reviewer is given the trust chain's fixed questions
// and answers each in its report, which the core-level approval reads.

// TrustReviewText is what the review of a pinned commit that touches the
// trusted core asks, before anything else (§12.2 item 3).
const TrustReviewText = "# Trust chain review\n\n" +
	"This commit changes the platform's trusted core: approvals, identity, the landing gate, or what they depend on. " +
	"Besides whether it is correct and complete, answer each question below in your report, under its number, " +
	"with what in the diff shows it:\n\n" +
	"1. Does it change who can approve, or what an approval needs?\n" +
	"2. Does it change how the thing approved is shown to the Owner (its material, its digests, its page)?\n" +
	"3. Does it change how a token or a credential is obtained, kept or checked?\n" +
	"4. Does it change how the landing gate decides, or open a way around it?\n\n" +
	"A question not answered counts as a failing review.\n"

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

// pinnedReview fits a review spec to a pinned subject: its time limit, and
// the trust chain's questions when the commit touches the trusted core. It
// returns zero for any other check (the default limit). The facts are what
// the result's check recorded (pinresult.go); a pinned result without them
// is reviewed as the largest and as touching the core.
func pinnedReview(ctx context.Context, tx pgx.Tx, subject string, spec *checkSpec) (time.Duration, error) {
	if spec.check != "review" {
		return 0, nil
	}
	var pin string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(executor_pin, '') FROM agentrt.task WHERE task_id = $1`, subject).Scan(&pin); err != nil || pin == "" {
		return 0, err
	}
	var lines int
	var hard bool
	err := tx.QueryRow(ctx, `SELECT (body_json->>'changed_lines')::int, (body_json->>'hard')::boolean FROM agentrt.event
	  WHERE attempt_id = $1 AND event_type IN ('attempt_persisted','attempt_salvaged') AND body_json ? 'changed_lines'
	  AND body_json->>'pushed_commit' = $2
	  ORDER BY event_id DESC LIMIT 1`, spec.attempt, spec.commit).Scan(&lines, &hard)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		lines, hard = 1<<30, true
	case err != nil:
		return 0, err
	}
	if hard {
		spec.text = TrustReviewText
	}
	return PinnedReviewLimit(lines), nil
}
