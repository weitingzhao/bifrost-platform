package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The review of a pinned task (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.8, §12.2 item 3). It goes
// to PinnedReviewer alone (route.go); its time limit follows the size of the
// commit it reviews; and when the commit touches the trusted core (§4.2,
// rules.LandingHard) the reviewer is given the trust chain's fixed questions
// and answers each in its report, which the core-level approval reads.

// TrustReviewText is what the review of a commit that touches the trusted
// core asks, before anything else (§12.2 item 3). Its answers have a fixed
// shape the core-level approval checks (trustAnswered, review 694 B-02): a
// section headed "## Trust chain answers", one line per question ID. The
// text itself holds no such section and no answer line.
const TrustReviewText = "# Trust chain review\n\n" +
	"This commit changes the platform's trusted core: approvals, identity, the landing gate, or what they depend on. " +
	"Besides whether it is correct and complete, answer the four questions below. End your report with a section whose heading line is " +
	"exactly `## Trust chain answers`, holding one line per question: its ID and a colon, then Yes or No, then what in the diff shows it " +
	"(for instance a line reading `T3: No, the token is still read from ...`).\n\n" +
	"- (T1) Does it change who can approve, or what an approval needs?\n" +
	"- (T2) Does it change how the thing approved is shown to the Owner (its material, its digests, its page)?\n" +
	"- (T3) Does it change how a token or a credential is obtained, kept or checked?\n" +
	"- (T4) Does it change how the landing gate decides, or open a way around it?\n\n" +
	"A question missing, answered without Yes or No, or without what in the diff shows it counts as a failing review.\n"

// trustQuestions are the four questions' texts, which no answer may copy.
var trustQuestions = []string{
	"does it change who can approve",
	"does it change how the thing approved is shown",
	"does it change how a token or a credential is obtained",
	"does it change how the landing gate decides",
}

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
	// One read (review 698 R-05): the pin, and for an unpinned task whether
	// its review asks the trust chain — no second round trip for any review.
	var pin, repo, role, tenant string
	var core bool
	var env grantEnvelopeRow
	if err := tx.QueryRow(ctx, `SELECT COALESCE(executor_pin, ''), t.core, COALESCE(t.repo, ''), t.role, m.tenant,
	    e.rule_version, e.direction, e.effective_at, e.expires_at, e.body_text, e.body_json, e.signed_by
	  FROM agentrt.task t JOIN agentrt.mission m USING (mission_id)
	  LEFT JOIN LATERAL (SELECT r.rule_version, r.direction, r.effective_at, r.expires_at, r.body_text, r.body_json, r.signed_by FROM agentrt.rule r
	    WHERE r.rule_id = 'standing-envelope:' || m.tenant AND r.effective_at <= clock_timestamp() ORDER BY r.rule_version DESC LIMIT 1) e ON true
	  WHERE t.task_id = $1`, subject).Scan(&pin, &core, &repo, &role, &tenant,
		&env.version, &env.direction, &env.effective, &env.expires, &env.text, &env.body, &env.signedBy); err != nil {
		return 0, err
	}
	if pin == "" {
		// An ordinary core task's commit may be granted too (§12.3 last
		// item): once grants are enabled its review answers the trust chain
		// (review 694 B-02), the default limit kept. Before, as it was.
		if core && role == "implement" && rules.GrantSupported(repo) && env.enables(tenant, repo, time.Now()) {
			spec.text = TrustReviewText
		}
		return 0, nil
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
