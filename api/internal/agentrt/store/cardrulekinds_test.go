package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// signCardAutoSeconds signs a card-auto-answer rule that waits seconds and
// answers kinds, on db.
func signCardAutoSeconds(t *testing.T, db store.DB, seconds, kinds string) {
	t.Helper()
	text := ruleText(rules.KindCardAuto, 1, rules.Initial, since, until, "after_seconds: "+seconds+"\nkinds: "+kinds+"\n")
	key := rptest.NewKey(t)
	_, err := store.SignRule(ctx, db, store.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text),
		key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint)
	ok(t, err)
}

// Live plan cards are asked ByRule only when an in-force rule lists their kind.
func TestCardsAreAskedByRuleWhenTheRuleAnswersThem(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var s seenCards
	propose := func(key string) string {
		t.Helper()
		out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename", []store.PlanChange{{Op: "retitle", Node: item, Title: key}}, s.ask)
		ok(t, err)
		id, _ := out["question_id"].(string)
		if id == "" {
			t.Fatalf("no card: %v", out)
		}
		return id
	}
	before := propose("before")
	signCardAutoSeconds(t, w.rt, "5", "[blocked]")
	unlisted := propose("unlisted")
	text := ruleText(rules.KindCardAuto, 2, rules.Loosen, since, until, "after_seconds: 5\nkinds: [blocked, plan_change]\n")
	key := rptest.NewKey(t)
	_, err := store.SignRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text), key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint)
	ok(t, err)
	listed := propose("listed")
	for id, want := range map[string]bool{before: false, unlisted: false, listed: true} {
		if got := s.asked[id].ByRule; got != want {
			t.Errorf("card %s asked ByRule %v; want %v", id, got, want)
		}
	}
}
