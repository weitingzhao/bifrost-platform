package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"slices"
	"strings"
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
	"applied":      {"repo_resumed", "not_delivered", "counted_delivered"},
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
	if m["outcome"] == "applied" {
		ok := map[string][]string{CardRepoStopped: {"repo_resumed"}, CardLandedUnverified: {"not_delivered"}, CardInMain: {"not_delivered", "counted_delivered"}}[m["kind"].(string)]
		if !slices.Contains(ok, m["why"].(string)) {
			return "landing_card_outcome applies only a resume or an Owner's delivery decision; the §8 choices are landing_answer"
		}
	}
	if seq, ok := cardNumber(m["trigger_seq"]); !ok || seq < 1 {
		return "landing_card_outcome needs trigger_seq"
	}
	if op, _ := m["op"].(string); (root != "") != (op != "") {
		return "landing_card_outcome of a root names its operation"
	}
	return badCardOutcomeChoice(m)
}

// badCardOutcomeChoice: the outcome follows the channel, the choice and
// the kind (review 672 R-03).
func badCardOutcomeChoice(m map[string]any) string {
	kind, outcome, why := m["kind"].(string), m["outcome"].(string), m["why"].(string)
	choice, _ := m["choice"].(string)
	known := slices.ContainsFunc(cardChoices[kind], func(set []string) bool { return slices.Contains(set, choice) })
	switch {
	case m["channel"] != "console":
		if outcome != "refused" || why != "channel" {
			return "landing_card_outcome off the Console is refused for its channel"
		}
		return ""
	case !known:
		if outcome != "refused" || why != "unknown_choice" {
			return "landing_card_outcome of a choice the card does not offer is refused"
		}
		return ""
	case outcome == "refused":
		return "landing_card_outcome refuses only an unknown choice or another channel"
	case outcome == "moot":
		if (why == "stop_superseded" || why == "already_resumed") != (choice == "repo_resume") {
			return "landing_card_outcome moot reason does not follow its choice"
		}
	case slices.Contains(section8Choices, choice):
		return "landing_card_outcome carries a §8 choice only as moot; §8 is landing_answer"
	case outcome == "applied":
		if map[string]string{"repo_resume": "repo_resumed", "confirm_not_delivered": "not_delivered", "count_as_delivered": "counted_delivered"}[choice] != why {
			return "landing_card_outcome applied reason does not follow its choice"
		}
	case outcome == "acknowledged":
		if choice != "ack" {
			return "landing_card_outcome acknowledges only ack"
		}
	case outcome == "held":
		if choice != why {
			return "landing_card_outcome held reason is its choice"
		}
		at, _ := time.Parse(time.RFC3339Nano, m["answered_at"].(string))
		remind, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(m["remind_at"]))
		if !remind.Equal(at.Add(CardHoldFor)) {
			return "landing_card_outcome held reminds a hold after its answer"
		}
	}
	return ""
}

