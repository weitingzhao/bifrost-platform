package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// markIssueCard gives the card id in the question store at path the work id
// work, as a later version that asks business issue cards leaves it (this
// one takes no BI- work id when a card is asked).
func markIssueCard(t *testing.T, path, id, work string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rows, _ := doc["questions"].([]any)
	found := false
	for _, row := range rows {
		if q, _ := row.(map[string]any); q != nil && q["id"] == id {
			q["work_id"], found = work, true
		}
	}
	if !found {
		t.Fatalf("no card %s in the store", id)
	}
	if raw, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// W-31 BI-1 slice 1b on the wire the runtime's rule answers through, with no
// database: a business issue's card is refused to the rule, with the option
// it recommends or another and why, and stays open for the Owner, who
// answers it with an option.
func TestRuleAnswerLeavesAnIssueCardOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "questions")
	rt := newQuestionRuntime(path, nil)
	ctx := context.Background()
	ask, answer, read := rt.agentRuntimeAsk(), rt.agentRuntimeRuleAnswer(), rt.agentRuntimeQuestion()
	id, err := ask(ctx, store.Card{Key: strings.Repeat("cd", 16), Title: "BI-7 · priority", Body: "No words of the issue here.",
		Recommended: 2, RecommendedWhy: "why"})
	if err != nil {
		t.Fatal(err)
	}
	markIssueCard(t, path, id, "BI-7")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	for _, c := range []struct{ option, why string }{{"b", ""}, {"a", "the option that changes nothing"}} {
		if err := answer(ctx, id, c.option, "rule card-auto-answer v1", until, c.why); err == nil || !strings.Contains(err.Error(), "business issue") {
			t.Fatalf("the rule's %s: %v", c.option, err)
		}
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatalf("a refused answer moved the store: %v", err)
	}
	if q, ok := read(ctx, id); !ok || q.Status != "open" || q.Option != "" || q.Text != "" {
		t.Fatalf("the card after the rule: %+v %v", q, ok)
	}
	if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", id, []byte(`{"option":"b","channel":"console"}`)); res.Code != 200 {
		t.Fatalf("the Owner's option: %d %v", res.Code, res.Body)
	}
	if q, ok := read(ctx, id); !ok || q.Status != store.QuestionAnswered || q.Option != "b" || q.Text != "" || q.Channel != "console" {
		t.Fatalf("the Owner's answer: %+v %v", q, ok)
	}
}

// Review 520 A1 on the wire the runtime withdraws its cards through (a
// blocked card whose task moved on, a plan's card whose round was refused):
// a business issue's card is withdrawn, never left open, but the reason the
// runtime gives is not kept on it; a card on other work keeps it, as before.
func TestRuntimeWithdrawsAnIssueCardWithoutItsReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "questions")
	rt := newQuestionRuntime(path, nil)
	ctx := context.Background()
	ask, withdraw := rt.agentRuntimeAsk(), rt.agentRuntimeWithdraw()
	const reason = "the task moved on: withdraw-marker-5b1e"
	for i, work := range []string{"BI-7", ""} {
		id, err := ask(ctx, store.Card{Key: strings.Repeat(string(rune('e'+i)), 32), Title: "BI-7 · priority", Body: "No words of the issue here."})
		if err != nil {
			t.Fatal(err)
		}
		if work != "" {
			markIssueCard(t, path, id, work)
		}
		if err = withdraw(ctx, id, reason); err != nil {
			t.Fatal(err)
		}
		q, ok, err := rt.svc.Lookup(id)
		if err != nil || !ok || q.Status != "withdrawn" || q.Withdrawal == nil || q.Withdrawal.Reason == "" {
			t.Fatalf("%q: %+v %v %v", work, q, ok, err)
		}
		if kept := q.Withdrawal.Reason == reason; kept != (work == "") {
			t.Fatalf("%q: the reason kept is %q", work, q.Withdrawal.Reason)
		}
	}
	if raw, err := os.ReadFile(path); err != nil || strings.Count(string(raw), "withdraw-marker-5b1e") != 1 {
		t.Fatalf("the runtime's reason is kept on more than the card on other work: %v", err)
	}
}
