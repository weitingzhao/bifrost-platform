package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A plan as a planner hands it in: two tasks (the second after the first, the
// first with two checks of its own), one question, two decisions, a mission
// with new criteria, what is out of scope and a lower budget.
const goodPlan = "Two changes, the second after the first.\n\n```json\n" + `{
  "mission": {"success": ["the comment says what MainHead reads", "go vet passes"], "out_of_scope": ["the Console"], "max_attempts": 6},
  "tasks": [
    {"ref": "t1", "title": "Document MainHead", "repo": "bifrost-platform", "text": "Say which ref MainHead reads.",
     "verify": [["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"], ["go", "-C", "api", "test", "-run", "TestGoal", "./internal/agentrt/agentrtapi/"]]},
    {"ref": "t2", "title": "Document GoalRepos", "repo": "bifrost-platform", "text": "Say why only the platform.", "depends_on": ["t1"]}
  ],
  "questions": [{"question": "Document the Console too?", "body": "It has no goal docs.", "options": ["Not now", "Yes, a third task"],
    "recommended": 1, "meanwhile": "two tasks go on without the Console", "owner_duty": "goal"}],
  "decisions": [{"decision": "two tasks, not one", "why": "each is one review", "dropped": "one task", "reverse": "merge them"},
    {"decision": "no new tests", "why": "comments only"}]
}` + "\n```\n"

// twoCards is goodPlan with a second question.
var twoCards = strings.Replace(goodPlan, `"owner_duty": "goal"}],`, `"owner_duty": "goal"},
  {"question": "Name the planner in the comments?", "options": ["No", "Yes"], "recommended": 1, "meanwhile": "no names", "owner_duty": "goal"}],`, 1)

// planWorld is a kernel world with mission M-3 on bifrost-platform, active.
func planWorld(t *testing.T) *world {
	t.Helper()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess},
			MaxAttempts: 10, MaxWallHours: 24}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "active", "test")
	}))
	return w
}

// handIn readies a planning task of M-3 and has claude work it to submit.
func (w *world) handIn(t *testing.T, report, conclusion string) string {
	t.Helper()
	id := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal", BaseCommit: commitA})
	w.work(t, id, report, conclusion)
	return id
}

// work has claude claim the ready planning task id on h1 and submit the
// report with the given conclusion.
func (w *world) work(t *testing.T, id, report, conclusion string) {
	t.Helper()
	c := w.claim(t, "h1", store.LeaseTTL, claude)
	if c == nil || c.TaskID != id {
		t.Fatalf("claude claimed %v, not the plan %s", c, id)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	if conclusion != "done" {
		exit = 1
	}
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", conclusion)
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", report+"\n"+id+conclusion)}, heads(nil, new(int)))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
}

// cards is a question store: it makes a card per key, refuses the keys in
// refuse as invalid, fails while down, and records withdrawals.
type cards struct {
	mu        sync.Mutex
	asked     []store.Card
	ids       map[string]string
	refuse    map[int]bool // by place in the plan
	before    map[int]bool // held under another key: already asked
	down      error
	failWith  int // withdrawals that fail before one works
	withdrawn []string
}

func (s *cards) ask(_ context.Context, c store.Card) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, c)
	if s.down != nil {
		return "", s.down
	}
	if s.refuse[len(s.asked)-1] {
		return "", store.ErrInvalid
	}
	if s.before[len(s.asked)-1] {
		return "", fmt.Errorf("%w: %s", store.ErrAsked, "already answered: #7") // as the server's Ask words it
	}
	if s.ids == nil {
		s.ids = map[string]string{}
	}
	if s.ids[c.Key] == "" {
		s.ids[c.Key] = "q_" + c.Key[:8]
	}
	return s.ids[c.Key], nil
}

func (s *cards) withdraw(_ context.Context, id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reason == "" {
		return errors.New("no reason")
	}
	if s.failWith > 0 {
		s.failWith--
		return errors.New("the question store is down")
	}
	s.withdrawn = append(s.withdrawn, id)
	return nil
}

