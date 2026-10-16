package store_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A plan as a planner hands it in: two tasks (the second after the first, the
// first with a check of its own), one question, two decisions, a mission
// with new criteria, what is out of scope and a lower budget.
const goodPlan = "Two changes, the second after the first.\n\n```json\n" + `{
  "mission": {"success": ["the comment says what MainHead reads", "go vet passes"], "out_of_scope": ["the Console"], "max_attempts": 6},
  "tasks": [
    {"ref": "t1", "title": "Document MainHead", "repo": "bifrost-platform", "text": "Say which ref MainHead reads.",
     "verify": [["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"], ["go", "-C", "api", "build", "./..."]]},
    {"ref": "t2", "title": "Document GoalRepos", "repo": "bifrost-platform", "text": "Say why only the platform.", "depends_on": ["t1"]}
  ],
  "questions": [{"question": "Document the Console too?", "body": "It has no goal docs.", "options": ["Not now", "Yes, a third task"],
    "recommended": 1, "meanwhile": "two tasks go on without the Console", "owner_duty": "goal"}],
  "decisions": [{"decision": "two tasks, not one", "why": "each is one review", "dropped": "one task", "reverse": "merge them"},
    {"decision": "no new tests", "why": "comments only"}]
}` + "\n```\n"

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

// handIn readies a planning task of M-3 and has claude work it on h1 to
// submit with the given report and conclusion.
func (w *world) handIn(t *testing.T, report, conclusion string) string {
	t.Helper()
	id := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal", BaseCommit: commitA})
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
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", report)}, heads(nil, new(int)))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	return id
}

type asked struct {
	cards []store.Card
	err   error
}

func (a *asked) ask(_ context.Context, c store.Card) (string, error) {
	a.cards = append(a.cards, c)
	if a.err != nil {
		return "", a.err
	}
	return "q_" + c.Key[:8], nil
}

func TestAcceptPlanRecordsIt(t *testing.T) {
	w := planWorld(t)
	plan := w.handIn(t, goodPlan, "done")
	if ids, err := store.PendingPlans(ctx, w.rt); err != nil || !slices.Equal(ids, []string{plan}) {
		t.Fatalf("pending plans %v %v", ids, err)
	}
	var a asked
	did, err := store.AcceptPlan(ctx, w.rt, plan, a.ask)
	if err != nil || did != "accepted" || w.state(t, "task", plan) != "accepted" {
		t.Fatalf("accept: %q %v; the plan is %s", did, err, w.state(t, "task", plan))
	}
	// The card: the planner's options and default, the duty, the task, a key
	// the question store takes.
	if len(a.cards) != 1 {
		t.Fatalf("cards %v", a.cards)
	}
	c := a.cards[0]
	if c.Title != "Document the Console too?" || !slices.Equal(c.Options, []string{"Not now", "Yes, a third task"}) || c.Recommended != 1 ||
		c.OwnerDuty != "goal" || c.TaskID != plan || c.Meanwhile == "" || len(c.Key) != 64 {
		t.Fatalf("the card %+v", c)
	}
	// The tasks: implementations by the seat, on the commit the planner read,
	// the second after the first; each spec agentd runs carries the
	// repository's checks first and the plan's own after, once.
	rows, err := w.rt.Query(ctx, `SELECT t.task_id, t.title, t.role, t.effect_class, t.base_commit, t.created_by_actor_id, t.off_plan_reason,
	  t.state, b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.mission_id = 'M-3' AND t.role = 'implement' ORDER BY t.title`)
	ok(t, err)
	type row struct {
		id, title, role, effect, base, by, offPlan, state string
		spec                                              agentd.TaskSpec
	}
	var got []row
	for rows.Next() {
		var r row
		var body []byte
		ok(t, rows.Scan(&r.id, &r.title, &r.role, &r.effect, &r.base, &r.by, &r.offPlan, &r.state, &body))
		r.spec, err = agentd.ParseSpec(body)
		ok(t, err)
		got = append(got, r)
	}
	rows.Close()
	if len(got) != 2 {
		t.Fatalf("tasks %v", got)
	}
	defaults := store.Repos["bifrost-platform"].Verify
	own := []string{"go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"}
	for i, r := range got {
		want := slices.Clone(defaults)
		if r.title == "Document MainHead" {
			want = append(want, own)
		}
		if r.effect != "branch" || r.base != commitA || r.by != store.Seat || !strings.Contains(r.offPlan, plan) ||
			!slices.EqualFunc(r.spec.Verify, want, slices.Equal[[]string]) || !strings.Contains(r.spec.Text, "# "+r.title) {
			t.Fatalf("task %d: %+v", i, r)
		}
	}
	var dep string
	ok(t, w.rt.QueryRow(ctx, `SELECT depends_on_task_id FROM agentrt.task_dependency WHERE task_id = $1`, got[0].id).Scan(&dep))
	if dep != got[1].id { // Document GoalRepos after Document MainHead
		t.Fatalf("%s depends on %s, not %s", got[0].title, dep, got[1].id)
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
	// The records: who wrote what.
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
	if did, err = store.AcceptPlan(ctx, w.rt, plan, a.ask); did != "" || err != nil || len(a.cards) != 1 {
		t.Fatalf("again: %q %v, cards %d", did, err, len(a.cards))
	}
	if ids, err := store.PendingPlans(ctx, w.rt); err != nil || len(ids) != 0 {
		t.Fatalf("pending after: %v %v", ids, err)
	}
}

// A plan that does not pass blocks its task with the reason and writes
// nothing else.
func TestAcceptPlanBlocks(t *testing.T) {
	plan := func(edit func(string) string) string { return edit(goodPlan) }
	for name, c := range map[string]struct {
		report, conclusion string
		askErr             error
		why                string
	}{
		"no block": {"I could not plan this.", "done", nil, "no ```json block"},
		"failed":   {goodPlan, "failed", nil, `ended "failed"`},
		"other repo": {plan(func(s string) string {
			return strings.Replace(s, `"repo": "bifrost-platform", "text": "Say which`, `"repo": "bifrost-trade-api", "text": "Say which`, 1)
		}), "done", nil, "repo is one of bifrost-platform"},
		"shell check": {plan(func(s string) string {
			return strings.Replace(s, `["go", "-C", "api", "test"`, `["sh", "-c", "api", "test"`, 1)
		}), "done", nil, "starts with go"},
		"cycle": {plan(func(s string) string {
			return strings.Replace(s, `"verify": [["go"`, `"depends_on": ["t2"], "verify": [["go"`, 1)
		}), "done", nil, "cycle"},
		"more budget":  {plan(func(s string) string { return strings.Replace(s, `"max_attempts": 6`, `"max_attempts": 40`, 1) }), "done", nil, "raises the mission's budget"},
		"unknown key":  {plan(func(s string) string { return strings.Replace(s, `"mission": {`, `"surprise": 1, "mission": {`, 1) }), "done", nil, "does not read"},
		"bad default":  {plan(func(s string) string { return strings.Replace(s, `"recommended": 1`, `"recommended": 3`, 1) }), "done", nil, "recommended"},
		"card refused": {goodPlan, "done", store.ErrInvalid, "questions[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			w := planWorld(t)
			id := w.handIn(t, c.report, c.conclusion)
			a := asked{err: c.askErr}
			did, err := store.AcceptPlan(ctx, w.rt, id, a.ask)
			var reason string
			ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
			  ORDER BY event_id DESC LIMIT 1`, id).Scan(&reason))
			if err != nil || did != "blocked" || w.state(t, "task", id) != "blocked" || !strings.Contains(reason, c.why) {
				t.Fatalf("%q %v; %s: %q", did, err, w.state(t, "task", id), reason)
			}
			var tasks, records int
			ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-3' AND role = 'implement'),
			  (SELECT count(*) FROM agentrt.event WHERE event_type IN ('mission_amended','decision_recorded'))`).Scan(&tasks, &records))
			if tasks != 0 || records != 0 {
				t.Fatalf("a blocked plan wrote %d tasks, %d records", tasks, records)
			}
		})
	}
}

