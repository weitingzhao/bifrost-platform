package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Missions outside the standing envelope (proposal §2, design v5 §5.2): such
// a mission waits as awaiting_owner on a decision card (owner_duty goal), the
// card's question named on the state change; once the card is answered the
// controller starts the mission or stops it, and acknowledges the answer.

// The options of a mission's card, in the order the question store keys
// them (a, b): the first starts the mission (CardApplyKey), the second does
// not (CardRefuseKey).
const (
	MissionStart    = "Start the mission"
	MissionNotStart = "Do not start it"
)

// askMission puts a mission on a decision card and moves it to
// awaiting_owner, naming the card. asked is the proposal as it came, before
// any default was filled in: the card shows only that and the proposal's own
// number, so a retry of the same proposal under the same key makes the same
// card whatever the rules say by then, and two proposals make two cards
// (reviews 190 A1, 192 A1, A4). Why it needs the Owner is recorded on the
// mission. A card the question store would not take for its size is refused
// here, before it is asked, with nothing written (review 193 A5; Owner
// 2026-10-15 「决策卡 A」). It asks inside tx: a proposal that fails after
// the card was made leaves a card no mission names; its retry gets that card
// back.
func askMission(ctx context.Context, tx pgx.Tx, w Writer, key, id string, asked MissionProposal, why string, ask Ask) (string, error) {
	cardKey := canonKey(w.Actor, "mission:"+key)
	body := missionCardBody(w, cardKey, asked)
	if n := questions.BodyBytes(body); n > questions.BodyLimit {
		return "", invalid("the Owner's card for this mission would be %d bytes, more than the question store's %d: %s",
			n, questions.BodyLimit, tooLong)
	}
	q, err := ask(ctx, Card{Key: cardKey, Title: "Mission: " + firstLine(asked.Title), Body: body,
		Options: []string{MissionStart, MissionNotStart}, Meanwhile: "the mission does not start", OwnerDuty: "goal",
		Urgency: "when you can: only this mission waits for you"})
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", ErrAsk, err)
	}
	return q, moveMission(ctx, tx, "controller", id, "awaiting_owner", why, q, "")
}

