package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if err = answer(ctx, id, "a", "rule card-auto-answer v1"); err == nil {
		t.Fatal("the rule answered with an option the card does not recommend")
	}
	if err = answer(ctx, id, "c", "rule card-auto-answer v1"); err != nil {
		t.Fatal(err)
	}
	q, ok := read(ctx, id)
	if !ok || q.Status != store.QuestionAnswered || q.Option != "c" || q.AnsweredBy != "rule card-auto-answer v1" || q.Options != 3 {
		t.Fatalf("read back: %+v %v", q, ok)
	}
	if list, err = cards(ctx); err != nil || len(list) != 0 {
		t.Fatalf("after the answer: %+v %v", list, err)
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
