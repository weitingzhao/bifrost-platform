package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
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

// landingCardRepos are the repositories whose landing cards are carried:
// every repository landing supports (S6 added bifrost-trade-infra), live or
// not, so a stop or a failure in any of them reaches the Owner and a card
// already asked is still settled after its repository leaves the live set.
// A repository without landing events has no triggers and costs one read.
func landingCardRepos() []string { return landing.LandingRepoNames() }

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
	// Each step needs its own reach (review 675 R-04): settling bound cards
	// reads (Lookup) or acknowledges (Ack); taking an existing card up by its
	// key needs LookupKey alone.
	if c.Env != "prod" || q.Lookup == nil && q.Ack == nil && q.LookupKey == nil {
		return nil
	}
	now := c.clock()
	var errs []error
	// A full quota, or no way to ask, pauses new cards only; an existing
	// card is still taken up by its key (review 670 R-03, 672 R-04). The
	// pause is shared by every repository: one question store.
	mayCreate := q.AskRaw != nil && !now.Before(c.cardAsksPausedUntil())
	for _, repo := range landingCardRepos() {
		if err := c.landingCardsRepoPass(ctx, q, repo, now, &mayCreate); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repo, err))
		}
	}
	return errors.Join(errs...)
}

// landingCardsRepoPass is one repository's part of a pass: settle its bound
// cards, ask its prepared ones, then prepare and ask new ones.
func (c *Controller) landingCardsRepoPass(ctx context.Context, q store.CardQuestions, repo string, now time.Time, mayCreate *bool) error {
	var errs []error
	ask, bound, err := store.LandingCardsToSettle(ctx, c.DB, repo)
	if err != nil {
		return err
	}
	if q.Lookup != nil || q.Ack != nil {
		for _, card := range bound {
			one, cancel := context.WithTimeout(ctx, landingCardCall)
			if _, err := store.SettleLandingCard(one, c.DB, q, card, now); err != nil {
				errs = append(errs, fmt.Errorf("card %s: %w", card.Prep, err))
			}
			cancel()
		}
	}
	if q.LookupKey == nil {
		return errors.Join(errs...)
	}
	askOne := func(prep string) {
		one, cancel := context.WithTimeout(ctx, landingCardCall)
		defer cancel()
		if _, err := store.AskLandingCard(one, c.DB, q, repo, prep, now, *mayCreate); err != nil {
			if errors.Is(err, store.ErrCardQuota) {
				// Shared with every card the runtime asks: pause, do not
				// hammer it every pass; the rest of this pass still looks
				// its cards up. Memory only: a restart tries once.
				*mayCreate = false
				c.pauseCardAsks(now.Add(landingCardQuotaPause))
			}
			errs = append(errs, fmt.Errorf("card %s: %w", prep, err))
		}
	}
	for _, card := range ask {
		askOne(card.Prep)
	}
	if !*mayCreate {
		return errors.Join(errs...)
	}
	prepared, err := store.PrepareLandingCards(ctx, c.DB, repo, now)
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
