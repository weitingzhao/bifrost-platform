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
	nothing := "the card recommends nothing and no AI assessment has come: the rule takes the option that changes nothing"
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

// A plan import's card recommends nothing and is answered as plan changes
// are: b ("Do not apply it" by any name), the import refused and named the
// rule's. A recorder import's card recommends importing (the Owner
// 2026-10-21 「2.3 的四条都照推荐 A」: STG's rule answers these cards), so the
// rule answers a and the log is imported, named the rule's.
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
	if rec := s.cards[recorderCard].card.Recommended; rec != "a" || s.cards[importCard].card.Recommended != "" {
		t.Fatalf("the recorder import's card recommends %q, the plan import's %q", rec, s.cards[importCard].card.Recommended)
	}
	signCardAutoSeconds(t, pool, "0", "[plan_import, recorder_import]")
	if n, err := store.AnswerByRule(ctx, pool, s.rule()); err != nil || n != 2 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, s.question, s.ack); err != nil || n != 2 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	for q, want := range map[string][2]string{importCard: {"b", "refused"}, recorderCard: {"a", "applied"}} {
		var decision, by string
		ok(t, pool.QueryRow(ctx, `SELECT body_json->>'decision', body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided'
		  AND body_json->>'question_id' = $1`, q).Scan(&decision, &by))
		if got := s.cards[q]; got.option != want[0] || decision != want[1] || by != "rule card-auto-answer v1" {
			t.Errorf("card %s answered %q, decided %s by %q; want %s, %s", q, got.option, decision, by, want[0], want[1])
		}
	}
	if heads := streamEvents(t, pool, store.RecorderStream("td-336"), "legacy_imported"); heads == 0 {
		t.Fatal("the recorder log the rule answered Import for was not imported")
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

// The coordinating thread 2026-10-20 for the Owner (「STG 不等评估」): a card
// that recommends nothing takes its AI assessment's option when one has come,
// saying why, and the decision acts on it as on the Owner's; with none come,
// the option that changes nothing.
func TestACardWithoutRecommendationTakesItsAssessment(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	assessed, bare := w.ready(t, store.Task{}), w.ready(t, store.Task{MissionID: "M-1"})
	for _, id := range []string{assessed, bare} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 2 {
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
	signCardAutoSeconds(t, w.rt, "0", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 2 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	took := "the card recommends nothing and its AI assessment recommends a: the rule takes the assessment's"
	if got := s.cards[card]; got.option != "a" || got.text != took {
		t.Fatalf("the assessed card: %+v", got)
	}
	if got := s.cards[s.cardOf(t, bare)]; got.option != "b" {
		t.Fatalf("the card with no assessment: %+v", got)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 2 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if w.state(t, "task", assessed) != "ready" || w.state(t, "task", bare) != "cancelled" {
		t.Fatalf("the assessed task is %s, the other %s", w.state(t, "task", assessed), w.state(t, "task", bare))
	}
	notes := strings.Join(ruleNotes(t, w.rt, store.MissionStream("M-1")), "|")
	if !strings.Contains(notes, "answered question "+card+" with a, the option its AI assessment recommends, after ") || !strings.Contains(notes, "; "+took) {
		t.Fatalf("notes: %q", notes)
	}
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
	plain := w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, plain) }))
	if _, err = store.AskBlocked(ctx, w.rt, s.ask); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{before: false, mission: true, markedCard: false, planCard: false, s.cardOf(t, plain): true} {
		if got := s.asked[id].ByRule; got != want {
			t.Errorf("card %s (%s) asked ByRule %v; want %v", id, s.asked[id].Title, got, want)
		}
	}
}
