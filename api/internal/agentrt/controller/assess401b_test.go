package controller_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-401's second half: a blocked card the card-auto-answer rule in force
// answers is not assessed by STG's controller (SkipCardsTheRuleAnswers),
// whose pass reads no evidence for it either; PROD's controller, the same
// card and rule, reads its evidence and asks its assessment.
func TestOnlySTGsControllerLeavesTheRulesCardsUnassessed(t *testing.T) {
	pool := setup(t)
	card := blockedCard(t, pool)
	_, err := pool.Exec(ctx, cardAutoFiveSeconds)
	must(t, err)
	item := store.AssessItem{Kind: store.AssessCard, ID: "q_8", Title: card.Title, Body: card.Body, TaskID: card.TaskID, CreatedAt: time.Now()}
	for i, l := range card.Options {
		item.Options = append(item.Options, store.AssessOption{Key: string(rune('a' + i)), Label: l})
	}
	for _, env := range []struct {
		name string
		skip bool
		want int
	}{{"STG", true, 0}, {"PROD", false, 1}} {
		w := &world{questions: map[string]store.Question{"q_8": {ID: "q_8", Status: "open"}}}
		src := w.sources()
		src.Assessable = func(context.Context) ([]store.AssessItem, error) { return []store.AssessItem{item}, nil }
		reads := 0
		src.Evidence = func(context.Context, store.EvidenceQuery) store.Evidence {
			reads++
			return store.Evidence{}
		}
		c := &controller.Controller{DB: pool, Sources: src, SkipCardsTheRuleAnswers: env.skip}
		if n, err := c.Assess(ctx); err != nil || n != env.want || reads != env.want {
			t.Fatalf("%s: asked %d, read evidence %d times (%v); want %d", env.name, n, reads, err, env.want)
		}
	}
}
