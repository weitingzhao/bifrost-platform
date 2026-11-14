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

// TD-393 (the coordinating thread 2026-10-20: STG's runtime was held by the
// question store's quotas, no card open for the rule to answer): a card is
// asked ByRule, so the question store may take it past its quotas, exactly
// when a card-auto-answer rule in force lists its kind and its mission does
// not need the Owner; with no rule, none is.
func TestCardsAreAskedByRuleWhenTheRuleAnswersThem(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var s seenCards
	_, before := proposeOutside(t, w, s.ask, "before")
	signCardAutoSeconds(t, w.rt, "5", "[blocked, mission]")
	_, mission := proposeOutside(t, w, s.ask, "after")
	marked, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "marked", store.MissionProposal{Title: "m marked", Goal: "g",
		Success: []string{"done"}, NeedsOwner: "it must wait for the Owner"}, s.ask)
	ok(t, err)
	markedCard, _ := marked["question_id"].(string)
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1,
		MissionID: "M-2"}}, s.ask)
	ok(t, err)
	planCard, _ := out["question_id"].(string)
	for id, want := range map[string]bool{before: false, mission: true, markedCard: false, planCard: false} {
		if got := s.asked[id].ByRule; got != want {
			t.Errorf("card %s (%s) asked ByRule %v; want %v", id, s.asked[id].Title, got, want)
		}
	}
}
