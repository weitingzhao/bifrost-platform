package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
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

// The same on the runtime's card-auto-answer loop, on the real question store
// and a real database: under a rule that answers plan change cards at once,
// a pass answers the plain card and leaves the business issue's alone, writes
// no note of an answer on it, and leaves its proposal undecided; the Owner's
// option then decides it. Since slice 2 the loop does not even try the
// issue's card (agentRuntimeCards lists no business issue's card, so no pass
// fails on it every second); the store's own refusal stays, tested above.
func TestCardRuleLoopLeavesAnIssueCardOpen(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ISSUES", Kind: "arch_root", Label: "ISSUES", Title: "issues",
			Root: "ISSUES"}); err != nil {
			return err
		}
		for i, label := range []string{"plain", "issue"} {
			if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ISSUES-" + label, Parent: "ISSUES", Kind: "arch_row",
				Label: label, Title: label, Ordinal: i, Root: "ISSUES"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := conn.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	key := rptest.NewKey(t)
	text := "rule_id: " + rules.KindCardAuto + "\nrule_version: 1\ndirection: " + rules.Initial +
		"\neffective_at: " + now.Add(-time.Minute).UTC().Format(time.RFC3339) + "\nexpires_at: " + now.Add(24*time.Hour).UTC().Format(time.RFC3339) +
		"\nafter_seconds: 0\nkinds: [plan_change]\n"
	if _, err := store.SignRule(ctx, conn, store.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text),
		key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "questions")
	rt := newQuestionRuntime(path, nil)
	rt.takeCardsByRule(true) // as STG's runtime is wired
	thread := store.Writer{Actor: "thread:bi1-1b", Token: "operator"}
	propose := func(key, node string) string {
		t.Helper()
		out, err := store.ProposePlanChange(ctx, conn, thread, key, "ISSUES", "a row is renamed",
			[]store.PlanChange{{Op: "retitle", Node: node, Title: node + " renamed"}}, rt.agentRuntimeAsk())
		id, _ := out["question_id"].(string)
		if err != nil || id == "" {
			t.Fatalf("%s: %v %v", key, out, err)
		}
		return id
	}
	plain, issue := propose("plain", "ISSUES-plain"), propose("issue", "ISSUES-issue")
	markIssueCard(t, path, issue, "BI-7")

	cr := store.CardRule{Cards: rt.agentRuntimeCards(), Answer: rt.agentRuntimeRuleAnswer(), Ack: rt.agentRuntimeAck()}
	if cards, err := cr.Cards(ctx); err != nil || len(cards) != 1 || cards[0].ID != plain {
		t.Fatalf("the cards the rule lists: %+v %v", cards, err)
	}
	answered := 0
	for deadline := time.Now().Add(10 * time.Second); answered < 1 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		n, err := store.AnswerByRule(ctx, conn, cr)
		if err != nil {
			t.Fatal(err)
		}
		answered += n
	}
	for i := 0; i < 3; i++ {
		n, err := store.AnswerByRule(ctx, conn, cr)
		if err != nil {
			t.Fatal(err)
		}
		answered += n
	}
	if answered != 1 {
		t.Fatalf("the rule answered %d", answered)
	}
	if q, ok, err := rt.svc.Lookup(plain); err != nil || !ok || q.Answer == nil || q.Answer.Channel != "rule" || q.Answer.Option != store.CardRefuseKey {
		t.Fatalf("the plain card: %+v %v %v", q.Answer, ok, err)
	}
	if q, ok, err := rt.svc.Lookup(issue); err != nil || !ok || q.Status != "open" || q.Answer != nil {
		t.Fatalf("the issue card: %+v %+v %v %v", q, q.Answer, ok, err)
	}
	var notes int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND body_json->>'text' LIKE '%' || $1 || '%'`,
		issue).Scan(&notes); err != nil || notes != 0 {
		t.Fatalf("notes of an answer on the issue card: %d %v", notes, err)
	}
	if n, err := store.DecidePlanChanges(ctx, conn, rt.agentRuntimeQuestion(), rt.agentRuntimeAck()); err != nil || n != 1 {
		t.Fatalf("decided %d: %v", n, err)
	}
	if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", issue, []byte(`{"option":"a","channel":"console"}`)); res.Code != 200 {
		t.Fatalf("the Owner's option: %d %v", res.Code, res.Body)
	}
	if n, err := store.DecidePlanChanges(ctx, conn, rt.agentRuntimeQuestion(), rt.agentRuntimeAck()); err != nil || n != 1 {
		t.Fatalf("decided after the Owner: %d %v", n, err)
	}
	var title string
	if err := conn.QueryRow(ctx, `SELECT title FROM agentrt.plan_node WHERE plan_node_id = 'ISSUES-issue'`).Scan(&title); err != nil ||
		title != "ISSUES-issue renamed" {
		t.Fatalf("the issue row's title: %q %v", title, err)
	}
}