func TestAcceptPlanRecordsIt(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	plan := w.handIn(t, goodPlan, "done")
	if ids, err := store.PendingPlans(ctx, w.rt); err != nil || !slices.Equal(ids, []string{plan}) {
		t.Fatalf("pending plans %v %v", ids, err)
	}
	var s cards
	did, err := store.AcceptPlan(ctx, w.rt, plan, s.ask, s.withdraw)
	if err != nil || did != "accepted" || w.state(t, "task", plan) != "accepted" {
		t.Fatalf("accept: %q %v; the plan is %s", did, err, w.state(t, "task", plan))
	}
	// The card: the planner's options and default, the duty, the task, a key
	// the question store takes.
	if len(s.asked) != 1 || len(s.withdrawn) != 0 {
		t.Fatalf("cards %v, withdrawn %v", s.asked, s.withdrawn)
	}
	c := s.asked[0]
	if c.Title != "Document the Console too?" || !slices.Equal(c.Options, []string{"Not now", "Yes, a third task"}) || c.Recommended != 1 ||
		c.OwnerDuty != "goal" || c.TaskID != plan || c.Meanwhile == "" || len(c.Key) != 64 {
		t.Fatalf("the card %+v", c)
	}
	// The tasks: implementations by the seat, on the commit the planner read,
	// the second after the first; each spec agentd runs carries the
	// repository's checks first and the plan's own after.
	rows, err := w.rt.Query(ctx, `SELECT t.task_id, t.title, t.effect_class, t.base_commit, t.created_by_actor_id, t.off_plan_reason,
	  b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.mission_id = 'M-3' AND t.role = 'implement' ORDER BY t.title`)
	ok(t, err)
	type row struct {
		id, title, effect, base, by, offPlan string
		spec                                 agentd.TaskSpec
	}
	var got []row
	for rows.Next() {
		var r row
		var body []byte
		ok(t, rows.Scan(&r.id, &r.title, &r.effect, &r.base, &r.by, &r.offPlan, &body))
		r.spec, err = agentd.ParseSpec(body)
		ok(t, err)
		got = append(got, r)
	}
	rows.Close()
	if len(got) != 2 || got[0].title != "Document GoalRepos" {
		t.Fatalf("tasks %v", got)
	}
	defaults := store.Repos["bifrost-platform"].Verify
	own := [][]string{{"go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"},
		{"go", "-C", "api", "test", "-run", "TestGoal", "./internal/agentrt/agentrtapi/"}}
	for _, r := range got {
		want := slices.Clone(defaults)
		if r.title == "Document MainHead" {
			want = append(want, own...)
		}
		if r.effect != "branch" || r.base != commitA || r.by != store.Seat || !strings.Contains(r.offPlan, plan) ||
			!slices.EqualFunc(r.spec.Verify, want, slices.Equal[[]string]) || !strings.Contains(r.spec.Text, "# "+r.title) {
			t.Fatalf("task %+v", r)
		}
	}
	var dep string
	ok(t, w.rt.QueryRow(ctx, `SELECT depends_on_task_id FROM agentrt.task_dependency WHERE task_id = $1`, got[0].id).Scan(&dep))
	if dep != got[1].id {
		t.Fatalf("Document GoalRepos depends on %s, not on Document MainHead %s", dep, got[1].id)
	}
	// The mission, amended by the seat: criteria replaced, scope joined, the
	// budget lowered and the rest kept.
	var success, scope, budget map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT success_json, scope_json, budget_json FROM agentrt.mission WHERE mission_id = 'M-3'`).Scan(&success, &scope, &budget))
	if crit, _ := success["criteria"].([]any); len(crit) != 2 || crit[0] != "the comment says what MainHead reads" {
		t.Fatalf("success %v", success)
	}
	if out, _ := scope["out_of_scope"].([]any); len(out) != 1 || scope["max_action"] != "branch" || scope["repos"] == nil {
		t.Fatalf("scope %v", scope)
	}
	if budget["max_attempts"] != float64(6) || budget["max_wall_hours"] != float64(24) {
		t.Fatalf("budget %v", budget)
	}
	for typ, want := range map[string]int{"mission_amended": 1, "decision_recorded": 2} {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1 AND actor_id = $2 AND task_id = $3`,
			typ, store.Seat, plan).Scan(&n))
		if n != want {
			t.Fatalf("%s by the seat: %d, want %d", typ, n, want)
		}
	}
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type = 'decision_recorded' ORDER BY event_id LIMIT 1`).Scan(&body))
	if body["decision"] != "two tasks, not one" || body["dropped"] != "one task" || body["reverse"] != "merge them" {
		t.Fatalf("the first decision %v", body)
	}
	// Used once: another pass finds nothing to do and asks nothing.
	if did, err = store.AcceptPlan(ctx, w.rt, plan, s.ask, s.withdraw); did != "" || err != nil || len(s.asked) != 1 {
		t.Fatalf("again: %q %v, cards %d", did, err, len(s.asked))
	}
	if ids, err := store.PendingPlans(ctx, w.rt); err != nil || len(ids) != 0 {
		t.Fatalf("pending after: %v %v", ids, err)
	}
}

// nothingWritten fails when a refused plan wrote tasks or records.
func nothingWritten(t *testing.T, w *world) {
	t.Helper()
	var tasks, records int
	ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-3' AND role = 'implement'),
	  (SELECT count(*) FROM agentrt.event WHERE event_type IN ('mission_amended','decision_recorded'))`).Scan(&tasks, &records))
	if tasks != 0 || records != 0 {
		t.Fatalf("a refused plan wrote %d tasks, %d records", tasks, records)
	}
}

