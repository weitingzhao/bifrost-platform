package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// The landing card engine (S3 B3, CUT §11.6 v3): prepare a card for each
// trigger that applies, ask and bind it under the card stream's lock, and
// carry every bound card to an end. The question store is reached only
// through CardQuestions; every write here is a card record or a handling
// record, never a business action outside the handlers.

// CardQuestions is the landing cards' own reach into the question store
// (CUT §11.6 v3 item 5): a raw ask with the frozen request, a lookup by key
// that changes nothing, an authoritative read that sweeps expiry, a
// withdrawal that says what became of the card, an acknowledgement.
type CardQuestions struct {
	// AskRaw asks the request as stored; it returns the question and when
	// the store created it.
	AskRaw func(ctx context.Context, key string, request map[string]any) (id string, created time.Time, err error)
	// LookupKey finds the protected question (open, or answered and unread)
	// under key; found false only when the store holds none.
	LookupKey func(ctx context.Context, key string) (q Question, created time.Time, found bool, err error)
	// Lookup reads a question by id after the store's sweep (an expired
	// open question reads expired); found false only when the store holds
	// no such question.
	Lookup func(ctx context.Context, id string) (q Question, found bool, err error)
	// Withdraw takes back an open question; it returns the question's
	// status after the call ("withdrawn", "answered", "expired", or "gone").
	Withdraw func(ctx context.Context, id, reason string) (status string, err error)
	// Ack acknowledges an answered question; found false when it is gone.
	Ack func(ctx context.Context, id string) (found bool, err error)
}

// Card limits (CUT §11.6 v2 item 4, v3 item 6).
const (
	CardTTLHours      = 72
	CardCooldown      = 6 * time.Hour
	CardDailyLimit    = 4
	CardHoldFor       = 24 * time.Hour
	CardRecoverWindow = 7 * 24 * time.Hour
	cardTemplate      = 1
)

// cardKey is a card's idempotency key in the question store: the canonical
// identity, never its text.
func cardKey(repo, kind string, triggerSeq int64, n, template int) string {
	sum, _ := canon.Sum(map[string]any{"route": "landing-card", "repo": repo, "kind": kind, "trigger_seq": triggerSeq, "n": n, "template": template})
	return strings.TrimPrefix(sum, "sha256:")
}

// cardChoiceLabels are the options as the Owner reads them.
var cardChoiceLabels = map[string]string{
	"try_again":             "Try again",
	"do_not_merge":          "Do not merge it",
	"wait":                  "Decide later (ask again in 24 hours)",
	"confirm_main_lost":     "Confirm it is lost from main",
	"ack":                   "Noted",
	"remind_later":          "Remind me in 24 hours",
	"repo_resume":           "Resume landings (does not retry the failed push or release anything)",
	"keep_stopped":          "Keep stopped (ask again in 24 hours)",
	"confirm_not_delivered": "Count it as not delivered",
}

// cardRequest is the frozen request a trigger's card asks: title, body,
// options in order, its task, 72 hours, free text allowed.
func cardRequest(repo string, tr CardTrigger, title string) map[string]any {
	labels := make([]any, 0, len(tr.Choices))
	for _, c := range tr.Choices {
		labels = append(labels, cardChoiceLabels[c])
	}
	body := landingCardBody(repo, tr)
	req := map[string]any{"title": title, "body": body, "options": labels, "ttl_hours": CardTTLHours, "allow_text": true,
		"urgency": "when you can: only this landing waits for you", "owner_duty": "goal"}
	if tr.Root != "" {
		req["task_id"] = tr.Root
	}
	return req
}

func landingCardBody(repo string, tr CardTrigger) string {
	at := tr.At.UTC().Format(time.RFC3339)
	switch tr.Kind {
	case CardRootFailed:
		return fmt.Sprintf("Repository %s: the landing of this task (operation %s) failed at %s: %s. Try again replays it on today's main; do not merge ends it.", repo, tr.Op, at, tr.Why)
	case CardFloor:
		return fmt.Sprintf("Repository %s: this task's change touches what no landing rule can allow (%s, %s). It can only be ended: do not merge, or decide later.", repo, tr.Op, at)
	case CardOkMissing:
		return fmt.Sprintf("Repository %s: the push of %s was reported accepted at %s, but main does not show it. Confirm it is lost to end it, or decide later.", repo, tr.Op, at)
	case CardLateLanding:
		return fmt.Sprintf("Repository %s: %s was ended as never sent, but main now has it (seen %s). Nothing is undone; this is for you to know.", repo, tr.Op, at)
	case CardRepoStopped:
		return fmt.Sprintf("Repository %s: landings stopped at %s after a push to main was denied (stop %d). Resume only lets new landings run.", repo, at, tr.StopGeneration)
	case CardCIStuck:
		return fmt.Sprintf("Repository %s: the CI of %s has run for 30 minutes (since %s). The landing waits; this is for you to know.", repo, tr.Op, at)
	case CardParkedLong:
		return fmt.Sprintf("Repository %s: %s has been parked since %s (%s). It ends by itself at its six-hour bound.", repo, tr.Op, at, tr.Why)
	}
	return fmt.Sprintf("Repository %s: %s (%s).", repo, tr.Kind, at)
}