// The question store failing leaves the plan submitted and writes nothing;
// the next pass asks again under the same key and records the plan.
func TestAcceptPlanWaitsForTheQuestionStore(t *testing.T) {
	w := planWorld(t)
	id := w.handIn(t, goodPlan, "done")
	a := asked{err: errors.New("the question store is down")}
	if did, err := store.AcceptPlan(ctx, w.rt, id, a.ask); !errors.Is(err, store.ErrAsk) || did != "" || w.state(t, "task", id) != "submitted" {
		t.Fatalf("store down: %q %v %s", did, err, w.state(t, "task", id))
	}
	if did, err := store.AcceptPlan(ctx, w.rt, id, nil); !errors.Is(err, store.ErrAsk) || did != "" {
		t.Fatalf("no store: %q %v", did, err)
	}
	a.err = nil
	if did, err := store.AcceptPlan(ctx, w.rt, id, a.ask); err != nil || did != "accepted" {
		t.Fatalf("store back: %q %v", did, err)
	}
	if len(a.cards) != 2 || a.cards[0].Key != a.cards[1].Key {
		t.Fatalf("the card's key changed between passes: %v", a.cards)
	}
}

// A goal end to end in the store: ProposeGoal writes the mission and its
// planning task; once the mission is active, dispatch readies the task for the
// planner job, claude takes it, and the plan it hands in is recorded.
func TestGoalToPlan(t *testing.T) {
	w := newKernelWorld(t)
	out, err := store.ProposeGoal(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "g1",
		store.Goal{Title: "Document the goal route", Text: "Document the goal route", Repo: "bifrost-platform", Base: commitA})
	ok(t, err)
	mission, plan := out["mission_id"].(string), out["task_id"].(string)
	if out["mission_state"] != "proposed" || w.state(t, "task", plan) != "proposed" {
		t.Fatalf("no envelope: %v; the plan is %s", out, w.state(t, "task", plan))
	}
	again, err := store.ProposeGoal(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "g1",
		store.Goal{Title: "Document the goal route", Text: "Document the goal route", Repo: "bifrost-platform", Base: commitB})
	if err != nil || again["task_id"] != plan || again["base_commit"] != commitA {
		t.Fatalf("a retry: %v %v", again, err)
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
	c := w.claim(t, "h1", store.LeaseTTL, claude)
	if c == nil || c.TaskID != plan || c.Effect != "none" {
		t.Fatalf("claude claimed %+v", c)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", goodPlan)}, heads(nil, new(int)))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	var a asked
	if did, err := store.AcceptPlan(ctx, w.rt, plan, a.ask); err != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, err)
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = $1 AND role = 'implement' AND state = 'proposed'`, mission).Scan(&n))
	if n != 2 {
		t.Fatalf("implementation tasks: %d", n)
	}
	// The first is ready at once; the second waits for it.
	if _, err = store.Dispatch(ctx, w.rt); err != nil {
		t.Fatal(err)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = $1 AND role = 'implement' AND state = 'ready'`, mission).Scan(&n))
	if n != 1 {
		t.Fatalf("ready implementation tasks: %d", n)
	}
}

