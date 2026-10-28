package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Handling a landing card's answer and carrying the card to an end (S3 B3,
// CUT §11.6 v3 items 2, 5). The §8 choices (try_again, do_not_merge,
// confirm_main_lost) go through HandleLandingAnswer unchanged; the others
// are recorded as landing_card_outcome on the repository stream, the
// business action (a resume) in the same transaction. Handled first,
// acknowledged after, the record is what the Ack rests on.

var section8Choices = []string{"try_again", "do_not_merge", "confirm_main_lost"}

// cardOutcomeWhys are the outcome / why pairs a landing_card_outcome may
// record (CUT §11.6 v3 item 5).
var cardOutcomeWhys = map[string][]string{
	"applied":      {"repo_resumed"},
	"moot":         {"stop_superseded", "already_resumed", "trigger_gone"},
	"refused":      {"unknown_choice", "channel"},
	"held":         {"wait", "keep_stopped", "remind_later"},
	"acknowledged": {"ack"},
}

// badCardOutcome is why a landing_card_outcome body is refused, or "".
func badCardOutcome(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "landing_card_outcome is not an object"
	}
	if m["v"] != "1" {
		return "landing_card_outcome version is not 1"
	}
	for _, k := range []string{"question_id", "prep", "kind", "outcome", "why", "by", "answered_at", "channel"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_card_outcome needs " + k
		}
	}
	if _, ok := cardChoices[m["kind"].(string)]; !ok {
		return "landing_card_outcome kind is not a card kind"
	}
	whys, ok := cardOutcomeWhys[m["outcome"].(string)]
	if !ok || !slices.Contains(whys, m["why"].(string)) {
		return "landing_card_outcome why does not follow its outcome"
	}
	if _, err := time.Parse(time.RFC3339Nano, m["answered_at"].(string)); err != nil {
		return "landing_card_outcome answered_at is not a time"
	}
	_, held := m["remind_at"]
	if (m["outcome"] == "held") != held {
		return "landing_card_outcome remind_at is for a held answer only"
	}
	if held {
		if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(m["remind_at"])); err != nil {
			return "landing_card_outcome remind_at is not a time"
		}
	}
	gen, _ := cardNumber(m["stop_generation"])
	root, _ := m["root"].(string)
	if slices.Contains(repoCards, m["kind"].(string)) {
		if gen < 1 || root != "" {
			return "landing_card_outcome of the repository names its stop generation and no root"
		}
	} else if gen != 0 || !landingRoot.MatchString(root) {
		return "landing_card_outcome of a root names the root"
	}
	if m["outcome"] == "applied" && m["kind"] != CardRepoStopped {
		return "landing_card_outcome applies only a resume; the §8 choices are landing_answer"
	}
	return ""
}

// checkCardOutcomeAppend: only the controller records it, on a repository
// landing stream, under the root it names (none for a repository card).
func checkCardOutcomeAppend(ev Event) error {
	v, ok := ev.Body["landing_card_outcome"]
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	root, _ := m["root"].(string)
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != "landing:bifrost-platform" || m == nil || ev.TaskID != root {
		return fmt.Errorf("landing_card_outcome is written by the controller on the repository stream under its root")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_card_outcome is not a landing record")
	}
	return nil
}

