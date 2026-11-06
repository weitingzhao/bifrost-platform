package store_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A plan is refused only for what its planner got wrong (R5:5.7, PROD M-342):
// a key the reader does not use, a fence with no language and a base the goal
// did not keep are not the planner's mistakes, and cost no round.

// unknownKeysPlan is goodPlan with keys a plan does not have at the top
// level, in mission and in a task.
var unknownKeysPlan = strings.NewReplacer(
	`"mission": {`, `"goal": "document the goal route", "mission": {"budget": {"hours": 2}, `,
	`{"ref": "t1", `, `{"ref": "t1", "goal": "say which ref", `,
).Replace(goodPlan)

func TestPlanGateUnknownKeys(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	p, err := store.ParsePlan([]byte(unknownKeysPlan), repos)
	if err != nil || len(p.Tasks) != 2 || p.Mission.MaxAttempts != 6 {
		t.Fatalf("a plan with unknown keys: %+v %v", p, err)
	}
	if want := []string{"goal", "mission.budget", "tasks[0].goal"}; !slices.Equal(p.Ignored, want) {
		t.Fatalf("ignored %v, want %v", p.Ignored, want)
	}
	if err = store.PlanBlock([]byte(unknownKeysPlan)); err != nil {
		t.Fatalf("PlanBlock: %v", err)
	}
	// A key matches a field whatever its case, so it is not ignored.
	if p, err = store.ParsePlan([]byte(strings.Replace(goodPlan, `"mission":`, `"Mission":`, 1)), repos); err != nil || len(p.Ignored) != 0 {
		t.Fatalf("a key in another case: %v %v", p.Ignored, err)
	}
	// A key named twice is still refused, a known one or not.
	for name, report := range map[string]string{
		"known":   strings.Replace(goodPlan, `"mission": {`, `"tasks": [], "mission": {`, 1),
		"unknown": strings.Replace(unknownKeysPlan, `"mission": {`, `"Goal": "again", "mission": {`, 1),
	} {
		if _, err := store.ParsePlan([]byte(report), repos); err == nil || !strings.Contains(err.Error(), "twice in one object") {
			t.Errorf("%s twice: %v", name, err)
		}
	}
}

func TestPlanGateFences(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	plain := strings.Replace(goodPlan, "```json\n", "```\n", 1)
	upper := strings.Replace(goodPlan, "```json\n", "```JSON\n", 1)
	oneTask := "```\n" + `{"tasks": [{"ref": "t1", "title": "One", "repo": "bifrost-platform", "text": "One."}]}` + "\n```\n"
	for name, c := range map[string]struct {
		report string
		tasks  int
	}{
		"no language":           {plain, 2},
		"JSON":                  {upper, 2},
		"after a go block":      {"```go\nfunc x() {}\n```\n\n" + plain, 2},
		"the last plain one":    {plain + "\n" + oneTask, 1},
		"json before plain":     {goodPlan + "\n" + oneTask, 2},
		"json after plain":      {oneTask + "\n" + goodPlan, 2},
		"a plain block of text": {plain + "\n```\nnot a plan\n```\n", 2},
	} {
		p, err := store.ParsePlan([]byte(c.report), repos)
		if err != nil || len(p.Tasks) != c.tasks {
			t.Errorf("%s: %d tasks, %v (want %d)", name, len(p.Tasks), err, c.tasks)
		}
	}
	for name, report := range map[string]string{
		"no block":       "# Plan\n\nNo block.\n",
		"a go block":     strings.Replace(goodPlan, "```json\n", "```go\n", 1),
		"plain, no {":    "```\n[1, 2]\n```\n",
		"never closed":   "```\n{\"tasks\": []}\n",
		"lower-case one": strings.Replace(goodPlan, "```json\n", "```Json\n", 1),
	} {
		err := store.PlanBlock([]byte(report))
		_, perr := store.ParsePlan([]byte(report), repos)
		if err == nil || err.Error() != "the report has no ```json block" || perr == nil || perr.Error() != err.Error() {
			t.Errorf("%s: %v, ParsePlan %v", name, err, perr)
		}
	}
}