func lastReason(t *testing.T, w *world, task string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' IN ('rework','blocked') ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	return reason
}

// A plan that does not pass goes back for another round: the task is ready
// again in round 2 with the reason added to its spec, and nothing else is
// written (review 216 A3).
func TestAcceptPlanReworks(t *testing.T) {
	t.Parallel()
	plan := func(old, new string) string { return strings.Replace(goodPlan, old, new, 1) }
	for name, c := range map[string]struct {
		report, conclusion string
		why                string
	}{
		"no block":    {"I could not plan this.", "done", "no ```json block"},
		"failed":      {goodPlan, "failed", `ended "failed"`},
		"other repo":  {plan(`"repo": "bifrost-platform", "text": "Say which`, `"repo": "bifrost-trade-api", "text": "Say which`), "done", "repo is one of bifrost-platform"},
		"go run":      {plan(`["go", "-C", "api", "test", "./internal`, `["go", "-C", "api", "run", "./internal`), "done", "a check is go -C <api> test"},
		"cycle":       {plan(`"verify": [["go"`, `"depends_on": ["t2"], "verify": [["go"`), "done", "cycle"},
		"more budget": {plan(`"max_attempts": 6`, `"max_attempts": 40`), "done", "raises the mission's budget"},
		"unknown key": {plan(`"mission": {`, `"surprise": 1, "mission": {`), "done", "does not read"},
		"bad default": {plan(`"recommended": 1`, `"recommended": 3`), "done", "recommended"},
	} {
		t.Run(name, func(t *testing.T) {
			w := planWorld(t)
			id := w.handIn(t, c.report, c.conclusion)
			var s cards
			did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw)
			if err != nil || did != "rework" || w.state(t, "task", id) != "ready" || !strings.Contains(lastReason(t, w, id), c.why) {
				t.Fatalf("%q %v; %s: %q", did, err, w.state(t, "task", id), lastReason(t, w, id))
			}
			var round int
			var spec []byte
			ok(t, w.rt.QueryRow(ctx, `SELECT t.round_no, b.body FROM agentrt.task t JOIN agentrt.artifact_blob b
			  ON b.artifact_id = t.spec_artifact_id WHERE t.task_id = $1`, id).Scan(&round, &spec))
			ts, err := agentd.ParseSpec(spec)
			ok(t, err)
			if round != 2 || !strings.Contains(ts.Text, "## Round 2") || !strings.Contains(ts.Text, c.why) {
				t.Fatalf("round %d, spec:\n%s", round, ts.Text)
			}
			nothingWritten(t, w)
		})
	}
}

