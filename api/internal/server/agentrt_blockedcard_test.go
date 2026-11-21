//bifrost:owner owner

package server

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
)

// Review 439 A3: only a card that says so (a blocked card) is taken for the
// card the store holds under its key, and only when that card names the same
// task and options; a plan change retried under its key with other content,
// or a card naming another task or other options, stays a conflict.
func TestOnlyTheSameCardIsTakenByItsKey(t *testing.T) {
	rt := questionbridge.New(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask := rt.Ask()
	pc := ownercard.Card{Key: strings.Repeat("ab", 16), Title: "Plan change to R2: a new item", Body: "A thread proposes P1"}
	first, err := ask(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	pc.Body = "A thread proposes P2"
	if id, err := ask(ctx, pc); err == nil || id != "" {
		t.Fatalf("a plan change's key with other content took card %q (%v); want a conflict", id, err)
	}
	task := "t_01M5192P0JPMJAP63ZKRPXJ2VV"
	blocked := ownercard.Card{Key: strings.Repeat("cd", 16), Title: "Blocked in M-3: t", Body: "started 30 hours ago",
		Options: []string{questioncards.BlockedRetry, questioncards.BlockedDrop, "Close mission M-3"}, Recommended: 3, RecommendedWhy: "why",
		Meanwhile: "the task stays blocked", OwnerDuty: "goal", TaskID: task, Urgency: "when you can", SameByKey: true}
	card, err := ask(ctx, blocked)
	if err != nil || card == first {
		t.Fatalf("the blocked card: %q %v", card, err)
	}
	later := blocked
	later.Body = "started 4 days ago"
	if id, err := ask(ctx, later); err != nil || id != card {
		t.Fatalf("the same card in other words: %q %v; want %s", id, err, card)
	}
	for name, edit := range map[string]func(*ownercard.Card){
		"another task": func(c *ownercard.Card) { c.TaskID = "t_01M5192P0JPMJAP63ZKRPXJ2VW" },
		"another mission": func(c *ownercard.Card) {
			c.Options = []string{questioncards.BlockedRetry, questioncards.BlockedDrop, "Close mission M-4"}
		},
		"not asked so": func(c *ownercard.Card) { c.SameByKey = false },
	} {
		other := later
		edit(&other)
		if id, err := ask(ctx, other); err == nil || id != "" {
			t.Errorf("%s: took card %q (%v); want a conflict", name, id, err)
		}
	}
}
