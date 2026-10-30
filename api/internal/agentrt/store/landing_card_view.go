package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// LandingCardView is what a landing card asked and what became of its
// answer (CUT §11.6 v3 item 8): the card from the card stream, the handling
// record from the repository stream (a §8 landing_answer or a
// landing_card_outcome), whatever the question store still holds. Found
// false when no card was bound to the question.
type LandingCardView struct {
	Family, Block, Batch                              string
	Repo, QuestionID, Prep, Kind, Root, Op, ClosedWhy string
	TriggerSeq, StopGeneration                        int64
	Acked, Closed                                     bool
	Options                                           map[string]string
	// Outcome is the handling record as stored; nil before the answer is
	// handled.
	Outcome map[string]any
	// NextStep says in a sentence what follows (English, for the Console).
	NextStep string
}

// ReadLandingCard reads a landing card by its question.
func ReadLandingCard(ctx context.Context, db DB, repo, questionID string) (LandingCardView, bool, error) {
	var v LandingCardView
	found := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		cards, err := LandingCards(ctx, tx, CardsStream(repo))
		if err != nil {
			return err
		}
		for _, c := range cards {
			if c.QuestionID != questionID {
				continue
			}
			found = true
			v = LandingCardView{Repo: repo, QuestionID: questionID, Prep: c.Prep, Kind: c.Kind, Root: c.Root, Op: c.Op, ClosedWhy: c.ClosedWhy,
				Family: c.Family, Block: c.Block, Batch: c.Batch,
				TriggerSeq: c.TriggerSeq, StopGeneration: c.StopGeneration, Acked: c.Acked, Closed: c.Closed, Options: c.Options}
		}
		if !found {
			return nil
		}
		// The record checked to be this card's, as the handler reads it
		// (review 672 R-03).
		var card CardRecord
		for _, c := range cards {
			if c.QuestionID == questionID {
				card = c
			}
		}
		_, stored, _, err := storedCardRecord(ctx, tx, card, questionID)
		if err != nil {
			return err
		}
		v.Outcome = stored
		v.NextStep = landingCardNextStep(v)
		return nil
	})
	return v, found, err
}

func landingCardNextStep(v LandingCardView) string {
	if v.Family == "stg" {
		return stgCardNextStep(v)
	}
	if v.Outcome == nil {
		switch {
		case v.Closed:
			return "This card ended without an answer being carried out (" + v.ClosedWhy + "). If the landing still needs you, a new card is asked."
		default:
			return "Waiting for your answer, or for the runtime to carry it out (every 15 seconds)."
		}
	}
	outcome, _ := v.Outcome["outcome"].(string)
	why, _ := v.Outcome["why"].(string)
	switch outcome {
	case "applied":
		return "Carried out (" + why + ")."
	case "held":
		return "Held: you are asked again after " + valueString(v.Outcome["remind_at"]) + " if it still applies."
	case "acknowledged":
		return "Noted. Nothing else follows from this card."
	case "moot":
		return "Not carried out: the landing had already moved on (" + why + "). Nothing was changed."
	case "refused":
		return "Not carried out (" + why + "). If the landing still needs you, a new card is asked after the cooldown."
	}
	return outcome + " (" + why + ")."
}

func valueString(v any) string {
	s, _ := v.(string)
	return s
}

// FindLandingCard resolves a globally unique question across the closed
// repository registry. A duplicate binding is unreadable, never guessed.
func FindLandingCard(ctx context.Context, db DB, questionID string) (LandingCardView, bool, error) {
	var found bool
	var result LandingCardView
	for _, repo := range landing.LandingRepoNames() {
		v, ok, err := ReadLandingCard(ctx, db, repo, questionID)
		if err != nil {
			return LandingCardView{}, false, err
		}
		if ok {
			if found {
				return LandingCardView{}, false, fmt.Errorf("landing question bound to multiple repositories")
			}
			result, found = v, true
		}
	}
	return result, found, nil
}
