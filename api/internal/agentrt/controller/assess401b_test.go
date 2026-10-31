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
// whose pass reads no evidence for it either; when the rule comes into force
// while the card's evidence is read, the pass still asks nothing (the check
// again where the task would be made, review 462 C3); PROD's controller, the
// same card and rule, reads its evidence and asks its assessment.
func TestOnlySTGsControllerLeavesTheRulesCardsUnassessed(t *testing.T) {
	pool := setup(t)
	card := blockedCard(t, pool)
	item := store.AssessItem{Kind: store.AssessCard, ID: "q_8", Title: card.Title, Body: card.Body, TaskID: card.TaskID, CreatedAt: time.Now()}
	for i, l := range card.Options {
		item.Options = append(item.Options, store.AssessOption{Key: string(rune('a' + i)), Label: l})
	}
	must(t, store.RequestAssessment(ctx, pool, store.Writer{Actor: "owner"}, item.Kind, item.ID, func(context.Context) error { return nil }))
	// In this order: the rule is not in force until the first pass reads the
	// card's evidence, and stays in force after.
	for _, env := range []struct {
		name        string
		skip        bool
		reads, want int
	}{{"STG, the rule signed during the read", true, 1, 0}, {"STG", true, 0, 0}, {"PROD", false, 1, 1}} {
		w := &world{questions: map[string]store.Question{"q_8": {ID: "q_8", Status: "open"}}}
		src := w.sources()
		src.Assessable = func(context.Context) ([]store.AssessItem, error) { return []store.AssessItem{item}, nil }
		reads := 0
		src.Evidence = func(context.Context, store.EvidenceQuery) store.Evidence {
			if reads++; reads == 1 {
				var signed bool
				must(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.rule WHERE rule_id = 'card-auto-answer')`).Scan(&signed))
				if !signed {
					_, err := pool.Exec(ctx, cardAutoFiveSeconds)
					must(t, err)
				}
			}
			return store.Evidence{}
		}
		c := &controller.Controller{DB: pool, Sources: src, SkipCardsTheRuleAnswers: env.skip}
		if n, err := c.Assess(ctx); err != nil || n != env.want || reads != env.reads {
			t.Fatalf("%s: asked %d, read evidence %d times (%v); want %d and %d", env.name, n, reads, err, env.want, env.reads)
		}
	}
}
