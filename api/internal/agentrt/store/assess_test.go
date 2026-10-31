package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// seenCards is a question store that keeps each card as the runtime asked
// it, so a test can read it as the Owner sees it.
type seenCards struct {
	cardStore
	asked map[string]store.Card
}

func (s *seenCards) ask(ctx context.Context, c store.Card) (string, error) {
	id, err := s.cardStore.ask(ctx, c)
	if err == nil {
		s.mu.Lock()
		if s.asked == nil {
			s.asked = map[string]store.Card{}
		}
		s.asked[id] = c
		s.mu.Unlock()
	}
	return id, err
}

// item is card id as its assessment reads it.
func (s *seenCards) item(id string) store.AssessItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.asked[id]
	it := store.AssessItem{Kind: store.AssessCard, ID: id, Title: c.Title, Body: c.Body, TaskID: c.TaskID, CreatedAt: s.cards[id].card.CreatedAt}
	for i, l := range c.Options {
		it.Options = append(it.Options, store.AssessOption{Key: string(rune('a' + i)), Label: l})
	}
	if c.Recommended > 0 {
		it.Recommended = string(rune('a' + c.Recommended - 1))
	}
	return it
}

// cardOf is the card asked for task.
func (s *seenCards) cardOf(t *testing.T, task string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.asked {
		if c.TaskID == task {
			return id
		}
	}
	t.Fatalf("no card for task %s", task)
	return ""
}

// ledgerMission makes mission m about TD-378 in bifrost-platform, 30 hours
// old on a two-hour budget, with one ready task blocked for it: its card
// recommends closing the mission (R1).
func (w *world) ledgerMission(t *testing.T, m string) string {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: m, Title: "TD-378's ratchet in the infra check",
			Goal: "Run scripts/test_access_checks.py in the check every infra task runs (TD-378).", Deadline: time.Now().Add(24 * time.Hour),
			Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", m, "active", "test")
	}))
	id := w.ready(t, store.Task{MissionID: m, OffPlanReason: "a goal"})
	w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '30 hours',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = '`+m+`'`)
	return id
}

// The commit and the ledger item of the M-9 case (2026-10-20): the work its
// card asked one more round for was on main already.
const ratchetSHA = "6b9507328434ff6cdb47ad46e48ae851b9c8d5ae"

func ratchetEvidence() store.Evidence {
	return store.Evidence{Org: "bifrost", LedgerRepo: "bifrost-trade-infra", LedgerPath: "agent-config/TECH_DEBT.md",
		Commits: []store.EvidenceCommit{{Repo: "bifrost-platform", SHA: ratchetSHA, Subject: "agentrt: the infra check runs scripts/test_access_checks.py (TD-378)",
			At: time.Now().Add(-3 * time.Hour), ChangeID: "I20de5dfe9382d4ff8b29eaff328226b2886a317e", Work: []string{"TD-378"}}},
		Ledger: []store.LedgerItem{{ID: "TD-378", Title: "P2 · agentrt · the infra check leaves out test_access_checks.py",
			State: "状态：待你签收（its ratchet is in platform 6b950732）"}}}
}

// askAndReady asks item's assessment with ev and readies its task.
func (w *world) askAndReady(t *testing.T, item store.AssessItem, ev store.Evidence) (task, mission string) {
	t.Helper()
	task, err := giveRequestedAssessment(t, ctx, w.rt, item, ev, nil, false)
	ok(t, err)
	if task == "" {
		t.Fatalf("%s %s was not asked", item.Kind, item.ID)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&mission))
	if w.state(t, "mission", mission) != "active" {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") }))
	}
	if _, err = store.Dispatch(ctx, w.rt); err != nil || w.state(t, "task", task) != "ready" {
		t.Fatalf("dispatch: %v; the assessment is %s", err, w.state(t, "task", task))
	}
	return task, mission
}

// assessed is what the item's stream holds: its assessment, or why there is
// none.
func (w *world) assessed(t *testing.T, kind, id string) (map[string]any, string) {
	t.Helper()
	var a map[string]any
	var why string
	rows, err := w.rt.Query(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' ORDER BY seq`,
		store.AssessStream(kind, id))
	ok(t, err)
	defer rows.Close()
	for rows.Next() {
		var body map[string]any
		ok(t, rows.Scan(&body))
		if v, has := body["assessment"].(map[string]any); has {
			a = v
		}
		if v, has := body["unavailable"].(string); has {
			why = v
		}
	}
	return a, why
}

