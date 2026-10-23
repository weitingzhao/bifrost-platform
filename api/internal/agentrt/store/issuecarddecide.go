package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Reading the Owner's answers on business issues' cards (issuecard.go;
// DESIGN-crew-intake §5.3 「答复怎么落库」): the controller reads each card
// not settled yet and, once the Owner has answered it on the Console with
// one of its options, writes the evidence in the issue's own records, as the
// runtime (controller) from his card: a priority or a point's option
// (RecordOwnerEvidence), or his confirmation of a brief that asks him
// (AnswerAsk, channel card, his words the option's label). An answer by any
// other channel (a rule's) counts for nothing; so does one on versions the
// issue has moved past. The answer is acknowledged before it is used, so a
// failure after it is retried on the next pass from the same answer, under
// the same idempotency key.

// cardGone is how long a card is waited for once the question store no
// longer has it: past a card's longest life (7 days).
const cardGone = 8 * 24 * time.Hour

// DecideIssueCards is one pass over the Owner's cards on issues that are not
// settled. It returns how many it settled.
func DecideIssueCards(ctx context.Context, db DB, question func(context.Context, string) (Question, bool), ask Ask, withdraw Withdraw,
	ack func(context.Context, string) error) (int, error) {
	var recs []*issueCardRec
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		recs, err = issueCardRecords(ctx, tx, "")
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, c := range recs {
		if c.settled {
			continue
		}
		did, err := decideIssueCard(ctx, db, c, question, ask, withdraw, ack)
		if err != nil {
			errs = append(errs, fmt.Errorf("card %s on %s: %w", c.question, c.issue, err))
		}
		if did {
			n++
		}
	}
	return n, errors.Join(errs...)
}

func decideIssueCard(ctx context.Context, db DB, c *issueCardRec, question func(context.Context, string) (Question, bool), ask Ask,
	withdraw Withdraw, ack func(context.Context, string) error) (bool, error) {
	q, ok := question(ctx, c.question)
	if !ok {
		if time.Since(c.opened) > cardGone {
			return true, settleIssueCard(ctx, db, c, "gone")
		}
		return false, nil
	}
	switch q.Status {
	case "open":
		current, err := cardCurrent(ctx, db, c)
		if err != nil || current {
			return false, err
		}
		if withdraw != nil {
			if err := withdraw(ctx, c.question, "the issue moved past the versions the card is bound to"); err != nil {
				return false, err
			}
		}
		return true, settleIssueCard(ctx, db, c, "superseded")
	case QuestionAnswered:
	default:
		return true, settleIssueCard(ctx, db, c, q.Status)
	}
	if ack != nil {
		if err := ack(ctx, c.question); err != nil && !errors.Is(err, ErrQuestionGone) {
			return false, err
		}
	}
	key := canonKey("controller", "issue-card:"+c.question)
	idx := -1
	if len(q.Option) == 1 && q.Option[0] >= 'a' && q.Option[0] <= 'd' {
		idx = int(q.Option[0] - 'a')
	}
	if q.Channel != "console" || idx < 0 || (q.Options > 0 && idx >= q.Options) {
		// Not the Owner on the Console with one of the card's options: a
		// rule's answer, or "none" from a writer before slice 1b.
		return true, settleIssueCard(ctx, db, c, "not_counted")
	}
	w := Writer{Actor: "controller", Token: "controller"}
	evidence := OwnerEvidence{IssueID: c.issue, BriefSHA256: c.brief, DesignSHA256: c.design, QuestionID: c.question,
		AnsweredBy: q.AnsweredBy, Channel: q.Channel}
	var err error
	switch c.purpose {
	case OwnerCardPriority:
		switch idx {
		case 0:
			return true, askPriorityLevel(ctx, db, c, ask)
		case 1:
			evidence.Priority = "later"
		default:
			evidence.Priority = "declined"
		}
		_, err = RecordOwnerEvidence(ctx, db, w, key, evidence)
	case OwnerCardPriorityLevel:
		evidence.Priority = []string{"p1", "p2", "p3"}[min(idx, 2)]
		_, err = RecordOwnerEvidence(ctx, db, w, key, evidence)
	case OwnerCardDesignDecision:
		evidence.Decision, evidence.Option = c.decision, string(rune('A'+idx))
		_, err = RecordOwnerEvidence(ctx, db, w, key, evidence)
	case OwnerCardReviewDispute:
		evidence.Decision, evidence.Option = "review_dispute", []string{"proceed", "stop", AnotherRound}[min(idx, 2)]
		evidence.DesignSHA256 = c.design
		_, err = RecordOwnerEvidence(ctx, db, w, key, evidence)
	case OwnerCardConfirmBrief:
		err = confirmByCard(ctx, db, c, q, key, idx)
	default:
		return true, settleIssueCard(ctx, db, c, "not_counted")
	}
	if err == nil {
		return true, nil
	}
	var stale *StaleError
	if errors.As(err, &stale) || errors.Is(err, ErrState) || errors.Is(err, ErrInvalid) {
		// Counted only on the versions it was asked on: when the issue
		// has moved past them the answer counts for nothing; when it has
		// not, the refusal is a race, and the next pass tries again.
		if current, cerr := cardCurrent(ctx, db, c); cerr != nil || current {
			return false, errors.Join(err, cerr)
		}
		return true, settleIssueCard(ctx, db, c, "stale")
	}
	return false, err
}

// confirmByCard records the Owner's answer to the brief's confirmation his
// card asked: his words are the option's label (§5.3).
func confirmByCard(ctx context.Context, db DB, c *issueCardRec, q Question, key string, idx int) error {
	v, err := IssueState(ctx, db, c.issue)
	if err != nil {
		return err
	}
	verdict, words := "confirmed", ConfirmLabel(c.brief)
	if idx != 0 {
		verdict, words = "rejected", "Not this version"
	}
	_, err = AnswerAsk(ctx, db, Writer{Actor: "controller", Token: "controller"}, key, AskAnswer{IssueID: c.issue, ExpectedVersion: v.Version,
		AskID: c.ask, Words: words, Verdict: verdict, Channel: ChannelCard, QuestionID: c.question, AnsweredBy: q.AnsweredBy,
		AnswerChannel: q.Channel})
	return err
}

// askPriorityLevel asks the priority's second step once the Owner took the
// issue on (X A): p1, p2 or p3, on the same brief; the first card is settled
// and the second recorded together.
func askPriorityLevel(ctx context.Context, db DB, c *issueCardRec, ask Ask) error {
	if current, err := cardCurrent(ctx, db, c); err != nil || !current {
		if err != nil {
			return err
		}
		return settleIssueCard(ctx, db, c, "stale")
	}
	if ask == nil {
		return fmt.Errorf("%w: the platform has no question store", ErrAsk)
	}
	card := ownerCard(c.issue, OwnerCardPriorityLevel, "", c.brief, "", 0, "")
	card.Key = canonKey("controller", "issue-card-level:"+c.question)
	q, err := askCard(ctx, ask, card)
	if err != nil {
		return err
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := noteIssueCard(ctx, tx, c.issue, "the Owner took it on on card "+c.question+"; its level is asked on card "+q,
			map[string]any{"state": "settled", "question_id": c.question, "outcome": "take"}); err != nil {
			return err
		}
		return noteIssueCard(ctx, tx, c.issue, "the runtime asked the Owner the level of its priority on card "+q,
			map[string]any{"state": "opened", "question_id": q, "purpose": OwnerCardPriorityLevel, "brief_sha256": c.brief, "after": c.question})
	})
}

