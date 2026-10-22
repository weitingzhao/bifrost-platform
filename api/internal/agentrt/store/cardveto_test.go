package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// signCardAutoVeto signs a card-auto-answer rule that waits seconds, answers
// kinds and keeps AI assessments to a veto (assessment: veto), on db.
func signCardAutoVeto(t *testing.T, db store.DB, seconds, kinds string) {
	t.Helper()
	text := ruleText(rules.KindCardAuto, 1, rules.Initial, since, until,
		"after_seconds: "+seconds+"\nkinds: "+kinds+"\nassessment: "+rules.CardAssessmentVeto+"\n")
	key := rptest.NewKey(t)
	_, err := store.SignRule(ctx, db, store.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text),
		key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint)
	ok(t, err)
}

// Review 489 A1 on T12 (the coordinating thread 2026-10-22 for the Owner:
// A1): under assessment: veto an AI assessment only ever keeps the rule from
// the card's recommendation. A card that recommends nothing takes the option
// that changes nothing (b on the decision cards) whatever its assessment
// says; a card that recommends one is answered as before; an option the card
// does not have is never taken, and the card is left to the Owner. Without
// assessment: veto the rule reads as versions 1 to 3 were signed.
func TestRuleChoiceUnderAVeto(t *testing.T) {
	t.Parallel()
	ab, abc := []string{"a", "b"}, []string{"a", "b", "c"}
	for name, c := range map[string]struct {
		kind, rec, assessed string
		veto                bool
		options             []string
		want, why           string
	}{
		"no recommendation, the assessment's a":      {store.CardBlocked, "", "a", true, abc, "b", "not the one its AI assessment recommends (a)"},
		"no recommendation, no assessment":           {store.CardBlocked, "", "", true, abc, "b", "the card recommends nothing: the rule takes the option that changes nothing"},
		"no recommendation, the assessment's b too":  {store.CardBlocked, "", "b", true, abc, "b", ", which its AI assessment recommends too"},
		"a recorder import, no recommendation":       {"recorder_import", "", "a", true, ab, "b", "the option that changes nothing"},
		"recommended and agreed":                     {store.CardBlocked, "a", "a", true, abc, "a", ""},
		"recommended, the assessment differs":        {store.CardBlocked, "a", "c", true, abc, "b", "the rule takes the option that changes nothing"},
		"recommended, no assessment":                 {"mission", "a", "", true, ab, "a", ""},
		"a planner's question, nothing recommended":  {store.CardPlanQuestion, "", "a", true, ab, "", ""},
		"a planner's question, its own option":       {store.CardPlanQuestion, "b", "a", true, ab, "b", ""},
		"no option that changes nothing on the card": {store.CardBlocked, "", "", true, []string{"a"}, "", ""},
		"the recommendation not among the options":   {"plan_change", "a", "", true, nil, "", ""},
		"before the veto: the assessment's option":   {store.CardBlocked, "", "a", false, abc, "a", "the rule takes the assessment's"},
		"before the veto: options not read":          {"plan_change", "a", "", false, nil, "a", ""},
	} {
		option, why := store.RuleChoiceForTest(c.kind, c.rec, c.assessed, c.veto, c.options)
		if option != c.want || (c.why == "" && why != "" && c.rec != "") || !strings.Contains(why, c.why) {
			t.Errorf("%s: %q (%q), want %q (%q)", name, option, why, c.want, c.why)
		}
		// Review 507 C1: the reason never says the assessment recommends
		// another option when it recommends the one taken.
		if c.veto && c.assessed == option && strings.Contains(why, "not the one") {
			t.Errorf("%s: %q says %q", name, option, why)
		}
	}
}