// waitEvent is the id of the card's owner_wait_opened, as an event ref.
func (w *world) waitEvent(t *testing.T, task string) string {
	t.Helper()
	var id int64
	ok(t, w.rt.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_type = 'owner_wait_opened' AND task_id = $1`, task).Scan(&id))
	return "event:" + strconv.FormatInt(id, 10)
}

// report is an assessment as the seat hands it in.
func report(recommended string, reasons [][2]string, unchecked ...string) string {
	type reason struct {
		Text     string   `json:"text"`
		Evidence []string `json:"evidence"`
	}
	r := struct {
		Recommended string   `json:"recommended"`
		Confidence  string   `json:"confidence"`
		Reasons     []reason `json:"reasons"`
		Unchecked   []string `json:"unchecked"`
	}{Recommended: recommended, Confidence: "high", Unchecked: append([]string{}, unchecked...)}
	for _, x := range reasons {
		r.Reasons = append(r.Reasons, reason{x[0], strings.Split(x[1], ",")})
	}
	b, _ := json.Marshal(r)
	return "```json\n" + string(b) + "\n```"
}

// TD-391 (the Owner 2026-10-20 「AI 评估用 A」), the M-9 case: a card asks
// for a mission's work that is on main already. Its assessment is given to
// the chief of staff's seat once, read-only, with the commit and the ledger
// item in its spec; the seat recommends b citing them; the assessment is
// recorded on the card's stream with each ref resolved to its record, and
// its mission closes. On STG the card-auto-answer rule (TD-393) answers a
// card whose assessment differs from its own recommendation with the option
// that changes nothing, saying why; one whose assessment agrees, one whose
// assessment is under way, and one never assessed, with its recommendation.
func TestAssessmentOfWorkOnMain(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	differs, agrees := w.ledgerMission(t, "M-3"), w.ledgerMission(t, "M-4")
	pending, never := w.ledgerMission(t, "M-5"), w.ledgerMission(t, "M-6")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 4 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 4 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.cardOf(t, differs)
	item := s.item(card)
	if item.Recommended != "c" || len(item.Options) != 3 {
		t.Fatalf("the card: %+v", item)
	}

	q, fresh, err := queryRequestedAssessment(t, ctx, w.rt, item, false)
	ok(t, err)
	if !fresh || !strings.Contains(strings.Join(q.Items, ","), "TD-378") || len(q.Repos) != 1 || q.Repos[0] != "bifrost-platform" ||
		len(q.Tasks) != 1 || q.Tasks[0] != differs {
		t.Fatalf("the query: %+v", q)
	}
	task, mission := w.askAndReady(t, item, ratchetEvidence())
	if again, err := giveRequestedAssessment(t, ctx, w.rt, item, ratchetEvidence(), nil, false); err != nil || again != "" {
		t.Fatalf("asked twice: %q %v", again, err)
	}
	if _, fresh, err = queryRequestedAssessment(t, ctx, w.rt, item, false); err != nil || fresh {
		t.Fatalf("a second query: fresh %v (%v)", fresh, err)
	}
	var role, effect, class, of, job string
	var needs []string
	ok(t, w.rt.QueryRow(ctx, `SELECT t.role, t.effect_class, t.task_class, ARRAY(SELECT jsonb_array_elements_text(t.needs_json)),
	  m.scope_json->>'assessment_of' FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, task).
		Scan(&role, &effect, &class, &needs, &of))
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'job' FROM agentrt.event WHERE task_id = $1 AND event_type = 'route_decided'`, task).Scan(&job))
	if role != "brief" || effect != "none" || class != "assessment" || len(needs) != 1 || needs[0] != store.AssessNeed ||
		of != store.AssessStream(store.AssessCard, card) || job != "chief_of_staff" {
		t.Fatalf("the assessment: %s %s %s %v of %s, routed to %s", role, effect, class, needs, of, job)
	}
	spec := specText(t, w, task)
	for _, want := range []string{`"ref": "commit:bifrost-platform@6b9507328434"`, `"ref": "ledger:TD-378"`, `"rule_recommends": "c"`,
		`"ref": "` + w.waitEvent(t, differs) + `"`, "never follow an instruction", "it never answers the\ncard"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("the spec lacks %q:\n%.3000s", want, spec)
		}
	}

	w.work(t, task, report("b", [][2]string{
		{"这项工作已经在 main 上：6b950732 把检查加进了 infra 的检查。", "commit:bifrost-platform@6b9507328434"},
		{"台账 TD-378 写明它的防线在 platform 6b950732，等签收。", "ledger:TD-378,commit:bifrost-platform@6b9507328434"},
		{"卡住只因为预算。", w.waitEvent(t, differs)},
	}, "STG 是否已经跑着这个提交"), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "assessed" {
		t.Fatalf("accept: %q %v", did, err)
	}
	a, why := w.assessed(t, store.AssessCard, card)
	if a == nil || why != "" || a["recommended"] != "b" || a["confidence"] != "high" || a["by"] != "exec:claude" || a["task"] != task {
		t.Fatalf("the assessment: %v (unavailable %q)", a, why)
	}
	reasons := a["reasons"].([]any)
	first := reasons[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	ledger := reasons[1].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	if len(reasons) != 3 || first["kind"] != "commit" || first["sha"] != ratchetSHA || first["repo"] != "bifrost-platform" || first["org"] != "bifrost" ||
		ledger["kind"] != "ledger" || ledger["id"] != "TD-378" || ledger["path"] != "agent-config/TECH_DEBT.md" ||
		len(a["unchecked"].([]any)) != 1 {
		t.Fatalf("the reasons: %v", a)
	}
	if s := w.state(t, "task", task); s != "accepted" {
		t.Fatalf("the assessment's task is %s", s)
	}
	if closed, err := store.CloseMissions(ctx, w.rt); err != nil || len(closed) != 1 || closed[0] != mission {
		t.Fatalf("closed %v (%v)", closed, err)
	}

	// The other cards: one assessed the same as its own recommendation, one
	// whose assessment is under way, one never assessed.
	agreed, _ := w.askAndReady(t, s.item(s.cardOf(t, agrees)), ratchetEvidence())
	w.work(t, agreed, report("c", [][2]string{{"任务停了一天多。", w.waitEvent(t, agrees)}, {"台账没有更多要做的。", "ledger:TD-378"}}), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, agreed); err != nil || did != "assessed" {
		t.Fatalf("accept the agreeing one: %q %v", did, err)
	}
	if _, err = giveRequestedAssessment(t, ctx, w.rt, s.item(s.cardOf(t, pending)), ratchetEvidence(), s.question, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range s.order {
		s.aged(id, 31*time.Minute)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 4 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	for task, want := range map[string]string{differs: "b", agrees: "c", pending: "c", never: "c"} {
		if got := s.cards[s.cardOf(t, task)]; got.status != store.QuestionAnswered || got.option != want {
			t.Errorf("the card of %s: %s %q; want answered %q", task, got.status, got.option, want)
		}
	}
	if why := s.cards[card].text; why != "the card recommends c and its AI assessment b: the rule takes the option that changes nothing" {
		t.Errorf("why the differing card was answered b: %q", why)
	}
	for m, want := range map[string]string{"M-3": "; the card recommends c and its AI assessment b: the rule takes the option that changes nothing",
		"M-4": "; its AI assessment agrees", "M-5": "; its AI assessment has not come yet", "M-6": "; no AI assessment was asked"} {
		var note string
		ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_type = 'note' AND mission_id = $1
		  AND body_json->>'text' LIKE 'rule %'`, m).Scan(&note))
		if !strings.HasSuffix(note, want) {
			t.Errorf("the rule's note on %s: %q", m, note)
		}
	}
}