// settleIssueCard records that a card's answer counts for nothing, and why.
func settleIssueCard(ctx context.Context, db DB, c *issueCardRec, outcome string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		return noteIssueCard(ctx, tx, c.issue, fmt.Sprintf("the Owner's card %s (%s) is settled: %s; it counts for nothing", c.question,
			c.purpose, outcome), map[string]any{"state": "settled", "question_id": c.question, "outcome": outcome})
	})
}

// noteIssueCard is the runtime's note on an issue's stream about a card:
// neutral words, ids and the outcome, never a text of the issue.
func noteIssueCard(ctx context.Context, tx pgx.Tx, issue, text string, card map[string]any) error {
	_, err := Append(ctx, tx, Event{Stream: IssueStream(issue), Type: "note", Actor: "controller", Body: map[string]any{"text": text, "card": card}})
	return err
}

// cardCurrent says whether a card still stands on the issue: open, not
// parked, on its current brief (and design), and for a brief's confirmation
// that ask still open.
func cardCurrent(ctx context.Context, db DB, c *issueCardRec) (bool, error) {
	current := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := readIssue(ctx, tx, c.issue, false)
		if err != nil {
			return err
		}
		switch {
		case r.open() != nil, r.briefSHA != c.brief, c.design != "" && r.designSHA != c.design:
			return nil
		case c.purpose == OwnerCardConfirmBrief:
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.issue_ask WHERE issue_ask_id = $1 AND issue_id = $2
			  AND verdict IS NULL AND superseded_at IS NULL)`, c.ask, c.issue).Scan(&current)
		}
		current = true
		return nil
	})
	return current, err
}

// afterOwnerCard is the state an issue waiting on the Owner shows once its
// card question is settled, and whom it waits on: unchanged while another of
// its cards is open; otherwise the step its evidence has reached (§4.3's
// table; the state only shows where it is). The reads apply the same rule
// (issueshown.go).
func afterOwnerCard(ctx context.Context, tx pgx.Tx, r issueRow, question string) (string, string, bool, error) {
	if r.State != "owner_deciding" {
		return "", "", false, nil
	}
	recs, err := issueCardRecords(ctx, tx, r.ID)
	if err != nil {
		return "", "", false, err
	}
	for _, c := range recs {
		if !c.settled && c.question != question {
			return "", "", false, nil
		}
	}
	if !r.BriefConfirmed {
		asked, err := openAsk(ctx, tx, r.ID, AskConfirmBrief)
		if err != nil || !asked {
			return "clarifying", "crew", err == nil, err
		}
		return "clarifying", r.waitsOn(), true, nil
	}
	var classified, reviewed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'classification'
	    AND brief_sha256 = $2),
	  EXISTS (SELECT 1 FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'review' AND brief_sha256 = $2 AND design_sha256 = $3)`,
		r.ID, r.briefSHA, r.designSHA).Scan(&classified, &reviewed); err != nil {
		return "", "", false, err
	}
	switch {
	case !classified:
		return "brief_confirmed", "crew", true, nil
	case r.designText == 0:
		return "classified", "crew", true, nil
	case !reviewed:
		return "designing", "crew", true, nil
	}
	return "in_review", "crew", true, nil
}

// isRuleChannel says whether a card was answered by a rule, which never
// counts as the Owner (§4.3).
func isRuleChannel(ch string) bool { return ch == questions.RuleChannel }