// The same under a signed rule: a blocked card that recommends nothing and
// whose assessment recommends a is answered b, as the Owner's do-not-do
// would be, saying why; a planner's question that recommends nothing stays
// open for the Owner; one that recommends its option is answered with it.
func TestAVetoRuleNeverTakesTheAssessmentsOption(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	assessed := w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, assessed) }))
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.cardOf(t, assessed)
	if rec := s.item(card).Recommended; rec != "" {
		t.Fatalf("the card recommends %q", rec)
	}
	task, _ := w.askAndReady(t, s.item(card), ratchetEvidence())
	w.work(t, task, report("a", [][2]string{{"再来一轮就够。", "commit:bifrost-platform@6b9507328434"}, {"失败只是一次。", w.waitEvent(t, assessed)}}), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "assessed" {
		t.Fatalf("accept: %q %v", did, err)
	}
	// A planner's question, asked once the assessment is in (the brief's
	// host would take the plan first).
	planTask := w.ready(t, store.Task{MissionID: "M-1", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	s.put(store.RuntimeCard{ID: "q_plannerquestion2", TaskID: planTask, CreatedAt: time.Now(), Options: []string{"a", "b"}})
	signCardAutoVeto(t, w.rt, "0", "[blocked, plan_question]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v), want the blocked card only", n, err)
	}
	why := "the card recommends nothing: the rule takes the option that changes nothing, not the one its AI assessment recommends (a)"
	if got := s.cards[card]; got.option != "b" || got.text != why || got.by != "rule card-auto-answer v1" {
		t.Fatalf("the assessed card: %+v", got)
	}
	if got := s.cards["q_plannerquestion2"]; got.status != "open" {
		t.Fatalf("the planner's question that recommends nothing: %+v", got)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if st := w.state(t, "task", assessed); st != "cancelled" {
		t.Fatalf("the assessed task is %s, want cancelled (b: do not do it)", st)
	}
	notes := strings.Join(ruleNotes(t, w.rt, store.MissionStream("M-1")), "|")
	if !strings.Contains(notes, "answered question "+card+" with b, the option that changes nothing, after ") || !strings.Contains(notes, "; "+why) {
		t.Fatalf("notes: %q", notes)
	}
	// Its recommendation given, the same question is the rule's again.
	s.mu.Lock()
	s.cards["q_plannerquestion2"].card.Recommended = "b"
	s.mu.Unlock()
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 || s.cards["q_plannerquestion2"].option != "b" {
		t.Fatalf("answered %d (%v): %+v", n, err, s.cards["q_plannerquestion2"])
	}
}

// Review 507 B1: STG asks no assessment for a card the rule answers
// (SkipCardsTheRuleAnswers). Under assessment: veto a card the rule leaves
// (a planner's question that recommends nothing) is the Owner's, so it is
// assessed; one the rule answers (a blocked card that recommends nothing: b)
// is still left out. Under a rule without the veto both are left out, as
// before.
func TestAVetoRuleAssessesTheCardsItLeaves(t *testing.T) {
	t.Parallel()
	for _, veto := range []bool{true, false} {
		w := newKernelWorld(t)
		blocked := w.ready(t, store.Task{})
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, blocked) }))
		var s seenCards
		if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
			t.Fatalf("asked %d (%v)", n, err)
		}
		planTask := w.ready(t, store.Task{MissionID: "M-1", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
		question := store.AssessItem{Kind: store.AssessCard, ID: "q_plannerquestion3", Title: "Which way?", Body: "two ways",
			Options: []store.AssessOption{{Key: "a", Label: "this way"}, {Key: "b", Label: "that way"}}, TaskID: planTask, CreatedAt: time.Now()}
		if veto {
			signCardAutoVeto(t, w.rt, "0", "[blocked, plan_question]")
		} else {
			signCardAutoSeconds(t, w.rt, "0", "[blocked, plan_question]")
		}
		_, fresh, err := store.AssessmentQuery(ctx, w.rt, s.item(s.cardOf(t, blocked)), true)
		if err != nil || fresh {
			t.Fatalf("veto %v: the blocked card the rule answers: fresh %v (%v)", veto, fresh, err)
		}
		_, fresh, err = store.AssessmentQuery(ctx, w.rt, question, true)
		if err != nil || fresh != veto {
			t.Fatalf("veto %v: the planner's question that recommends nothing: fresh %v (%v)", veto, fresh, err)
		}
	}
}