// missionCardBody is the card's text: who proposes, the proposal's number
// (the first 12 hex digits of the card's key), and the proposal as it came.
func missionCardBody(w Writer, cardKey string, p MissionProposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s proposes a mission (tenant %s), proposal %s.\n\nTitle: %s\n\nGoal: %s\n\nSuccess:\n",
		w.Actor, p.Tenant, cardKey[:12], p.Title, p.Goal)
	for _, s := range p.Success {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	repos := "none named"
	if len(p.Repos) > 0 {
		repos = strings.Join(p.Repos, ", ")
	}
	fmt.Fprintf(&b, "\nRepos: %s\n", repos)
	if p.WorkID != "" {
		fmt.Fprintf(&b, "Work: %s\n", p.WorkID)
	}
	if p.NoMerge != "" {
		fmt.Fprintf(&b, "Not to be merged into main: %s\n", p.NoMerge)
	}
	if p.NeedsOwner != "" {
		fmt.Fprintf(&b, "Its cards wait for you, never answered by a rule: %s\n", p.NeedsOwner)
	}
	deadline := fmt.Sprintf("the default (%d days from when it is made)", DefaultMissionDays)
	if !p.Deadline.IsZero() {
		deadline = p.Deadline.UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(&b, "Deadline: %s\nUp to: %s\n", deadline, p.MaxAction)
	if p.MaxAttempts == 0 && p.MaxWallHours == 0 {
		b.WriteString("Budget: not stated; the tenant's signed budget rule gives it when the mission is made (the mission's record has the numbers)\n")
	} else {
		fmt.Fprintf(&b, "Budget: %d attempts, %d hours\n", p.MaxAttempts, p.MaxWallHours)
	}
	b.WriteString("\nIt needs you because it does not fit the standing envelope in force, or none is in force; " +
		"the mission's record says which bound.\n\nStarting it lets its tasks be dispatched; not starting it stops it.")
	return b.String()
}

// decidePageSize is how many waiting missions DecideMissions reads at a time
// (a test lowers it).
var decidePageSize = 100

// DecideMissions is one pass over the missions waiting on a card, page by
// page (review 190 B3): an answer starts the mission (the first option) or
// stops it (the second, or "none of these" with the Owner's words); a card
// that expired or was withdrawn stops it; an open card or one that cannot be
// read now leaves it waiting. The answer as read is kept (question_seen) in
// the transaction that acts on it, before anything acknowledges it (review
// 190 A3). It returns how many missions it moved. With ack, the answers it
// used are then acknowledged.
func DecideMissions(ctx context.Context, db DB, question func(context.Context, string) (Question, bool),
	ack func(context.Context, string) error) (int, error) {
	n := 0
	var errs []error
	after := ""
	for {
		page, err := waitingMissions(ctx, db, after)
		if err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, w := range page {
			did, derr := decideMission(ctx, db, w, question)
			if derr != nil {
				errs = append(errs, fmt.Errorf("mission %s: %w", w.mission, derr))
			}
			if did {
				n++
			}
		}
		if len(page) < decidePageSize {
			break
		}
		after = page[len(page)-1].mission
	}
	if ack != nil {
		errs = append(errs, ackMissionCards(ctx, db, ack))
	}
	return n, errors.Join(errs...)
}

type waitingMission struct{ mission, question string }

func waitingMissions(ctx context.Context, db DB, after string) ([]waitingMission, error) {
	var list []waitingMission
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT m.mission_id, s.body_json->>'question_id' FROM agentrt.mission m
		  JOIN LATERAL (SELECT e.body_json FROM agentrt.event e WHERE e.mission_id = m.mission_id
		    AND e.event_type = 'mission_state_changed' ORDER BY e.event_id DESC LIMIT 1) s ON true
		  WHERE m.state = 'awaiting_owner' AND s.body_json ? 'question_id' AND m.mission_id > $1
		  ORDER BY m.mission_id LIMIT $2`, after, decidePageSize)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (waitingMission, error) {
			var w waitingMission
			return w, r.Scan(&w.mission, &w.question)
		})
		return err
	})
	return list, err
}

// decideMission acts on one mission's card; did is whether it moved it.
func decideMission(ctx context.Context, db DB, w waitingMission, question func(context.Context, string) (Question, bool)) (bool, error) {
	q, ok := question(ctx, w.question)
	if !ok || q.Status == "open" {
		return false, nil
	}
	to, reason, answer := "stopped", fmt.Sprintf("its card %s was %s unanswered", w.question, q.Status), ""
	if q.Status == QuestionAnswered {
		answer = q.Option
		switch q.Option {
		case CardApplyKey:
			to, reason = "active", fmt.Sprintf("the Owner started it (question %s)", w.question)
		case CardRefuseKey:
			reason = fmt.Sprintf("the Owner did not start it (question %s)", w.question)
		case "none":
			// The question store always offers "none of these" with the
			// Owner's own words (review 190 A2): it does not start the
			// mission.
			reason = fmt.Sprintf("the Owner answered none of these (question %s): %s", w.question, firstLine(q.Text))
		default:
			return false, fmt.Errorf("question %s was answered %q, no option of its card", w.question, q.Option)
		}
	}
	body, _, err := questionSeenBody(w.question, q)
	if err != nil {
		return false, err
	}
	key, err := canon.Encode(map[string]any{"question": w.question})
	if err != nil {
		return false, err
	}
	did := false
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		var state string
		if serr := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`, w.mission).Scan(&state); serr != nil {
			return serr
		}
		if state != "awaiting_owner" {
			return nil // another pass decided it, or something else moved it
		}
		if _, rerr := recordIfChanged(ctx, tx, "question_seen", string(key), func(context.Context) (map[string]any, bool, error) {
			return body, true, nil
		}); rerr != nil {
			return rerr
		}
		did = true
		return moveMission(ctx, tx, "controller", w.mission, to, reason, w.question, answer)
	})
	return did && err == nil, err
}

// ackMissionCards acknowledges the answers the decisions of the last 7 days
// used (a card that expired or was withdrawn has no answer to acknowledge,
// review 190 B1), recording a note on the mission once the question store
// took it (or no longer has the question); a failure leaves it for the next
// pass.
func ackMissionCards(ctx context.Context, db DB, ack func(context.Context, string) error) error {
	type card struct{ mission, question string }
	var cards []card
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT d.mission_id, d.body_json->>'question_id' FROM agentrt.event d
		  WHERE d.event_type = 'mission_state_changed' AND d.body_json->>'from' = 'awaiting_owner' AND d.body_json ? 'question_id'
		    AND d.body_json ? 'answer'
		    AND d.at > clock_timestamp() - interval '7 days'
		    AND NOT EXISTS (SELECT 1 FROM agentrt.event n WHERE n.event_type = 'note' AND n.mission_id = d.mission_id
		      AND n.body_json->>'text' IN ('acknowledged question ' || (d.body_json->>'question_id'),
		                                  'question ' || (d.body_json->>'question_id') || ' is gone from the question store'))
		  LIMIT 100`)
		if err != nil {
			return err
		}
		cards, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (card, error) {
			var c card
			return c, r.Scan(&c.mission, &c.question)
		})
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range cards {
		text := "acknowledged question " + c.question
		if aerr := ack(ctx, c.question); errors.Is(aerr, ErrQuestionGone) {
			text = "question " + c.question + " is gone from the question store"
		} else if aerr != nil {
			errs = append(errs, fmt.Errorf("acknowledge %s: %w", c.question, aerr))
			continue
		}
		if nerr := InTx(ctx, db, func(tx pgx.Tx) error {
			_, err := Append(ctx, tx, Event{Stream: MissionStream(c.mission), Type: "note", Actor: "controller", MissionID: c.mission,
				Body: map[string]any{"text": text}})
			return err
		}); nerr != nil {
			errs = append(errs, nerr)
		}
	}
	return errors.Join(errs...)
}
