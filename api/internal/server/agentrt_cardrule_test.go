package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of a card answered by rule (console/src/api/__fixtures__/question-rule-answered.testdb.json):
// GET /api/v1/questions/{id} of the card below once the rule answered it. Off unless asked for:
//
//	CONSOLE_QUESTION_FIXTURE_WRITE=1 go test ./internal/server/ -run TestCardRuleOnTheStore
const consoleRuleAnsweredFixture = "../../../console/src/api/__fixtures__/question-rule-answered.testdb.json"

// TD-390 on the real question store: the runtime lists its open cards with
// the option each recommends and when it was asked, and answers one as its
// rule; the store keeps the answer as the rule's, on the rule channel, and
// the runtime reads it back like the Owner's. Another requester's question
// is not listed.
func TestCardRuleOnTheStore(t *testing.T) {
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask, cards, answer, read := rt.agentRuntimeAsk(), rt.agentRuntimeCards(), rt.agentRuntimeRuleAnswer(), rt.agentRuntimeQuestion()
	task := "t_01M5192P0JPMJAP63ZKRPXJ2VV"
	id, err := ask(ctx, store.Card{Key: strings.Repeat("ab", 16), Title: "Blocked in M-3: STG walk", Body: "Recommended: Close mission M-3 — why",
		Options: []string{store.BlockedRetry, store.BlockedDrop, "Close mission M-3"}, Recommended: 3, RecommendedWhy: "why",
		Meanwhile: "the task stays blocked", OwnerDuty: "goal", TaskID: task, Urgency: "when you can", SameByKey: true})
	if err != nil {
		t.Fatal(err)
	}
	thread, _ := json.Marshal(map[string]any{"idempotency_key": "thread-question-1", "title": "A thread asks", "urgency": "u", "meanwhile": "m",
		"options": []map[string]string{{"label": "Yes"}, {"label": "No"}}, "recommended": 1})
	if res := rt.svc.Create(ctx, "thread-a", "reporter", "reporter", thread); res.Code != 201 {
		t.Fatalf("a thread's question: %d %v", res.Code, res.Body)
	}
	list, err := cards(ctx)
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].TaskID != task || list[0].Recommended != "c" || list[0].CreatedAt.IsZero() {
		t.Fatalf("the runtime's cards: %+v %v", list, err)
	}
	until := time.Now().Add(time.Hour)
	if err = answer(ctx, id, "a", "rule card-auto-answer v1", until, ""); err == nil {
		t.Fatal("the rule answered with an option the card does not recommend")
	}
	if err = answer(ctx, id, "c", "rule card-auto-answer v1", until, ""); err != nil {
		t.Fatal(err)
	}
	q, ok := read(ctx, id)
	if !ok || q.Status != store.QuestionAnswered || q.Option != "c" || q.AnsweredBy != "rule card-auto-answer v1" || q.Options != 3 ||
		q.Channel != "rule" {
		t.Fatalf("read back: %+v %v", q, ok)
	}
	if list, err = cards(ctx); err != nil || len(list) != 1 || !list[0].Answered || list[0].By != "rule card-auto-answer v1" {
		t.Fatalf("after the answer, before the runtime read it: %+v %v", list, err)
	}
	if err = rt.agentRuntimeAck()(ctx, id); err != nil {
		t.Fatal(err)
	}
	if list, err = cards(ctx); err != nil || len(list) != 0 {
		t.Fatalf("after the runtime read it: %+v %v", list, err)
	}
	if os.Getenv("CONSOLE_QUESTION_FIXTURE_WRITE") == "1" {
		res := rt.svc.Get(ctx, "owner-console", "admin", "admin", id)
		raw, err := json.MarshalIndent(map[string]any{"status": res.Code, "body": res.Body}, "", "  ")
		if err != nil || res.Code != 200 {
			t.Fatalf("the fixture: %d %v", res.Code, err)
		}
		if err = os.WriteFile(consoleRuleAnsweredFixture, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TD-393: the card-auto-answer loop (every kind; every second since TD-402)
// is wired on STG alone; PROD's and DEV's controllers get no card list and no
// rule's answer, so the loop never starts there (controller.Start needs
// both), while the assessments stay. TD-401's second half: only STG's
// controller leaves the cards that rule answers unassessed; PROD's and DEV's
// assess every card, whatever rule is in force.
func TestOnlySTGAnswersCardsByRule(t *testing.T) {
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	full := controller.Sources{Cards: rt.agentRuntimeCards(), RuleAnswer: rt.agentRuntimeRuleAnswer(),
		Assessable: assessable(rt.agentRuntimeOpenCards(), nil)}
	if full.Cards == nil || full.RuleAnswer == nil || full.Assessable == nil {
		t.Fatal("the sources are nil before the environment is applied")
	}
	for _, env := range []string{"prod", "dev", "", "STG"} {
		c := agentRuntimeController(env, nil, full)
		if src := c.Sources; src.Cards != nil || src.RuleAnswer != nil || src.Assessable == nil || c.SkipCardsTheRuleAnswers || c.FailFirstVerify {
			t.Errorf("%q: cards %v, rule answer %v, assessments %v, skips the rule's cards %v, fails the first verify %v", env,
				src.Cards != nil, src.RuleAnswer != nil, src.Assessable != nil, c.SkipCardsTheRuleAnswers, c.FailFirstVerify)
		}
	}
	if c := agentRuntimeController("stg", nil, full); c.Sources.Cards == nil || c.Sources.RuleAnswer == nil || c.Sources.Assessable == nil ||
		!c.SkipCardsTheRuleAnswers || !c.FailFirstVerify {
		t.Fatalf("STG's controller: %+v", c)
	}
	if controller.CardRuleEvery != time.Second {
		t.Fatalf("the loop runs every %s", controller.CardRuleEvery)
	}
}

// TD-393 (the coordinating thread 2026-10-20: STG's runtime held by the
// question store's 429 while no card was open for the rule to answer): on
// STG the runtime asks ten cards its rule answers in a row; a card no rule
// answers, and any card on PROD, is held by the quotas as before.
func TestRuntimeCardsTheRuleAnswersSkipTheQuotas(t *testing.T) {
	ctx := context.Background()
	card := func(i int, byRule bool) store.Card {
		return store.Card{Key: fmt.Sprintf("%032x", i), Title: fmt.Sprintf("Blocked in M-%d: a task", i), Body: "b",
			Options: []string{store.BlockedRetry, store.BlockedDrop}, OwnerDuty: "goal", TaskID: "t_01M5192P0JPMJAP63ZKRPXJ2VV",
			Meanwhile: "m", ByRule: byRule}
	}
	stg := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	stg.takeCardsByRule(true)
	for i := range 10 {
		if _, err := stg.agentRuntimeAsk()(ctx, card(i, true)); err != nil {
			t.Fatalf("STG, card %d the rule answers: %v", i, err)
		}
	}
	held := func(rt *questionRuntime, byRule bool) int {
		t.Helper()
		for i := range 10 {
			if _, err := rt.agentRuntimeAsk()(ctx, card(100+i, byRule)); err != nil {
				if !strings.Contains(err.Error(), "429") {
					t.Fatalf("card %d: %v", i, err)
				}
				return i
			}
		}
		return 10
	}
	if n := held(newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil), true); n != 3 {
		t.Fatalf("PROD (no lift) took %d cards before the quota; want 3", n)
	}
	lifted := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	lifted.takeCardsByRule(true)
	if n := held(lifted, false); n != 3 {
		t.Fatalf("STG took %d cards no rule answers before the quota; want 3", n)
	}
}