func TestParsePlan(t *testing.T) {
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
	for name, c := range map[string]struct{ report, why string }{
		"two values":  {"```json\n{\"tasks\": []} {}\n```", "more than one value"},
		"no tasks":    {"```json\n{\"tasks\": []}\n```", "1 to 8 tasks"},
		"empty title": {strings.Replace(goodPlan, `"title": "Document MainHead"`, `"title": " "`, 1), "title is empty"},
		"bad ref":     {strings.Replace(goodPlan, `"ref": "t1"`, `"ref": "t 1"`, 1), "ref is 1 to"},
		"self dep":    {strings.Replace(goodPlan, `"depends_on": ["t1"]`, `"depends_on": ["t2"]`, 1), "not another task"},
		"duty":        {strings.Replace(goodPlan, `"owner_duty": "goal"`, `"owner_duty": "taste"`, 1), "owner_duty"},
		"one option":  {strings.Replace(goodPlan, `["Not now", "Yes, a third task"]`, `["Not now"]`, 1), "2 to 4 options"},
		"control":     {strings.Replace(goodPlan, `two tasks, not one`, "two tasks\\u001b, not one", 1), "control character"},
		"no why":      {strings.Replace(goodPlan, `"why": "comments only"`, `"why": ""`, 1), "why is empty"},
	} {
		if _, err := store.ParsePlan([]byte(c.report), repos); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
}