// cardTitle names a card.
func cardTitle(ctx context.Context, tx pgx.Tx, repo string, tr CardTrigger) (string, error) {
	if tr.Root == "" {
		return "Landings stopped in " + repo, nil
	}
	var title string
	if err := tx.QueryRow(ctx, `SELECT title FROM agentrt.task WHERE task_id = $1`, tr.Root).Scan(&title); err != nil {
		return "", err
	}
	verb := map[string]string{CardRootFailed: "Landing failed", CardFloor: "Cannot land", CardOkMissing: "Push not in main",
		CardLateLanding: "Landed late", CardCIStuck: "CI running long", CardParkedLong: "Landing parked"}[tr.Kind]
	return verb + ": " + firstLine(title), nil
}

// cardOutcome is how one card's answer was handled, from the repository
// stream (a §8 landing_answer or a landing_card_outcome).
type cardOutcome struct {
	outcome, why string
	remindAt     time.Time
}

// cardOutcomes reads the handling records of these questions.
func cardOutcomes(ctx context.Context, tx pgx.Tx, repo string, ids []string) (map[string]cardOutcome, error) {
	out := map[string]cardOutcome{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT COALESCE(body_json->'landing_answer', body_json->'landing_card_outcome') FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note' AND COALESCE(body_json->'landing_answer'->>'question_id', body_json->'landing_card_outcome'->>'question_id') = ANY($2)`,
		"landing:"+repo, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m map[string]any
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		o := cardOutcome{}
		o.outcome, _ = m["outcome"].(string)
		o.why, _ = m["why"].(string)
		if s, _ := m["remind_at"].(string); s != "" {
			o.remindAt, _ = time.Parse(time.RFC3339Nano, s)
		}
		out[fmt.Sprint(m["question_id"])] = o
	}
	return out, rows.Err()
}

// PrepareLandingCards writes a prepared card for each trigger that applies
// and may have one now (CUT §11.6 v3 items 1, 6): no card of the trigger
// still to settle; not acknowledged; not held past now; six hours since its
// last card was made and at most four made in 24 hours. Under the card
// stream's lock. It returns the cards it prepared.
func PrepareLandingCards(ctx context.Context, db DB, repo string, now time.Time) ([]string, error) {
	var prepared []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		prepared = nil
		if err := lockStream(ctx, tx, CardsStream(repo)); err != nil {
			return err
		}
		triggers, err := LandingCardTriggers(ctx, tx, repo, now)
		if err != nil {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		var ids []string
		for _, c := range cards {
			if c.QuestionID != "" {
				ids = append(ids, c.QuestionID)
			}
		}
		outcomes, err := cardOutcomes(ctx, tx, repo, ids)
		if err != nil {
			return err
		}
		for _, tr := range triggers {
			var mine []CardRecord
			for _, c := range cards {
				if c.TriggerSeq == tr.Seq && c.Kind == tr.Kind {
					mine = append(mine, c)
				}
			}
			if !cardDue(mine, outcomes, now) {
				continue
			}
			title, err := cardTitle(ctx, tx, repo, tr)
			if err != nil {
				return err
			}
			opts, err := cardOptions(tr.Kind, tr.Choices)
			if err != nil {
				return err
			}
			n := len(mine) + 1
			req := cardRequest(repo, tr, title)
			fp, err := canon.Sum(req)
			if err != nil {
				return err
			}
			prep := CardPrepID(tr.Kind, tr.Seq, n)
			body := map[string]any{"prep": prep, "repo": repo, "kind": tr.Kind, "trigger_seq": tr.Seq, "n": n, "template_version": cardTemplate,
				"key": cardKey(repo, tr.Kind, tr.Seq, n, cardTemplate), "request": req, "request_fp": strings.TrimPrefix(fp, "sha256:"), "options": opts}
			if tr.Kind == CardRepoStopped {
				body["stop_generation"] = tr.StopGeneration
			} else {
				body["root"], body["op"] = tr.Root, tr.Op
			}
			if err := appendCardRecord(ctx, tx, repo, "landing_card_prepared", body); err != nil {
				return err
			}
			prepared = append(prepared, prep)
		}
		return nil
	})
	return prepared, err
}

// cardDue says whether a trigger with these cards may have a new one now.
func cardDue(mine []CardRecord, outcomes map[string]cardOutcome, now time.Time) bool {
	made := 0
	var last time.Time
	for _, c := range mine {
		if !c.Ended() {
			return false // one card to settle at a time
		}
		o, handled := outcomes[c.QuestionID]
		switch {
		case handled && (o.outcome == "acknowledged" || o.outcome == "applied"):
			return false // known, or carried out: the trigger needs no more cards
		case handled && o.outcome == "held" && now.Before(o.remindAt):
			return false
		}
		created := c.CreatedAt
		if created.IsZero() {
			created = c.BoundAt
		}
		if created.IsZero() {
			continue
		}
		if created.After(last) {
			last = created
		}
		if now.Sub(created) < 24*time.Hour {
			made++
		}
	}
	if made >= CardDailyLimit {
		return false
	}
	// The cooldown follows a card that ended unanswered or refused; a held
	// answer is asked again at its reminder.
	if !last.IsZero() && now.Before(last.Add(CardCooldown)) {
		for _, c := range mine {
			if o, ok := outcomes[c.QuestionID]; ok && o.outcome == "held" && !now.Before(o.remindAt) {
				return true
			}
		}
		return false
	}
	return true
}