// checkCardOutcomeAppend: only the controller records it, on a repository
// landing stream, under the root it names (none for a repository card), for
// the card bound to its question (same kind, root, operation and trigger),
// once per question across both handling records (review 672 R-03). A §8
// landing_answer is refused for a question a card outcome already handled.
func checkCardOutcomeAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	_, answer := ev.Body["landing_answer"]
	_, outcome := ev.Body["landing_card_outcome"]
	if answer && outcome {
		return fmt.Errorf("one handling record per note: landing_answer or landing_card_outcome") // review 675 R-03
	}
	if a, ok := ev.Body["landing_answer"].(map[string]any); ok {
		// One handling record per question, of either kind (review 675 R-03).
		var other bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND (body_json->'landing_card_outcome'->>'question_id' = $2 OR body_json->'landing_answer'->>'question_id' = $2))`, ev.Stream, fmt.Sprint(a["question_id"])).Scan(&other); err != nil {
			return err
		}
		if other {
			return fmt.Errorf("question %v is handled once", a["question_id"])
		}
	}
	v, ok := ev.Body["landing_card_outcome"]
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	root, _ := m["root"].(string)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID != root {
		return fmt.Errorf("landing_card_outcome is written by the controller on the repository stream under its root")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_card_outcome is not a landing record")
	}
	qid := fmt.Sprint(m["question_id"])
	var handled bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND (body_json->'landing_card_outcome'->>'question_id' = $2 OR body_json->'landing_answer'->>'question_id' = $2))`, ev.Stream, qid).Scan(&handled); err != nil {
		return err
	}
	if handled {
		return fmt.Errorf("question %s is handled once", qid)
	}
	cards, err := LandingCards(ctx, tx, CardsStream(strings.TrimPrefix(ev.Stream, "landing:")))
	if err != nil {
		return err
	}
	c := cards[fmt.Sprint(m["prep"])]
	seq, _ := cardNumber(m["trigger_seq"])
	op, _ := m["op"].(string)
	gen, _ := cardNumber(m["stop_generation"])
	if !c.Bound || c.QuestionID != qid || c.Kind != m["kind"] || c.Root != root || c.Op != op || c.TriggerSeq != seq || c.StopGeneration != gen {
		return fmt.Errorf("landing_card_outcome names its card's bound question, kind, root, operation and trigger")
	}
	// The choice is the one this card offered under its letter, not just a
	// choice of its kind (review 675 R-03); an unknown letter has no choice.
	letter, _ := m["letter"].(string)
	choice, _ := m["choice"].(string)
	if offered, ok := c.Options[letter]; ok {
		if choice != offered {
			return fmt.Errorf("landing_card_outcome choice %q is not what the card offered as %q", choice, letter)
		}
	} else if choice != "" {
		return fmt.Errorf("landing_card_outcome letter %q is not one the card offered", letter)
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
		dispose := false
		body := map[string]any{"v": "1", "question_id": q.ID, "prep": c.Prep, "kind": c.Kind, "trigger_seq": c.TriggerSeq, "letter": q.Option, "choice": choice,
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
		case choice == "confirm_not_delivered" || choice == "count_as_delivered":
			outcome, why = "applied", "not_delivered"
			if choice == "count_as_delivered" {
				why = "counted_delivered"
			}
			// The disposition follows its answer's record, below.
			dispose = true
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
		if _, err = Append(ctx, tx, ev); err != nil || !dispose {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: c.Root, MissionID: mission,
			Body: map[string]any{"text": "Landing counted by the Owner: " + why, "landing_disposed": map[string]any{"v": "1", "root": c.Root, "op": c.Op,
				"trigger_seq": c.TriggerSeq, "question_id": q.ID, "why": map[string]string{"not_delivered": "owner", "counted_delivered": "owner_delivered"}[why]}}})
		return err
	})
	return outcome, why, err
}

// storedCardOutcome is the handling record of a card's question, from
// either record, checked to be this card's (review 670 R-01): a §8
// landing_answer for its root and operation, written by the controller; a
// landing_card_outcome for its prep. Another card's record is an error.
func storedCardOutcome(ctx context.Context, tx pgx.Tx, c CardRecord, questionID string) (cardOutcome, bool, error) {
	o, _, found, err := storedCardRecord(ctx, tx, c, questionID)
	return o, found, err
}

