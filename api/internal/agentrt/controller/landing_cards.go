package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingCardsEvery is how often the landing cards are carried along (S3
// B3, CUT §11.6 v3): settle bound cards, ask unbound ones, prepare new.
const LandingCardsEvery = 15 * time.Second

// landingCardCall bounds one call to the question store.
const landingCardCall = 20 * time.Second

// landingCardQuotaPause is how long asking pauses after the question store's
// quota is full.
const landingCardQuotaPause = 30 * time.Minute

// landingCardsRepo is the one repository landing runs for (S3).
const landingCardsRepo = "bifrost-platform"

// LandingCardsComplete: the question store gives all the landing cards
// need. A new live landing starts only with them (CUT §11.6 v3 item 9);
// without them what exists is still carried as far as it can be.
func LandingCardsComplete(q store.CardQuestions) bool {
	return q.AskRaw != nil && q.LookupKey != nil && q.Lookup != nil && q.Withdraw != nil && q.Ack != nil
}

// LandingCardsPass is one pass over the landing cards on PROD, whatever the
// live switch says: first every bound card one step toward its end (an
// answer handled and acknowledged, a stale card withdrawn, an expired or
// lost one closed), then the prepared cards asked and bound, then new cards
// prepared for the triggers that may have one, and asked. Each step reads
// and writes the database on its own; one card's failure leaves the others.
func (c *Controller) LandingCardsPass(ctx context.Context) error {
	q := c.LandingCards
	if c.Env != "prod" || q.Lookup == nil && q.Ack == nil {
		return nil
	}
	now := c.clock()
	var errs []error
	ask, bound, err := store.LandingCardsToSettle(ctx, c.DB, landingCardsRepo)
	if err != nil {
		return err
	}
	for _, card := range bound {
		one, cancel := context.WithTimeout(ctx, landingCardCall)
		if _, err := store.SettleLandingCard(one, c.DB, q, card, now); err != nil {
			errs = append(errs, fmt.Errorf("card %s: %w", card.Prep, err))
		}
		cancel()
	}
	if q.LookupKey == nil {
		return errors.Join(errs...)
	}
	// A full quota, or no way to ask, pauses new cards only; an existing
	// card is still taken up by its key (review 670 R-03, 672 R-04).
	mayCreate := q.AskRaw != nil && !now.Before(c.cardAsksPausedUntil())
	askOne := func(prep string) {
		one, cancel := context.WithTimeout(ctx, landingCardCall)
		defer cancel()
		if _, err := store.AskLandingCard(one, c.DB, q, landingCardsRepo, prep, now, mayCreate); err != nil {
			if errors.Is(err, store.ErrCardQuota) {
				// Shared with every card the runtime asks: pause, do not
				// hammer it every pass; the rest of this pass still looks
				// its cards up. Memory only: a restart tries once.
				mayCreate = false
				c.pauseCardAsks(now.Add(landingCardQuotaPause))
			}
			errs = append(errs, fmt.Errorf("card %s: %w", prep, err))
		}
	}
	for _, card := range ask {
		askOne(card.Prep)
	}
	if !mayCreate {
		return errors.Join(errs...)
	}
	prepared, err := store.PrepareLandingCards(ctx, c.DB, landingCardsRepo, now)
	if err != nil {
		errs = append(errs, err)
	}
	for _, prep := range prepared {
		askOne(prep)
	}
	return errors.Join(errs...)
}

func (c *Controller) cardAsksPausedUntil() time.Time {
	c.cardMu.Lock()
	defer c.cardMu.Unlock()
	return c.cardPause
}

func (c *Controller) pauseCardAsks(until time.Time) {
	c.cardMu.Lock()
	defer c.cardMu.Unlock()
	c.cardPause = until
}