// ErrCardAskUnconfirmed: an ask started too long ago to know whether the
// Owner answered a card the store has since trimmed.
var ErrCardAskUnconfirmed = errors.New("landing card: ask unconfirmed past the recovery window")

// AskLandingCard carries one prepared, unbound card to a binding (CUT
// §11.6 v3 items 1–2), under the card stream's lock so two recoverers run
// one after the other: it marks the ask started, looks the key up without
// side effects, asks when the store holds no protected card under it (and
// the ask started less than seven days ago), and binds the question. It
// returns the question bound, or "" with nothing bound.
func AskLandingCard(ctx context.Context, db DB, q CardQuestions, repo, prep string, now time.Time) (string, error) {
	if q.AskRaw == nil || q.LookupKey == nil {
		return "", fmt.Errorf("landing card: no question store to ask")
	}
	// The ask's start is its own commit: a crash after the Ask leaves the
	// mark, and the lookup below finds what the Ask made.
	var c CardRecord
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, CardsStream(repo)); err != nil {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		c = cards[prep]
		if c.Prep == "" || c.Bound || c.Ended() || c.AskStarted {
			return nil
		}
		if err := appendCardRecord(ctx, tx, repo, "landing_card_ask_started", map[string]any{"prep": prep}); err != nil {
			return err
		}
		c.AskStarted, c.AskStartedAt = true, now
		return nil
	})
	if err != nil || c.Prep == "" || c.Bound || c.Ended() {
		return "", err
	}
	bound := ""
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		bound = ""
		if err := lockStream(ctx, tx, CardsStream(repo)); err != nil {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		c = cards[prep]
		if c.Prep == "" || c.Bound || c.Ended() || !c.AskStarted {
			return nil // another recoverer bound it, or it ended
		}
		found, created, ok, err := q.LookupKey(ctx, c.Key)
		if err != nil {
			return err
		}
		id := ""
		switch {
		case ok:
			id = found.ID
		case !now.Before(c.AskStartedAt.Add(CardRecoverWindow)):
			// An answer may have been trimmed: say so, never ask again under it.
			return appendCardRecord(ctx, tx, repo, "landing_card_closed", map[string]any{"prep": prep, "why": "unconfirmed"})
		default:
			id, created, err = q.AskRaw(ctx, c.Key, c.Request)
			if err != nil {
				return err
			}
		}
		if id == "" {
			return fmt.Errorf("landing card %s: the question store gave no id", prep)
		}
		body := map[string]any{"prep": prep, "question_id": id}
		if !created.IsZero() {
			body["created_at"] = created.UTC().Format(time.RFC3339Nano)
		}
		if err := appendCardRecord(ctx, tx, repo, "landing_card", body); err != nil {
			return err
		}
		bound = id
		return nil
	})
	return bound, err
}

// LandingCardsToSettle are the cards still to carry to an end: prepared and
// not bound (to ask), bound and not ended (to read, handle, ack, withdraw).
func LandingCardsToSettle(ctx context.Context, db DB, repo string) (ask, bound []CardRecord, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		ask, bound = nil, nil
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		for _, c := range cards {
			switch {
			case c.Ended():
			case c.Bound:
				bound = append(bound, c)
			default:
				ask = append(ask, c)
			}
		}
		slices.SortFunc(ask, func(a, b CardRecord) int { return strings.Compare(a.Prep, b.Prep) })
		slices.SortFunc(bound, func(a, b CardRecord) int { return strings.Compare(a.Prep, b.Prep) })
		return nil
	})
	return ask, bound, err
}

// closeCard ends a bound card for why, under the card stream's lock.
func closeCard(ctx context.Context, db DB, repo string, c CardRecord, why string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, CardsStream(repo)); err != nil {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		if now := cards[c.Prep]; now.Ended() {
			return nil
		}
		return appendCardRecord(ctx, tx, repo, "landing_card_closed", map[string]any{"prep": c.Prep, "question_id": c.QuestionID, "why": why})
	})
}

// ackedCard records an acknowledgement, under the card stream's lock.
func ackedCard(ctx context.Context, db DB, repo string, c CardRecord) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, CardsStream(repo)); err != nil {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		if now := cards[c.Prep]; now.Ended() {
			return nil
		}
		return appendCardRecord(ctx, tx, repo, "landing_card_acked", map[string]any{"prep": c.Prep, "question_id": c.QuestionID})
	})
}
