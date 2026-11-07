package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The plan view says what each task of a step waits on, with its role and
// round: its checks, the Owner's card, a stop, its dependencies, its landing;
// a task accepted and landed waits on nothing.
func TestPlanViewTaskWaits(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	submitted := w.ready(t, store.Task{})
	blocked := w.ready(t, store.Task{})
	stopped := w.ready(t, store.Task{})
	landing := w.ready(t, store.Task{})
	landed := w.ready(t, store.Task{})
	holder := w.ready(t, store.Task{})
	queued := w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, to := range []string{"leased", "running", "submitted"} {
			if err := store.MoveTaskForTest(ctx, tx, "controller", submitted, to); err != nil {
				return err
			}
		}
		return store.FailWorkForTest(ctx, tx, blocked)
	}))
	check, err := w.task(t, "M-1", s1, submitted, "verify")
	ok(t, err)
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	question := a.lastID()
	control := store.NewControlID()
	w.stop(t, control, "task", stopped, "wrong branch")
	waits, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	waitsTwo, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.task_dependency (task_id, depends_on_task_id) VALUES ('%s', '%s'), ('%s', '%s'), ('%s', '%s')`,
		waits, submitted, waitsTwo, submitted, waitsTwo, stopped),
		fmt.Sprintf(`UPDATE agentrt.task SET state = 'accepted' WHERE task_id IN ('%s', '%s', '%s')`, landing, landed, holder))

	// land:1 landed the one task; land:2 for the other is claimed, still
	// active, while land:3 holds the slot at main_intent.
	attempt := store.NewAttemptID()
	put := func(r map[string]any) { t.Helper(); ok(t, appendLanding(w, landingRepoStream, r)) }
	ref := "refs/heads/land/prod/" + landed + "/1"
	put(landingRecord("enable", "enabled", "repo", "bifrost-platform", "rule_version", 1))
	put(landingRecord("land:1", "claimed", "task", landed, "attempt", attempt, "A", commitA, "rule_version", 1))
	put(landingRecord("land:1", "prepared", "R", commitB, "M0", commitA, "committer_date", time.Now().UTC().Format(time.RFC3339), "dry_run", false))
	put(landingRecord("land:1", "branch_intent", "ref", ref, "R", commitB))
	put(landingRecord("land:1", "branch_pushed", "ref", ref, "R", commitB))
	put(landingRecord("land:1", "ci_seen", "run", "ci-1", "status", "Succeeded", "R", commitB,
		"created_at", time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)))
	put(landingRecord("land:1", "main_intent", "R", commitB, "M0", commitA))
	put(landingRecord("land:1", "main_result", "outcome", "ok"))
	put(landingRecord("land:1", "landed", "R", commitB))
	put(landingRecord("land:2", "claimed", "task", landing, "attempt", attempt, "A", commitA, "rule_version", 1))
	ref3 := "refs/heads/land/prod/" + holder + "/1"
	put(landingRecord("land:3", "claimed", "task", holder, "attempt", attempt, "A", commitA, "rule_version", 1))
	put(landingRecord("land:3", "prepared", "R", commitB, "M0", commitA, "committer_date", time.Now().UTC().Format(time.RFC3339), "dry_run", false))
	put(landingRecord("land:3", "branch_intent", "ref", ref3, "R", commitB))
	put(landingRecord("land:3", "branch_pushed", "ref", ref3, "R", commitB))
	put(landingRecord("land:3", "ci_seen", "run", "ci-3", "status", "Succeeded", "R", commitB,
		"created_at", time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)))
	put(landingRecord("land:3", "main_intent", "R", commitB, "M0", commitA))

	before := w.footprint(t)
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	if before != w.footprint(t) {
		t.Fatal("read wrote state")
	}
	tasks := map[string]map[string]any{}
	for _, task := range view["tasks"].([]map[string]any) {
		tasks[task["task_id"].(string)] = task
		if _, shown := task["depends_on"]; shown {
			t.Fatalf("task %s shows depends_on", task["task_id"])
		}
	}
	for id, want := range map[string]struct{ on, reason string }{
		submitted: {"checks", "waits for its verification or review"},
		blocked:   {"owner", "the task waits for the Owner's answer on question " + question + ": "},
		stopped:   {"stop", "stopped by owner: wrong branch (control " + control + "); only the Owner lifts it"},
		waits:     {"dependencies", "waits for 1 dependencies to be accepted"},
		waitsTwo:  {"dependencies", "waits for 2 dependencies to be accepted"},
		landing:   {"landing", "landing land:2 is in phase claimed"},
		holder:    {"landing", "landing land:3 is in phase main_intent"},
		queued:    {"queued", ""},
	} {
		task := tasks[id]
		if task == nil {
			t.Fatalf("task %s not in the view", id)
		}
		wait, _ := task["waiting"].(map[string]any)
		if wait == nil || wait["on"] != want.on || !strings.HasPrefix(wait["reason"].(string), want.reason) {
			t.Fatalf("task %s waits %v; want on %s, reason %q", id, wait, want.on, want.reason)
		}
		if since, _ := wait["since"].(string); since == "" {
			t.Fatalf("task %s waits since nothing: %v", id, wait)
		}
		var role string
		var round, rounds int
		ok(t, w.rt.QueryRow(ctx, `SELECT role, round_no, max_rounds FROM agentrt.task WHERE task_id = $1`, id).Scan(&role, &round, &rounds))
		if task["role"] != role || task["round_no"] != float64(round) || task["max_rounds"] != float64(rounds) {
			t.Fatalf("task %s reads role %v round %v of %v; want %s %d of %d", id, task["role"], task["round_no"], task["max_rounds"], role, round, rounds)
		}
	}
	checks, _ := tasks[submitted]["waiting"].(map[string]any)["checks"].([]map[string]any)
	if len(checks) != 1 || checks[0]["task_id"] != check || checks[0]["role"] != "verify" || checks[0]["state"] != "proposed" {
		t.Fatalf("checks %v; want %s, verify, proposed", tasks[submitted]["waiting"], check)
	}
	if q := tasks[blocked]["waiting"].(map[string]any)["question_id"]; q != question {
		t.Fatalf("question %v; want %s", q, question)
	}
	if wait, shown := tasks[landed]["waiting"]; shown {
		t.Fatalf("a landed task waits: %v", wait)
	}

	// Each wait names what it waits on: the dependencies by title and state,
	// the operation holding the landing slot, the executor a queued task
	// requires when the store names one.
	title := func(id string) (string, string) {
		t.Helper()
		var title, state string
		ok(t, w.rt.QueryRow(ctx, `SELECT title, state FROM agentrt.task WHERE task_id = $1`, id).Scan(&title, &state))
		return title, state
	}
	deps, _ := tasks[waitsTwo]["waiting"].(map[string]any)["depends_on"].([]map[string]any)
	want := []string{submitted, stopped}
	if want[0] > want[1] {
		want[0], want[1] = want[1], want[0]
	}
	if len(deps) != 2 {
		t.Fatalf("depends_on %v; want %v", deps, want)
	}
	for i, d := range deps {
		name, state := title(want[i])
		if d["task_id"] != want[i] || d["title"] != name || d["state"] != state || name == "" {
			t.Fatalf("dependency %d is %v; want %s %q %s", i, d, want[i], name, state)
		}
	}
	if one, _ := tasks[waits]["waiting"].(map[string]any)["depends_on"].([]map[string]any); len(one) != 1 || one[0]["task_id"] != submitted {
		t.Fatalf("depends_on %v; want %s", one, submitted)
	}
	slot, _ := tasks[landing]["waiting"].(map[string]any)["slot_holder"].(map[string]any)
	name, _ := title(holder)
	if slot == nil || slot["operation"] != "land:3" || slot["task_id"] != holder || slot["title"] != name || slot["phase"] != "main_intent" || slot["since"] == "" {
		t.Fatalf("slot holder %v; want land:3 of %s %q at main_intent", slot, holder, name)
	}
	if slot, shown := tasks[holder]["waiting"].(map[string]any)["slot_holder"]; shown {
		t.Fatalf("the slot's holder waits on %v; want its own landing, held by nobody", slot)
	}
	for _, id := range []string{submitted, blocked, stopped, waits, waitsTwo} {
		for _, key := range []string{"slot_holder", "executor"} {
			if v, shown := tasks[id]["waiting"].(map[string]any)[key]; shown {
				t.Fatalf("task %s waits on %s %v", id, key, v)
			}
		}
	}
	var executors []string
	ok(t, w.rt.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT COALESCE(t.executor_pin, j.executor_actor_id)
	  FROM agentrt.task t LEFT JOIN LATERAL jsonb_array_elements_text(t.route_json) r(id) ON true
	  LEFT JOIN agentrt.job_candidate j ON j.job_candidate_id = r.id
	  WHERE t.task_id = $1 AND COALESCE(t.executor_pin, j.executor_actor_id) IS NOT NULL)`, queued).Scan(&executors))
	wait := tasks[queued]["waiting"].(map[string]any)
	executor, _ := wait["executor"].(map[string]any)
	switch {
	case len(executors) == 1 && (executor == nil || executor["executor"] != executors[0]):
		t.Fatalf("queued task waits %v; want executor %s", wait, executors[0])
	case len(executors) != 1 && (executor != nil || !strings.HasSuffix(wait["reason"].(string), "; the store names no executor or pool it requires")):
		t.Fatalf("queued task waits %v; want no executor among %v, and the reason saying so", wait, executors)
	}
}