// storedCardRecord is storedCardOutcome with the record itself. Two records
// for one question are an error (review 672 R-03).
func storedCardRecord(ctx context.Context, tx pgx.Tx, c CardRecord, questionID string) (cardOutcome, map[string]any, bool, error) {
	rows, err := tx.Query(ctx, `SELECT body_json->'landing_answer', body_json->'landing_card_outcome', actor_id, COALESCE(task_id, '') FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note'
	    AND (body_json->'landing_answer'->>'question_id' = $2 OR body_json->'landing_card_outcome'->>'question_id' = $2)
	  ORDER BY seq LIMIT 2`, "landing:"+c.Repo, questionID)
	if err != nil {
		return cardOutcome{}, nil, false, err
	}
	type rec struct {
		answer, other map[string]any
		actor, task   string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.answer, &r.other, &r.actor, &r.task); err != nil {
			rows.Close()
			return cardOutcome{}, nil, false, err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cardOutcome{}, nil, false, err
	}
	switch len(recs) {
	case 0:
		return cardOutcome{}, nil, false, nil
	case 1:
	default:
		return cardOutcome{}, nil, false, fmt.Errorf("landing card %s: question %s has two handling records", c.Prep, questionID)
	}
	r := recs[0]
	m := r.other
	switch {
	case r.actor != "controller":
		return cardOutcome{}, nil, false, fmt.Errorf("landing card %s: question %s has a record not the controller's", c.Prep, questionID)
	case r.answer != nil && r.other != nil:
		return cardOutcome{}, nil, false, fmt.Errorf("landing card %s: question %s has two records in one", c.Prep, questionID)
	case r.answer != nil:
		if why := badLandingAnswer(r.answer); why != "" || r.answer["root"] != c.Root || r.answer["op"] != c.Op || r.task != c.Root {
			return cardOutcome{}, nil, false, fmt.Errorf("landing card %s: question %s was recorded for %v %v (%s)", c.Prep, questionID, r.answer["root"], r.answer["op"], why)
		}
		m = r.answer
	default:
		root, _ := r.other["root"].(string)
		op, _ := r.other["op"].(string)
		seq, _ := cardNumber(r.other["trigger_seq"])
		gen, _ := cardNumber(r.other["stop_generation"])
		if why := badCardOutcome(r.other); why != "" || r.other["prep"] != c.Prep || r.other["kind"] != c.Kind || root != c.Root || op != c.Op || r.task != c.Root ||
			seq != c.TriggerSeq || gen != c.StopGeneration {
			return cardOutcome{}, nil, false, fmt.Errorf("landing card %s: question %s was recorded for %v (%s)", c.Prep, questionID, r.other["prep"], why)
		}
	}
	o := cardOutcome{}
	o.outcome, _ = m["outcome"].(string)
	o.why, _ = m["why"].(string)
	if s, _ := m["remind_at"].(string); s != "" {
		o.remindAt, _ = time.Parse(time.RFC3339Nano, s)
	}
	return o, m, true, nil
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

// badLandingDisposed is why a landing_disposed body is refused, or "".
func badLandingDisposed(v any) string {
	m, ok := v.(map[string]any)
	if !ok || m["v"] != "1" {
		return "landing_disposed is not a v1 object"
	}
	if !landingRoot.MatchString(fmt.Sprint(m["root"])) {
		return "landing_disposed root is not a task"
	}
	for _, k := range []string{"op", "question_id"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_disposed needs " + k
		}
	}
	if n, ok := cardNumber(m["trigger_seq"]); !ok || n < 1 {
		return "landing_disposed needs trigger_seq"
	}
	if w := fmt.Sprint(m["why"]); w != "owner" && w != "owner_delivered" {
		return "landing_disposed why is owner or owner_delivered"
	}
	return ""
}

// checkLandingDisposedAppend: the controller records it on the repository
// stream under its root, right after the applied card outcome it follows:
// same question, root, operation and trigger, why as the Owner chose
// (review 672 R-03). A disposition with no such answer is refused.
func checkLandingDisposedAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	v, ok := ev.Body["landing_disposed"]
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) || m == nil || ev.TaskID != fmt.Sprint(m["root"]) {
		return fmt.Errorf("landing_disposed is written by the controller on the repository stream under its root")
	}
	var o map[string]any
	var actor string
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_card_outcome', actor_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing_card_outcome'->>'question_id' = $2 ORDER BY seq LIMIT 1`, ev.Stream, fmt.Sprint(m["question_id"])).Scan(&o, &actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("landing_disposed follows the Owner's applied answer")
	}
	if err != nil {
		return err
	}
	seq, _ := cardNumber(m["trigger_seq"])
	oseq, _ := cardNumber(o["trigger_seq"])
	want := map[string]string{"not_delivered": "owner", "counted_delivered": "owner_delivered"}[fmt.Sprint(o["why"])]
	if actor != "controller" || o["outcome"] != "applied" || o["root"] != m["root"] || o["op"] != m["op"] || seq != oseq || want == "" || want != m["why"] {
		return fmt.Errorf("landing_disposed follows the Owner's applied answer for its root and operation")
	}
	return nil
}
