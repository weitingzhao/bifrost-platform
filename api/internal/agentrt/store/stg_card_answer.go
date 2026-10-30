package store

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
)

// The answer and its business records commit together. It changes only
// ledger budget/disposition/quarantine; it creates no approval or release.
func handleStgCardAnswer(ctx context.Context, db DB, c CardRecord, q Question, at time.Time) (string, string, error) {
	var outcome, why string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		outcome, why = "", ""
		enabled, err := lockStgCardPolicy(ctx, tx, c.Repo)
		if err != nil || !enabled {
			return err
		}
		cards, err := LandingCards(ctx, tx, CardsStream(c.Repo))
		if err != nil {
			return err
		}
		stored := cards[c.Prep]
		if !stored.Bound || stored.QuestionID != q.ID || c.QuestionID != q.ID || stored.Family != c.Family || stored.Kind != c.Kind || stored.Block != c.Block || stored.Batch != c.Batch || stored.TriggerSeq != c.TriggerSeq || !reflect.DeepEqual(stored.Options, c.Options) {
			return fmt.Errorf("STG answer must name its durable bound card")
		}
		s, err := StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		roots := stgCardRoots(s, c)
		bindings, err := stgBindingsForRoots(ctx, tx, roots)
		if err != nil {
			return err
		}
		if err = stgLockBindings(ctx, tx, bindings); err != nil {
			return err
		}
		if err = lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		again, err := stgBindingsForRoots(ctx, tx, roots)
		if err != nil {
			return err
		}
		if !sameStgBindings(bindings, again) {
			return ErrLandingBindingChanged
		}
		old, found, err := storedCardOutcome(ctx, tx, stored, q.ID)
		if err != nil || found {
			outcome, why = old.outcome, old.why
			return err
		}
		if stored.Ended() || q.Status != QuestionAnswered || q.AnsweredBy == "" || at.IsZero() {
			return fmt.Errorf("STG card needs an answered, unhandled bound question")
		}
		s, err = StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		body, records := stgCardDecision(s, stored, q, at)
		outcome, why = valueString(body["outcome"]), valueString(body["why"])
		if _, err = Append(ctx, tx, Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{
			"text": "STG card " + c.Kind + ": " + outcome + " (" + why + ")", "landing_card_outcome": body}}); err != nil {
			return err
		}
		for _, r := range records {
			if _, err = appendStg(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	return outcome, why, err
}

func stgCardRoots(s StgState, c CardRecord) map[string]bool {
	roots := map[string]bool{}
	r := s.Records[c.TriggerSeq].Record
	if c.Batch != "" {
		if b := s.Batches[c.Batch]; b != nil {
			for m := range b.Members {
				roots[m.Root] = true
			}
		}
	} else {
		if r.String("kind") == "blocked" {
			r = r.object("detail")
		}
		for _, m := range r.members("members") {
			roots[m.Root] = true
		}
	}
	return roots
}

func stgCardDecision(s StgState, c CardRecord, q Question, at time.Time) (map[string]any, []StgRecord) {
	choice, known := c.Options[q.Option]
	tr, applies := matchingStgTrigger(s, c)
	body := map[string]any{"v": "1", "family": "stg", "question_id": q.ID, "prep": c.Prep, "kind": c.Kind, "trigger_seq": c.TriggerSeq,
		"letter": q.Option, "choice": choice, "by": q.AnsweredBy, "owner_answer": q.OwnerAnswer, "answered_at": at.UTC().Format(time.RFC3339Nano), "channel": q.Channel, "answer_text": q.Text}
	if c.Block != "" {
		body["block"] = c.Block
	}
	if c.Batch != "" {
		body["batch"] = c.Batch
	}
	outcome, why := "moot", "trigger_gone"
	var records []StgRecord
	switch {
	case q.Channel != "console":
		outcome, why = "refused", "channel"
	case !q.OwnerAnswer:
		outcome, why = "refused", "actor"
	case !known:
		outcome, why = "refused", "unknown_choice"
	case !applies:
	case choice == "ack":
		outcome, why = "acknowledged", "ack"
	case choice == "remind_later":
		outcome, why = "held", "remind_later"
		body["remind_at"] = at.Add(CardHoldFor).UTC().Format(time.RFC3339Nano)
	case choice == "retry_release" || choice == "do_not_release":
		outcome, why = "applied", "release_granted"
		body["members"] = stgMemberValues(tr.Members)
		r := StgRecord{"v": "1", "kind": "granted", "scope": "grant", "grant": q.ID, "question_id": q.ID, "trigger_seq": c.TriggerSeq, "members": body["members"]}
		if choice == "retry_release" {
			r["gen"] = stgNextGrantGeneration(s, tr.Members)
		} else {
			why = "release_withheld"
			r["kind"], r["why"] = "withheld", "owner"
		}
		records = append(records, r, StgRecord{"v": "1", "kind": "unblocked", "scope": "block", "block": c.Block, "why": "granted", "ref": q.ID})
	case choice == "accept_unreconciled":
		outcome, why = "applied", "quarantine_closed"
		records = append(records, stgBatchRecord("quarantine_closed", c.Batch, s.Records[c.TriggerSeq].Record.Int("try"), "question_id", q.ID, "by", "owner"))
	default:
		outcome, why = "refused", "unknown_choice"
	}
	body["outcome"], body["why"] = outcome, why
	return body, records
}

// A grant has one generation for its whole exact set. Choose a fresh
// generation above every member's previous one; history/backoff stay intact.
func stgNextGrantGeneration(s StgState, members []StgMemberKey) int64 {
	var gen int64
	for _, m := range members {
		if n := s.generation(m.R); n > gen {
			gen = n
		}
	}
	return gen + 1
}
