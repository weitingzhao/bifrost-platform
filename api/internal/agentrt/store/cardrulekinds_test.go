package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// allKinds is every kind of card the runtime asks (TD-393).
const allKinds = "[blocked, plan_question, plan_change, plan_import, recorder_import, mission]"

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

// querier is a connection or a pool, as a test reads them.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ruleNotes is the notes of a rule's answers on a stream.
func ruleNotes(t *testing.T, db querier, stream string) []string {
	t.Helper()
	rows, err := db.Query(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->>'text' LIKE 'rule %' ORDER BY seq`, stream)
	ok(t, err)
	notes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	return notes
}

// TD-393 (the Owner 2026-10-20 「STG 的规则等待按秒、设 5 秒…STG 上所有种类的运行时
// 卡都归规则管；给不出推荐…选不改变任何东西的那项」): with a rule that waits five
// seconds and lists every kind, no card is answered at four seconds; at six,
// one pass answers each: a blocked card that recommends an option with it, one
// that recommends none with b ("Do not do this task"), a planner's question
// with its own option, a plan change's card with b ("Do not apply it") and a
// mission's with b ("Do not start it"), saying why when it is not the card's
// recommendation; a mission marked as needing the Owner keeps its card. Each
// decision then acts as on the Owner's answer and names the rule; each answer
// is noted once, where the card is.
func TestEveryKindIsAnsweredAfterItsWait(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	stale := w.overBudget(t, "M-3", "")
	plain := w.ready(t, store.Task{}) // M-1
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, plain) }))
	planTask := w.ready(t, store.Task{MissionID: "M-1", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	s.put(store.RuntimeCard{ID: "q_plannerquestion1", TaskID: planTask, Recommended: "b", CreatedAt: time.Now()})
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1,
		MissionID: "M-2"}}, s.ask)
	ok(t, err)
	planCard, _ := out["question_id"].(string)
	mission, missionCard := proposeOutside(t, w, s.ask, "out")
	marked, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "marked", store.MissionProposal{Title: "a walk of the Owner's buttons",
		Goal: "g", Success: []string{"done"}, NeedsOwner: "the walk tests the Owner's Start button"}, s.ask)
	ok(t, err)
	markedCard, _ := marked["question_id"].(string)
	if planCard == "" || missionCard == "" || markedCard == "" {
		t.Fatalf("cards: plan %q, mission %q, marked %q", planCard, missionCard, markedCard)
	}

	signCardAutoSeconds(t, w.rt, "5", allKinds)
	for _, id := range s.order {
		s.aged(id, 4*time.Second)
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 0 {
		t.Fatalf("at four seconds: answered %d (%v)", n, err)
	}
	for _, id := range s.order {
		s.aged(id, 6*time.Second)
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 5 {
		t.Fatalf("at six seconds: answered %d (%v)", n, err)
	}
	nothing := "the card recommends nothing: the rule takes the option that changes nothing"
	for id, want := range map[string][2]string{s.cardOf(t, stale): {"c", ""}, s.cardOf(t, plain): {"b", nothing}, "q_plannerquestion1": {"b", ""},
		planCard: {"b", nothing}, missionCard: {"b", nothing}} {
		if got := s.cards[id]; got.status != store.QuestionAnswered || got.option != want[0] || got.text != want[1] || got.by != "rule card-auto-answer v1" {
			t.Errorf("card %s: %+v; want %v", id, got, want)
		}
	}
	if s.cards[markedCard].status != "open" {
		t.Fatalf("the card of a mission that needs the Owner is %s", s.cards[markedCard].status)
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 0 {
		t.Fatalf("a second pass answered %d (%v)", n, err)
	}

	// The decisions act on the rule's answers, and name it.
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 2 {
		t.Fatalf("blocked cards decided %d (%v)", n, err)
	}
	if n, err := store.DecidePlanChanges(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("plan changes decided %d (%v)", n, err)
	}
	if n, err := store.DecideMissions(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("missions decided %d (%v)", n, err)
	}
	var decision, by, reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'decision', body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided'
	  AND body_json->>'question_id' = $1`, planCard).Scan(&decision, &by))
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE event_type = 'mission_state_changed' AND mission_id = $1
	  ORDER BY event_id DESC LIMIT 1`, mission).Scan(&reason))
	if decision != "refused" || by != "rule card-auto-answer v1" || w.state(t, "mission", mission) != "stopped" ||
		reason != "the rule card-auto-answer v1 did not start it (question "+missionCard+"): "+nothing || w.state(t, "task", plain) != "cancelled" ||
		w.state(t, "mission", "M-3") != "stopped" {
		t.Fatalf("plan change %s by %q; mission %s (%q); plain task %s; M-3 %s", decision, by, w.state(t, "mission", mission), reason,
			w.state(t, "task", plain), w.state(t, "mission", "M-3"))
	}
	for stream, want := range map[string][]string{
		store.PlanStream(root):       {"rule card-auto-answer v1 answered question " + planCard + " with b, the option that changes nothing, after 6 seconds unanswered; " + nothing + "; no AI assessment was asked"},
		store.MissionStream(mission): {"rule card-auto-answer v1 answered question " + missionCard + " with b, the option that changes nothing, after 6 seconds unanswered; " + nothing + "; no AI assessment was asked"},
	} {
		if got := ruleNotes(t, w.rt, stream); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("notes on %s: %q; want %q", stream, got, want)
		}
	}
	if s.acked[planCard] != 1 || s.acked[missionCard] != 1 {
		t.Fatalf("acknowledged: plan card %d, mission card %d", s.acked[planCard], s.acked[missionCard])
	}
}

// A plan import's and a recorder import's cards are answered as plan changes
// are: b ("Do not apply it" by any name), the import refused and named the
// rule's.
func TestImportCardsAreAnsweredByRule(t *testing.T) {
	t.Parallel()
	pool, req := importDB(t)
	var s cardStore
	out, err := store.ProposePlanImport(ctx, pool, wr("thread:s1"), "k1", "round 2 into the runtime", req, s.ask)
	ok(t, err)
	importCard, _ := out["question_id"].(string)
	out, err = store.ProposeRecorderImport(ctx, pool, wr("thread:s1"), "k2", "retire the recorder", tdLog(t), s.ask)
	ok(t, err)
	recorderCard, _ := out["question_id"].(string)
	signCardAutoSeconds(t, pool, "0", "[plan_import, recorder_import]")
	if n, err := store.AnswerByRule(ctx, pool, s.rule()); err != nil || n != 2 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, s.question, s.ack); err != nil || n != 2 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	for _, q := range []string{importCard, recorderCard} {
		var decision, by string
		ok(t, pool.QueryRow(ctx, `SELECT body_json->>'decision', body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided'
		  AND body_json->>'question_id' = $1`, q).Scan(&decision, &by))
		if got := s.cards[q]; got.option != "b" || decision != "refused" || by != "rule card-auto-answer v1" {
			t.Errorf("card %s answered %q, decided %s by %q", q, got.option, decision, by)
		}
	}
	if heads := streamEvents(t, pool, store.RecorderStream("td-336"), "legacy_imported"); heads != 0 {
		t.Fatalf("the refused recorder log was imported: %d", heads)
	}
}

// A rule that does not list a kind leaves its cards open, however long they
// wait; one that waits zero seconds answers a card the pass it sees it.
func TestARuleAnswersOnlyTheKindsItLists(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var s cardStore
	_, missionCard := proposeOutside(t, w, s.ask, "out")
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1,
		MissionID: "M-2"}}, s.ask)
	ok(t, err)
	signCardAutoSeconds(t, w.rt, "0", "[mission]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 || s.cards[missionCard].option != "b" {
		t.Fatalf("answered %d (%v); the mission's card %+v", n, err, s.cards[missionCard])
	}
	for _, id := range s.order {
		if id != missionCard && s.cards[id].status != "open" {
			t.Errorf("card %s of a kind the rule does not list was answered", id)
		}
	}
}