// HandleLandingCardAnswer handles one answered landing card exactly once,
// in one transaction under the locks the decision needs (task, mission, then
// the repository stream for a root card; the repository stream for a
// repository card): a stored result for the question (either record) is
// returned as it is; otherwise the card must still apply (review 670 B-02),
// or the answer is moot and nothing else is written; then a §8 choice is
// carried out by §8's own decision (handleLandingAnswerTx) and the others
// are decided here. The letter is turned into the card's choice by the
// prepared map.
func HandleLandingCardAnswer(ctx context.Context, db DB, c CardRecord, q Question, answeredAt time.Time) (string, string, error) {
	choice, known := c.Options[q.Option]
	stream := "landing:" + c.Repo
	var outcome, why string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		outcome, why = "", ""
		mission := ""
		if c.Root != "" {
			if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, c.Root).Scan(&mission); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission); err != nil {
				return err
			}
		}
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		o, found, err := storedCardOutcome(ctx, tx, c, q.ID)
		if err != nil || found {
			outcome, why = o.outcome, o.why
			return err
		}
		applies, err := cardApplies(ctx, tx, c, DecisionAt())
		if err != nil {
			return err
		}
		if known && applies && q.Channel == "console" && slices.Contains(section8Choices, choice) {
			qq := q
			qq.Option = choice
			res, err := handleLandingAnswerTx(ctx, tx, qq, c.Root, c.Op, answeredAt)
			outcome, why = res.Outcome, res.Why
			return err
		}
		body := map[string]any{"v": "1", "question_id": q.ID, "prep": c.Prep, "kind": c.Kind, "letter": q.Option, "choice": choice,
			"by": q.AnsweredBy, "answered_at": answeredAt.UTC().Format(time.RFC3339Nano), "channel": q.Channel, "answer_text": q.Text}
		if c.Root != "" {
			body["root"], body["op"] = c.Root, c.Op
		} else {
			body["stop_generation"] = c.StopGeneration
		}
		switch {
		case q.Channel != "console":
			outcome, why = "refused", "channel"
		case !known:
			outcome, why = "refused", "unknown_choice"
		case !applies:
			outcome, why = "moot", "trigger_gone"
			if choice == "repo_resume" {
				why = "stop_superseded"
			}
		case choice == "repo_resume":
			outcome, why = "applied", "repo_resumed"
			if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
				Body: map[string]any{"text": "Landing resumed for the repository by the Owner", "landing_resume": map[string]any{"repo": c.Repo, "generation": c.StopGeneration}}}); err != nil {
				return err
			}
		case choice == "ack":
			outcome, why = "acknowledged", "ack"
		case slices.Contains([]string{"wait", "keep_stopped", "remind_later"}, choice):
			outcome, why = "held", choice
			body["remind_at"] = answeredAt.Add(CardHoldFor).UTC().Format(time.RFC3339Nano)
		default:
			outcome, why = "refused", "unknown_choice"
		}
		body["outcome"], body["why"] = outcome, why
		ev := Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": "Landing card " + c.Kind + ": " + outcome + " (" + why + ")", "landing_card_outcome": body}}
		if c.Root != "" {
			ev.TaskID, ev.MissionID = c.Root, mission
		}
		_, err = Append(ctx, tx, ev)
		return err
	})
	return outcome, why, err
}