// Round 2 with a plan that passes is used; round 2 refused again blocks the
// task; a closed mission blocks it at once.
func TestAcceptPlanRounds(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	var s cards
	id := w.handIn(t, "no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "rework" || err != nil {
		t.Fatalf("round 1: %q %v", did, err)
	}
	w.work(t, id, goodPlan, "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "accepted" || err != nil {
		t.Fatalf("round 2 with a plan: %q %v", did, err)
	}

	w = planWorld(t)
	id = w.handIn(t, "no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "rework" || err != nil {
		t.Fatalf("round 1: %q %v", did, err)
	}
	w.work(t, id, "still no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "blocked" || err != nil ||
		w.state(t, "task", id) != "blocked" || !strings.Contains(lastReason(t, w, id), "round 2 of 2") {
		t.Fatalf("round 2 refused: %q %v %s %q", did, err, w.state(t, "task", id), lastReason(t, w, id))
	}
	nothingWritten(t, w)

	w = planWorld(t)
	id = w.handIn(t, goodPlan, "done")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-3", "stopped", "test") }))
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "blocked" || err != nil ||
		!strings.Contains(lastReason(t, w, id), "the mission is stopped") {
		t.Fatalf("a stopped mission: %q %v %q", did, err, lastReason(t, w, id))
	}
}

// The second of two cards refused: the round goes back and the first card,
// already made, is withdrawn (review 216 B2).
func TestAcceptPlanWithdrawsARefusedRoundsCards(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	id := w.handIn(t, twoCards, "done")
	s := cards{refuse: map[int]bool{1: true}}
	did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw)
	if err != nil || did != "rework" || !strings.Contains(lastReason(t, w, id), "questions[1]") {
		t.Fatalf("%q %v %q", did, err, lastReason(t, w, id))
	}
	if len(s.asked) != 2 || !slices.Equal(s.withdrawn, []string{s.ids[s.asked[0].Key]}) {
		t.Fatalf("asked %d, withdrawn %v (made %v)", len(s.asked), s.withdrawn, s.ids)
	}
	nothingWritten(t, w)
	// Round 2 asks under keys of its own.
	w.work(t, id, goodPlan, "done")
	s.refuse = nil
	if did, err = store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "accepted" || err != nil {
		t.Fatalf("round 2: %q %v", did, err)
	}
	if s.asked[2].Key == s.asked[0].Key {
		t.Fatal("round 2's card has round 1's key")
	}
}

// A withdrawal that fails leaves the plan submitted, nothing written, and
// the next pass does it again: the same card made, withdrawn, the round
// refused (review 217 B1).
func TestAcceptPlanRetriesAFailedWithdrawal(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	id := w.handIn(t, twoCards, "done")
	s := cards{refuse: map[int]bool{1: true, 3: true}, failWith: 1}
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); !errors.Is(err, store.ErrAsk) || did != "" ||
		w.state(t, "task", id) != "submitted" || len(s.withdrawn) != 0 {
		t.Fatalf("the withdrawal failed: %q %v %s %v", did, err, w.state(t, "task", id), s.withdrawn)
	}
	nothingWritten(t, w)
	did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw)
	if err != nil || did != "rework" || s.asked[2].Key != s.asked[0].Key || !slices.Equal(s.withdrawn, []string{s.ids[s.asked[0].Key]}) {
		t.Fatalf("the next pass: %q %v, asked %d, withdrawn %v", did, err, len(s.asked), s.withdrawn)
	}
}

