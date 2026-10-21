package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-401's second half (the Owner 2026-10-20 20:0x 「STG 上规则会答的卡（规则
// 列着这种卡、mission 不是 needs_owner）不起评估；needs_owner 的卡和审批单照常评
// 估；PROD 不变」). On STG (skipRuled) a card the card-auto-answer rule in force
// answers is neither read for nor asked, and leaves nothing on its stream; a
// card of a mission that needs the Owner is assessed, and so is an approval;
// with no rule in force the card is assessed too (a rule that does not list
// its kind: ruleAnswers, TestCardsAreAskedByRuleWhenTheRuleAnswersThem).
// PROD (not skipRuled) assesses every card, the ones the rule answers
// included.
func TestSTGDoesNotAssessTheCardsItsRuleAnswers(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ruled, owners := w.overBudget(t, "M-3", ""), w.overBudget(t, "M-4", "the walk tests the Owner's buttons")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card, ownerCard := s.item(s.cardOf(t, ruled)), s.item(s.cardOf(t, owners))
	approval := store.AssessItem{Kind: store.AssessApproval, ID: "7", Title: "Deliver bifrost-platform to PROD",
		Body: "STG runs 8f30a9f6 since 14:00 and its CI is green.", Options: store.ApprovalOptions(), CreatedAt: time.Now()}
	fresh := func(it store.AssessItem) bool {
		t.Helper()
		_, fresh, err := store.AssessmentQuery(ctx, w.rt, it, true)
		ok(t, err)
		return fresh
	}
	if !fresh(card) {
		t.Fatal("with no rule in force, STG left the card out")
	}
	signCardAutoSeconds(t, w.rt, "5", "[blocked]")
	if fresh(card) {
		t.Fatal("STG read the evidence of a card its rule answers")
	}
	if task, err := store.GiveAssessment(ctx, w.rt, card, ratchetEvidence(), s.question, true); err != nil || task != "" {
		t.Fatalf("STG asked the assessment of a card its rule answers: %q (%v)", task, err)
	}
	if events, missions := answeredAsks(t, w, card.ID); events != 0 || missions != 0 {
		t.Fatalf("the card the rule answers left %d events and %d missions", events, missions)
	}
	for _, it := range []store.AssessItem{ownerCard, approval} {
		if !fresh(it) {
			t.Fatalf("STG left out %s %s", it.Kind, it.ID)
		}
		if task, err := store.GiveAssessment(ctx, w.rt, it, ratchetEvidence(), s.question, true); err != nil || task == "" {
			t.Fatalf("STG did not assess %s %s: %q (%v)", it.Kind, it.ID, task, err)
		}
	}
	// PROD: the same card under the same rule is assessed.
	if _, fresh, err := store.AssessmentQuery(ctx, w.rt, card, false); err != nil || !fresh {
		t.Fatalf("PROD left out the card: fresh %v (%v)", fresh, err)
	}
	if task, err := store.GiveAssessment(ctx, w.rt, card, ratchetEvidence(), s.question, false); err != nil || task == "" {
		t.Fatalf("PROD did not assess the card the rule answers: %q (%v)", task, err)
	}
}