// A plan used with unknown keys says which in its reason.
func TestPlanGateIgnoredKeysReason(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	plan := w.handIn(t, unknownKeysPlan, "done")
	var s cards
	if did, err := store.AcceptPlan(ctx, w.rt, plan, s.ask, s.withdraw, nil); err != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, err)
	}
	if r := acceptedReason(t, w, plan); !strings.HasSuffix(r, ", ignored keys: goal, mission.budget, tasks[0].goal") {
		t.Fatalf("the reason: %q", r)
	}
}

func acceptedReason(t *testing.T, w *world, task string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'accepted' ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	return reason
}

// noBaseWorld is M-3 on the platform and infra with no bases kept, as a
// mission proposed with repos and no bases, and a submitted twoRepoPlan.
func noBaseWorld(t *testing.T) (*world, string) {
	t.Helper()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform", infra}, Success: []string{store.GoalSuccess},
			MaxAttempts: 10, MaxWallHours: 24}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "active", "test")
	}))
	plan := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal", BaseCommit: commitA})
	w.work(t, plan, twoRepoPlan, "done")
	return w, plan
}

func TestPlanGateMissingBaseReadsMain(t *testing.T) {
	t.Parallel()
	w, plan := noBaseWorld(t)
	var mu sync.Mutex
	var read []string
	main := func(ctx context.Context, repo string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := ctx.Deadline(); !ok {
			return "", errors.New("no timeout")
		}
		read = append(read, repo)
		return commitB, nil
	}
	var c cards
	if did, err := store.AcceptPlan(ctx, w.rt, plan, c.ask, c.withdraw, main); err != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, err)
	}
	if !slices.Equal(read, []string{infra}) {
		t.Fatalf("main read for %v, want infra alone", read)
	}
	rows, err := w.rt.Query(ctx, `SELECT repo, base_commit FROM agentrt.task WHERE mission_id = 'M-3' AND role = 'implement' ORDER BY repo`)
	ok(t, err)
	var got []string
	for rows.Next() {
		var repo, base string
		ok(t, rows.Scan(&repo, &base))
		got = append(got, repo+"|"+base)
	}
	rows.Close()
	if want := []string{"bifrost-platform|" + commitA, infra + "|" + commitB}; !slices.Equal(got, want) {
		t.Fatalf("the implementations %v, want %v", got, want)
	}
}

func TestPlanGateMainUnreadWaits(t *testing.T) {
	t.Parallel()
	w, plan := noBaseWorld(t)
	round := func() int {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT round_no FROM agentrt.task WHERE task_id = $1`, plan).Scan(&n))
		return n
	}
	before := round()
	var c cards
	for _, main := range []store.ReadMain{
		func(context.Context, string) (string, error) { return "", errors.New("the forge is down") },
		func(context.Context, string) (string, error) { return " ", nil },
	} {
		if did, err := store.AcceptPlan(ctx, w.rt, plan, c.ask, c.withdraw, main); err != nil || did != "" {
			t.Fatalf("main unread: %q %v", did, err)
		}
		if st := w.state(t, "task", plan); st != "submitted" || round() != before {
			t.Fatalf("main unread: the plan is %s in round %d (was %d)", st, round(), before)
		}
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-3' AND role = 'implement'`).Scan(&n))
	if n != 0 {
		t.Fatalf("a waiting plan made %d tasks", n)
	}
	// With no reader the refusal stands.
	if did, err := store.AcceptPlan(ctx, w.rt, plan, c.ask, c.withdraw, nil); err != nil || did != "rework" ||
		!strings.Contains(lastReason(t, w, plan), "tasks[0]: the goal recorded no commit of bifrost-trade-infra to start from") {
		t.Fatalf("no reader: %q %v %q", did, err, lastReason(t, w, plan))
	}
}