// A question the store holds already under another key (an earlier round's,
// answered): the plan goes on with its default, the card is not its own to
// withdraw, and the task says it was asked before (review 217 B2).
func TestAcceptPlanAskedBefore(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	id := w.handIn(t, twoCards, "done")
	s := cards{before: map[int]bool{0: true}}
	did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw)
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  ORDER BY event_id DESC LIMIT 1`, id).Scan(&reason))
	if err != nil || did != "accepted" || !strings.Contains(reason, "asked before: already answered: #7") || len(s.ids) != 1 {
		t.Fatalf("%q %v %q (made %v)", did, err, reason, s.ids)
	}
}

// The question store failing leaves the plan submitted and writes nothing;
// the next pass asks again under the same key and records the plan.
func TestAcceptPlanWaitsForTheQuestionStore(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	id := w.handIn(t, goodPlan, "done")
	s := cards{down: errors.New("the question store is down")}
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); !errors.Is(err, store.ErrAsk) || did != "" || w.state(t, "task", id) != "submitted" {
		t.Fatalf("store down: %q %v %s", did, err, w.state(t, "task", id))
	}
	if did, err := store.AcceptPlan(ctx, w.rt, id, nil, nil); !errors.Is(err, store.ErrAsk) || did != "" {
		t.Fatalf("no store: %q %v", did, err)
	}
	s.down = nil
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); err != nil || did != "accepted" {
		t.Fatalf("store back: %q %v", did, err)
	}
	if len(s.asked) != 2 || s.asked[0].Key != s.asked[1].Key || len(s.withdrawn) != 0 {
		t.Fatalf("the card's key changed between passes, or it was withdrawn: %v %v", s.asked, s.withdrawn)
	}
}

// AcceptPlan and a batch of tasks over HTTP on the same mission at once
// (review 216 A2): the batch holds the dependency graph and waits; the plan
// waits for the graph before it writes any event, so the batch finishes and
// then the plan does, neither aborted as a deadlock.
//
// Serial: it holds the package's test hook (SetHook).
func TestAcceptPlanAndABatchTogether(t *testing.T) {
	w := planWorld(t)
	id := w.handIn(t, goodPlan, "done")
	held, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	defer store.SetHook(func(point, _ string) {
		if point == "batch locked" && first.CompareAndSwap(false, true) {
			close(held)
			<-release
		}
	})()
	var batchErr, planErr error
	var did string
	var wg sync.WaitGroup
	other := w.db.Connect(t, w.db.Runtime)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, batchErr = store.ProposeTasks(ctx, other, store.Writer{Actor: "owner", Token: "admin"}, "b1", "M-3", []store.TaskProposal{
			{Ref: "a", OffPlanReason: "by hand", Role: "implement", Class: "go", Title: "a", SpecArtifact: w.spec, Effect: "branch", Repo: "bifrost-platform"},
			{Ref: "b", OffPlanReason: "by hand", Role: "implement", Class: "go", Title: "b", SpecArtifact: w.spec, Effect: "branch", Repo: "bifrost-platform", DependsOn: []string{"a"}}})
	}()
	<-held
	wg.Add(1)
	go func() {
		defer wg.Done()
		var s cards
		did, planErr = store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw)
	}()
	// The plan is waiting for a lock before the batch goes on.
	deadline := time.Now().Add(10 * time.Second)
	watch := w.db.Connect(t, w.db.Admin) // only a superuser sees other roles' waits
	for {
		var waiting int
		ok(t, watch.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting))
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the plan never waited for the batch's lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	if batchErr != nil || planErr != nil || did != "accepted" {
		t.Fatalf("batch %v; plan %q %v", batchErr, did, planErr)
	}
}

// A goal end to end in the store: ProposeGoal writes the mission and its
// planning task; a retry gets the first answer, also while main cannot be
// read; once the mission is active, dispatch readies the task for the
// planner job, claude takes it, and the plan it hands in is recorded.
func TestGoalToPlan(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	at := func(commit string, err error) func(context.Context, string) (string, error) {
		return func(context.Context, string) (string, error) { return commit, err }
	}
	goal := store.Goal{Title: "Document the goal route", Text: "Document the goal route", Repos: []string{"bifrost-platform"}, Main: at(commitA, nil)}
	out, err := store.ProposeGoal(ctx, w.rt, owner, "g1", goal)
	ok(t, err)
	mission, plan := out["mission_id"].(string), out["task_id"].(string)
	if out["mission_state"] != "proposed" || w.state(t, "task", plan) != "proposed" {
		t.Fatalf("no envelope: %v; the plan is %s", out, w.state(t, "task", plan))
	}
	for _, main := range []func(context.Context, string) (string, error){at(commitB, nil), at("", errors.New("gitea is down"))} {
		goal.Main = main
		again, err := store.ProposeGoal(ctx, w.rt, owner, "g1", goal)
		if err != nil || again["task_id"] != plan || again["base_commit"] != commitA {
			t.Fatalf("a retry: %v %v", again, err)
		}
	}
	goal.Text = "Another goal"
	if _, err = store.ProposeGoal(ctx, w.rt, owner, "g2", goal); !errors.Is(err, store.ErrNoMain) {
		t.Fatalf("a new goal while main cannot be read: %v", err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") }))
	if _, err = store.Dispatch(ctx, w.rt); err != nil || w.state(t, "task", plan) != "ready" {
		t.Fatalf("dispatch: %v; the plan is %s", err, w.state(t, "task", plan))
	}
	var job string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'job' FROM agentrt.event WHERE task_id = $1 AND event_type = 'route_decided'`, plan).Scan(&job))
	if job != "planner" {
		t.Fatalf("the plan is routed to %s", job)
	}
	w.work(t, plan, goodPlan, "done")
	var s cards
	if did, err := store.AcceptPlan(ctx, w.rt, plan, s.ask, s.withdraw); err != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, err)
	}
	if _, err = store.Dispatch(ctx, w.rt); err != nil {
		t.Fatal(err)
	}
	// The first is ready at once; the second waits for it.
	var proposed, ready int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state = 'proposed'), count(*) FILTER (WHERE state = 'ready')
	  FROM agentrt.task WHERE mission_id = $1 AND role = 'implement'`, mission).Scan(&proposed, &ready))
	if proposed != 1 || ready != 1 {
		t.Fatalf("implementation tasks: %d proposed, %d ready", proposed, ready)
	}
}

func TestParsePlan(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	p, err := store.ParsePlan([]byte(goodPlan), repos)
	if err != nil || len(p.Tasks) != 2 || len(p.Questions) != 1 || len(p.Decisions) != 2 || p.Mission.MaxAttempts != 6 {
		t.Fatalf("%+v %v", p, err)
	}
	// The last block is the plan.
	two := "```json\n{\"tasks\": []}\n```\n" + goodPlan
	if p, err = store.ParsePlan([]byte(two), repos); err != nil || len(p.Tasks) != 2 {
		t.Fatalf("the last block: %+v %v", p, err)
	}
	check := func(verify string) string {
		return strings.Replace(goodPlan, `["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"]`, verify, 1)
	}
	label := func(s string) string { return strings.Replace(goodPlan, `"Not now"`, `"`+s+`"`, 1) }
	for name, c := range map[string]struct{ report, why string }{
		"two values":     {"```json\n{\"tasks\": []} {}\n```", "more than one value"},
		"no tasks":       {"```json\n{\"tasks\": []}\n```", "1 to 8 tasks"},
		"empty title":    {strings.Replace(goodPlan, `"title": "Document MainHead"`, `"title": " "`, 1), "title is empty"},
		"bad ref":        {strings.Replace(goodPlan, `"ref": "t1"`, `"ref": "t 1"`, 1), "ref is 1 to"},
		"self dep":       {strings.Replace(goodPlan, `"depends_on": ["t1"]`, `"depends_on": ["t2"]`, 1), "not another task"},
		"duty":           {strings.Replace(goodPlan, `"owner_duty": "goal"`, `"owner_duty": "taste"`, 1), "owner_duty"},
		"one option":     {strings.Replace(goodPlan, `["Not now", "Yes, a third task"]`, `["Not now"]`, 1), "2 to 4 options"},
		"control":        {strings.Replace(goodPlan, `two tasks, not one`, "two tasks\\u001b, not one", 1), "control character"},
		"no why":         {strings.Replace(goodPlan, `"why": "comments only"`, `"why": ""`, 1), "why is empty"},
		"go run":         {check(`["go", "-C", "api", "run", "./payload.go"]`), "a check is"},
		"go generate":    {check(`["go", "-C", "api", "generate", "./..."]`), "a check is"},
		"exec":           {check(`["go", "-C", "api", "test", "-exec=/bin/sh", "./..."]`), "not a package path"},
		"toolexec":       {check(`["go", "-C", "api", "build", "-toolexec=/bin/sh", "./..."]`), "a check is"},
		"other dir":      {check(`["go", "-C", "/tmp", "test", "./..."]`), "a check is"},
		"no -C":          {check(`["go", "test", "./..."]`), "a check is"},
		"up and out":     {check(`["go", "-C", "api", "test", "./../../x"]`), "not a package path"},
		"run then flag":  {check(`["go", "-C", "api", "test", "-run", "-exec", "./..."]`), "-run takes"},
		"run no package": {check(`["go", "-C", "api", "test", "-run", "TestX"]`), "-run takes"},
		"sh":             {check(`["sh", "-c", "true"]`), "a check is"},
		// The question store counts a label's JSON encoding, quotes included:
		// 54 three-byte characters are 164 bytes.
		"label bytes": {label(strings.Repeat("字", 54)), "164 bytes encoded"},
		"label 159":   {label(strings.Repeat("a", 159)), "161 bytes encoded"},
	} {
		if _, err := store.ParsePlan([]byte(c.report), repos); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
	for name, report := range map[string]string{
		"label 158":     label(strings.Repeat("a", 158)),
		"label 52 字":    label(strings.Repeat("字", 52)),
		"whole module":  check(`["go", "-C", "api", "test", "./..."]`),
		"two packages":  check(`["go", "-C", "api", "test", "./internal/a/", "./internal/b/..."]`),
		"a run pattern": check(`["go", "-C", "api", "test", "-run", "^TestGoal(Route|Title)$", "./internal/agentrt/agentrtapi/"]`),
	} {
		if _, err := store.ParsePlan([]byte(report), repos); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
}