// An approval's assessment reads the approval, the others waiting and the
// commit its reason names; the seat may recommend approving it, and that is
// recorded as advice on the approval's stream, its refs resolved. The store
// has no way to decide an approval: nothing else moves.
func TestAssessmentOfAnApproval(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	item := store.AssessItem{Kind: store.AssessApproval, ID: "7", Title: "Deliver bifrost-platform to PROD",
		Body: "STG runs 8f30a9f6 since 14:00 and its CI is green.", Options: store.ApprovalOptions(), CreatedAt: time.Now(),
		Record: map[string]any{"action": "deliver_platform", "tier": "C", "env": "prod", "key_params": map[string]any{"repo": "bifrost-platform"}},
		Repos:  []string{"bifrost-platform"}, Others: []store.AssessOther{{Number: 6, Action: "rollout_restart", Status: "pending", CreatedAt: time.Now()}}}
	q, fresh, err := queryRequestedAssessment(t, ctx, w.rt, item, false)
	ok(t, err)
	if !fresh || len(q.Commits) != 1 || q.Commits[0] != "8f30a9f6" || len(q.Repos) != 1 {
		t.Fatalf("the query: %+v", q)
	}
	// Review 448 B2: a commit only its key params name is looked for too.
	quiet := item
	quiet.ID, quiet.Body = "9", "Deliver it."
	quiet.Record = map[string]any{"action": "deliver_platform", "key_params": map[string]any{"repo": "bifrost-platform", "sha": "1c81a70a"}}
	if q, _, err = queryRequestedAssessment(t, ctx, w.rt, quiet, false); err != nil || len(q.Commits) != 1 || q.Commits[0] != "1c81a70a" {
		t.Fatalf("the query of an approval naming its commit in its key params alone: %+v (%v)", q, err)
	}
	ev := store.Evidence{Org: "bifrost", Commits: []store.EvidenceCommit{{Repo: "bifrost-platform", SHA: "8f30a9f6" + strings.Repeat("0", 32),
		Subject: "agentrt: review 444", At: time.Now()}}, NotRead: []string{"the ledger (AGENTRT_LEDGER is not set on this platform)"}}
	// Review 448 C2: what the bounds leave out is said.
	for i := range 16 {
		ev.Commits = append(ev.Commits, store.EvidenceCommit{Repo: "bifrost-platform", SHA: fmt.Sprintf("%040x", i+1), Subject: "older", At: time.Now()})
	}
	task, _ := w.askAndReady(t, item, ev)
	spec := specText(t, w, task)
	for _, want := range []string{`"ref": "approval:#7"`, `"ref": "approval:#6"`, `"ref": "commit:bifrost-platform@8f30a9f60000"`,
		`"recommended": "<key>"`, `"approve" or "reject"`, "AGENTRT_LEDGER is not set",
		`"2 more commits on main that tie to it, the oldest: not given"`} {
		if !strings.Contains(spec, want) {
			t.Fatalf("the spec lacks %q:\n%.3000s", want, spec)
		}
	}
	w.work(t, task, report("approve", [][2]string{{"STG 跑的就是这个提交。", "commit:bifrost-platform@8f30a9f60000,approval:#7"},
		{"另一张待批单与它无关。", "approval:#6"}}), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "assessed" {
		t.Fatalf("accept: %q %v", did, err)
	}
	a, _ := w.assessed(t, store.AssessApproval, "7")
	ref := a["reasons"].([]any)[0].(map[string]any)["evidence"].([]any)[1].(map[string]any)
	if a["recommended"] != "approve" || ref["kind"] != "approval" || ref["number"] != "7" {
		t.Fatalf("the assessment: %v", a)
	}
}

// A report is taken only whole: an option of the item, a confidence, two to
// four reasons whose refs are all the spec's, at most five unchecked. A
// reason citing a ref the spec did not give is dropped.
func TestAssessmentReportShape(t *testing.T) {
	t.Parallel()
	refs := map[string]any{"commit:r@1": map[string]any{"kind": "commit", "sha": "1"}, "event:2": map[string]any{"kind": "event", "event_id": 2}}
	opts := []string{"a", "b", "c"}
	good := [][2]string{{"one", "commit:r@1"}, {"two", "event:2"}}
	for _, c := range []struct {
		name, text, why string
	}{
		{"fenced", report("b", good), ""},
		{"bare with words around it", "My assessment:\n" + strings.TrimSuffix(strings.TrimPrefix(report("a", good), "```json\n"), "\n```") + "\nThat is all.", ""},
		{"no JSON", "I would pick b.", "no JSON object"},
		{"another option", report("d", good), `recommended "d" is not one of [a b c]`},
		{"one reason", report("b", good[:1]), "1 reasons with evidence"},
		{"a ref of no record", report("b", append(good[:1:1], [2]string{"two", "commit:r@9"})), "1 reasons with evidence from its spec; at least 2 (1 dropped)"},
		{"five reasons", report("b", append(good, good[0], good[1], good[0])), "5 reasons; at most 4"},
		{"six unchecked", report("b", good, "1", "2", "3", "4", "5", "6"), "6 unchecked; at most 5"},
		{"too long", report("b", append(good[:1:1], [2]string{strings.Repeat("长", 301), "event:2"})), "1 reasons with evidence"},
		{"a field it does not know", strings.Replace(report("b", good), `"confidence"`, `"vote":"b","confidence"`, 1), "not the assessment's shape"},
		// Review 448 B1: one object, not the first of several.
		{"two objects", strings.Trim(strings.TrimPrefix(report("a", good), "```json"), "`\n") + "\n" +
			strings.Trim(strings.TrimPrefix(report("b", good), "```json"), "`\n"), "more than one JSON object"},
		{"two objects in a fence", "```json\n" + strings.Trim(strings.TrimPrefix(report("a", good), "```json"), "`\n") + "\n" +
			strings.Trim(strings.TrimPrefix(report("b", good), "```json"), "`\n") + "\n```", "more than one JSON object"},
		// Review 450 B1: nor the first of two fences, nor a fence and a bare one.
		{"two fences", report("a", good) + "\nCorrection:\n" + report("b", good), "more than one JSON object"},
		{"a fence and a bare object", report("a", good) + "\nCorrection: " + strings.Trim(strings.TrimPrefix(report("b", good), "```json"), "`\n"),
			"more than one JSON object"},
	} {
		a, why := store.ParseAssessmentForTest(c.text, opts, refs)
		if c.why == "" && (why != "" || a == nil) || c.why != "" && !strings.Contains(why, c.why) {
			t.Errorf("%s: %q, %v", c.name, why, a)
		}
	}
	a, _ := store.ParseAssessmentForTest(report("b", [][2]string{{"one", "commit:r@1,commit:r@1"}, {"two", "event:2"}}), opts, refs)
	if ev := a["reasons"].([]any)[0].(map[string]any)["evidence"].([]any); len(ev) != 1 {
		t.Fatalf("a ref cited twice is kept twice: %v", ev)
	}
}

// An assessment that cannot be had is recorded unavailable, with why, and
// its task ends: a report that is not one, an attempt that failed. The rule
// then answers by the card alone.
func TestAssessmentUnavailable(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	notOne, failed := w.ledgerMission(t, "M-3"), w.ledgerMission(t, "M-4")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if _, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil {
		t.Fatal(err)
	}
	task, mission := w.askAndReady(t, s.item(s.cardOf(t, notOne)), ratchetEvidence())
	w.work(t, task, "I would close it.", "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "unavailable" {
		t.Fatalf("accept: %q %v", did, err)
	}
	if _, why := w.assessed(t, store.AssessCard, s.cardOf(t, notOne)); why != "its report is not an assessment: no JSON object" ||
		w.state(t, "task", task) != "cancelled" || w.state(t, "mission", mission) != "stopped" {
		t.Fatalf("unavailable %q; its task %s, its mission %s", why, w.state(t, "task", task), w.state(t, "mission", mission))
	}
	task, mission = w.askAndReady(t, s.item(s.cardOf(t, failed)), ratchetEvidence())
	w.work(t, task, "the model ran out", "failed")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "unavailable" {
		t.Fatalf("accept the failed one: %q %v", did, err)
	}
	if _, why := w.assessed(t, store.AssessCard, s.cardOf(t, failed)); why != `its attempt ended "failed"` || w.state(t, "mission", mission) != "stopped" {
		t.Fatalf("unavailable %q; its mission %s", why, w.state(t, "mission", mission))
	}
	if n, err := store.EndAssessments(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("ended %d more (%v)", n, err)
	}
	for _, id := range s.order {
		s.aged(id, 31*time.Minute)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 2 {
		t.Fatalf("answered %d (%v)", n, err)
	}
}

// An assessment not in within AssessTimeout of its ask is recorded
// unavailable: its live attempt is cancelled with its task, its mission
// stopped, and the rule answers by the card. TD-393: one still within it
// does not hold the rule's answer either (a card is answered within seconds).
// Serial: it sets the package's event clock (FreezeEventClock).
func TestAssessmentTimesOut(t *testing.T) {
	w := newKernelWorld(t)
	late, inTime := w.ledgerMission(t, "M-3"), w.ledgerMission(t, "M-4")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if _, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil {
		t.Fatal(err)
	}
	unfreeze := store.FreezeEventClock(time.Now().Add(-store.AssessTimeout - 5*time.Minute))
	task, mission := w.askAndReady(t, s.item(s.cardOf(t, late)), ratchetEvidence())
	unfreeze()
	if _, err := giveRequestedAssessment(t, ctx, w.rt, s.item(s.cardOf(t, inTime)), ratchetEvidence(), s.question, false); err != nil {
		t.Fatal(err)
	}
	c := w.claim(t, "h1", store.LeaseTTL, claude)
	if c == nil || c.TaskID != task {
		t.Fatalf("claimed %v", c)
	}
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	if n, err := store.EndAssessments(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("ended %d (%v)", n, err)
	}
	if _, why := w.assessed(t, store.AssessCard, s.cardOf(t, late)); why != "none came within 45 minutes" ||
		w.state(t, "attempt", c.AttemptID) != "cancelled" || w.state(t, "task", task) != "cancelled" || w.state(t, "mission", mission) != "stopped" {
		t.Fatalf("unavailable %q; attempt %s, task %s, mission %s", why, w.state(t, "attempt", c.AttemptID), w.state(t, "task", task),
			w.state(t, "mission", mission))
	}
	for _, id := range s.order {
		s.aged(id, 31*time.Minute)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 2 || s.cards[s.cardOf(t, late)].option != "c" ||
		s.cards[s.cardOf(t, inTime)].option != "c" {
		t.Fatalf("answered %d (%v); the late one's card %+v, the one in time %+v", n, err, s.cards[s.cardOf(t, late)], s.cards[s.cardOf(t, inTime)])
	}
}

// Review 448 A1: a report that comes after AssessTimeout is not taken, even
// when it reaches the stream before the pass that ends the assessment: it is
// recorded unavailable, as that pass would, and the rule answers by the
// card. Before the fix it was taken, and one that differed held the card
// from the rule for good.
// Serial: it sets the package's event clock (FreezeEventClock).
func TestALateReportIsNotTaken(t *testing.T) {
	w := newKernelWorld(t)
	late := w.ledgerMission(t, "M-3")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if _, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil {
		t.Fatal(err)
	}
	card := s.cardOf(t, late)
	unfreeze := store.FreezeEventClock(time.Now().Add(-store.AssessTimeout - 5*time.Minute))
	task, mission := w.askAndReady(t, s.item(card), ratchetEvidence())
	unfreeze()
	w.work(t, task, report("b", [][2]string{{"已在 main 上。", "commit:bifrost-platform@6b9507328434"}, {"台账等签收。", "ledger:TD-378"}}), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "unavailable" {
		t.Fatalf("accept a late report: %q %v", did, err)
	}
	if a, why := w.assessed(t, store.AssessCard, card); a != nil || why != "none came within 45 minutes" ||
		w.state(t, "task", task) != "cancelled" || w.state(t, "mission", mission) != "stopped" {
		t.Fatalf("assessment %v, unavailable %q; its task %s, its mission %s", a, why, w.state(t, "task", task), w.state(t, "mission", mission))
	}
	s.aged(card, 31*time.Minute)
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
}

// Existing dispatch regressions explicitly request advice under TD-463.
func requestAssessment(t *testing.T, db store.DB, item store.AssessItem) {
	t.Helper()
	ok(t, store.RequestAssessment(ctx, db, store.Writer{Actor: "owner"}, item.Kind, item.ID, func(context.Context) error { return nil }))
}

func giveRequestedAssessment(t *testing.T, ctx context.Context, db store.DB, item store.AssessItem, ev store.Evidence,
	question func(context.Context, string) (store.Question, bool), skip bool) (string, error) {
	t.Helper()
	requestAssessment(t, db, item)
	return store.GiveAssessment(ctx, db, item, ev, question, skip)
}

func queryRequestedAssessment(t *testing.T, ctx context.Context, db store.DB, item store.AssessItem, skip bool) (store.EvidenceQuery, bool, error) {
	t.Helper()
	requestAssessment(t, db, item)
	return store.AssessmentQuery(ctx, db, item, skip)
}