// storedCardOutcome is the handling record of a card's question, from
// either record, checked to be this card's (review 670 R-01): a §8
// landing_answer for its root and operation, written by the controller; a
// landing_card_outcome for its prep. Another card's record is an error.
func storedCardOutcome(ctx context.Context, tx pgx.Tx, c CardRecord, questionID string) (cardOutcome, bool, error) {
	var answer, other map[string]any
	var actor string
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_answer', body_json->'landing_card_outcome', actor_id FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note'
	    AND (body_json->'landing_answer'->>'question_id' = $2 OR body_json->'landing_card_outcome'->>'question_id' = $2)
	  ORDER BY seq LIMIT 1`, "landing:"+c.Repo, questionID).Scan(&answer, &other, &actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return cardOutcome{}, false, nil
	}
	if err != nil {
		return cardOutcome{}, false, err
	}
	m := other
	switch {
	case actor != "controller":
		return cardOutcome{}, false, fmt.Errorf("landing card %s: question %s has a record not the controller's", c.Prep, questionID)
	case answer != nil && other != nil:
		return cardOutcome{}, false, fmt.Errorf("landing card %s: question %s has two records in one", c.Prep, questionID)
	case answer != nil:
		if why := badLandingAnswer(answer); why != "" || answer["root"] != c.Root || answer["op"] != c.Op {
			return cardOutcome{}, false, fmt.Errorf("landing card %s: question %s was recorded for %v %v (%s)", c.Prep, questionID, answer["root"], answer["op"], why)
		}
		m = answer
	default:
		if why := badCardOutcome(other); why != "" || other["prep"] != c.Prep {
			return cardOutcome{}, false, fmt.Errorf("landing card %s: question %s was recorded for %v (%s)", c.Prep, questionID, other["prep"], why)
		}
	}
	o := cardOutcome{}
	o.outcome, _ = m["outcome"].(string)
	o.why, _ = m["why"].(string)
	if s, _ := m["remind_at"].(string); s != "" {
		o.remindAt, _ = time.Parse(time.RFC3339Nano, s)
	}
	return o, true, nil
}

// cardApplies says whether a card's trigger still applies, read under the
// caller's locks: the repository's newest stop is this generation and not
// resumed, or the trigger is still listed with this kind.
func cardApplies(ctx context.Context, tx pgx.Tx, c CardRecord, now time.Time) (bool, error) {
	triggers, err := LandingCardTriggers(ctx, tx, c.Repo, now)
	if err != nil {
		return false, err
	}
	for _, tr := range triggers {
		if tr.Seq == c.TriggerSeq && tr.Kind == c.Kind && tr.StopGeneration == c.StopGeneration {
			return true, nil
		}
	}
	return false, nil
}

// SettleLandingCard carries one bound card one step toward its end (CUT
// §11.6 v3 item 2): handled → acknowledged; answered → handled; open while
// its trigger no longer applies → withdrawn; expired, withdrawn or gone →
// closed. A read that fails writes nothing. It returns what it did.
func SettleLandingCard(ctx context.Context, db DB, q CardQuestions, c CardRecord, now time.Time) (string, error) {
	var handled bool
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		_, found, err := storedCardOutcome(ctx, tx, c, c.QuestionID)
		handled = found
		return err
	}); err != nil {
		return "", err
	}
	ack := func() (string, error) {
		if q.Ack == nil {
			return "", nil // handled; acknowledged once the store can be told
		}
		found, err := q.Ack(ctx, c.QuestionID)
		if err != nil {
			return "", err
		}
		if !found {
			return "gone_after_handled", closeCard(ctx, db, c.Repo, c, "gone_after_handled")
		}
		return "acked", ackedCard(ctx, db, c.Repo, c)
	}
	if handled {
		return ack()
	}
	if q.Lookup == nil {
		return "", nil
	}
	qq, found, err := q.Lookup(ctx, c.QuestionID)
	if err != nil {
		return "", err
	}
	if !found {
		return "lost", closeCard(ctx, db, c.Repo, c, "lost")
	}
	switch qq.Status {
	case QuestionAnswered:
		at, err := time.Parse(time.RFC3339Nano, qq.AnsweredAt)
		if err != nil || qq.AnsweredBy == "" {
			return "", fmt.Errorf("landing card %s: an answer without who or when", c.Prep)
		}
		if _, _, err := HandleLandingCardAnswer(ctx, db, c, qq, at); err != nil {
			return "", err
		}
		return ack()
	case "expired":
		return "expired", closeCard(ctx, db, c.Repo, c, "expired")
	case "withdrawn":
		return "withdrawn", closeCard(ctx, db, c.Repo, c, "withdrawn")
	case "open":
		var applies bool
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			var err error
			applies, err = cardApplies(ctx, tx, c, now)
			return err
		}); err != nil || applies || q.Withdraw == nil {
			return "", err
		}
		status, err := q.Withdraw(ctx, c.QuestionID, "the landing it asks about has moved on")
		if err != nil {
			return "", err
		}
		switch status {
		case "withdrawn":
			return "resolved", closeCard(ctx, db, c.Repo, c, "resolved")
		case "expired":
			return "expired", closeCard(ctx, db, c.Repo, c, "expired")
		case "gone":
			return "lost", closeCard(ctx, db, c.Repo, c, "lost")
		}
		return "", nil // answered meanwhile: the next pass handles it
	}
	return "", nil
}
