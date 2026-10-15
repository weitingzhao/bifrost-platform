package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The projection from real rows (SPEC-agentrt-progress-2026-10-14.md): each
// task status from what the runtime records, needs-you with its one count,
// recent results with the failed one kept, the plan's counts.
func TestProgress(t *testing.T) {
	w := newKernelWorld(t)
	tools := []store.Tool{codex}
	claimOne := func(host string, ttl time.Duration) (string, *store.Claim) {
		t.Helper()
		id := w.ready(t, store.Task{})
		c := w.claim(t, host, ttl, tools...)
		if c == nil || c.TaskID != id {
			t.Fatalf("claim on %s: %+v; want %s", host, c, id)
		}
		return id, c
	}
	working, _ := claimOne("h1", time.Hour)
	stuck, _ := claimOne("h2", time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	lost, _ := claimOne("h3", time.Hour)
	slow, slowClaim := claimOne("h4", time.Hour)
	queued := w.ready(t, store.Task{})
	blocked := w.ready(t, store.Task{})
	review := w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if merr := store.MoveTaskForTest(ctx, tx, "controller", blocked, "blocked"); merr != nil {
			return merr
		}
		for _, to := range []string{"leased", "running", "submitted"} {
			if merr := store.MoveTaskForTest(ctx, tx, "controller", review, to); merr != nil {
				return merr
			}
		}
		return nil
	}))
	// Three accepted implementations of 100 seconds make the usual time;
	// one rejected with a failed attempt is a result to keep.
	var seed []string
	for i := range 3 {
		task, attempt := store.NewTaskID(), store.NewAttemptID()
		seed = append(seed, fmt.Sprintf(`INSERT INTO agentrt.task (task_id, mission_id, plan_node_id, role, task_class, title, spec_artifact_id,
		  effect_class, state, hard_limit_s, created_by_actor_id) VALUES ('%s', 'M-1', '%s', 'implement', 'go', 'done %d', '%s', 'branch', 'accepted', 3600, 'controller')`,
			task, s1, i, w.spec),
			fmt.Sprintf(`INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch, lease_expires_at,
		  heartbeat_at, state, started_at, computed_at, ended_at, conclusion) VALUES ('%s', '%s', 1, 'exec:codex', %d, now(), now(), 'finished',
		  now() - interval '100 seconds', now(), now(), 'done')`, attempt, task, 900+i))
	}
	failedTask, failedAttempt := store.NewTaskID(), store.NewAttemptID()
	seed = append(seed, fmt.Sprintf(`INSERT INTO agentrt.task (task_id, mission_id, plan_node_id, role, task_class, title, spec_artifact_id,
	  effect_class, state, hard_limit_s, created_by_actor_id) VALUES ('%s', 'M-1', '%s', 'implement', 'go', 'an experiment', '%s', 'branch', 'rejected', 3600, 'controller')`,
		failedTask, s1, w.spec),
		fmt.Sprintf(`INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch, lease_expires_at,
	  heartbeat_at, state, started_at, computed_at, ended_at, conclusion) VALUES ('%s', '%s', 1, 'exec:codex', 999, now(), now(), 'finished',
	  now() - interval '50 seconds', now(), now(), 'failed')`, failedAttempt, failedTask),
		`UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '10 minutes' WHERE host_id = 'h3'`,
		fmt.Sprintf(`UPDATE agentrt.attempt SET started_at = clock_timestamp() - interval '200 seconds' WHERE attempt_id = '%s'`, slowClaim.AttemptID))
	w.legacy(t, seed...)
	// Waiting for the Owner: a proposed mission with a task, a plan change
	// on a card.
	idle, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-2", []store.TaskProposal{offPlan(proposal("e", w.spec))})
	ok(t, err)
	waiting := taskIDs(idle)["e"]
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "c", root, "a new item", []store.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "x"}},
		func(context.Context, store.Card) (string, error) { return "q_7", nil })
	ok(t, err)
	w.activate(t, s1)

	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc struct {
		NeedsYou struct {
			Count int `json:"count"`
			Items []struct {
				Kind   string         `json:"kind"`
				Ref    string         `json:"ref"`
				Action map[string]any `json:"action"`
			} `json:"items"`
		} `json:"needs_you"`
		Work []struct {
			MissionID string         `json:"mission_id"`
			Status    string         `json:"status"`
			Slices    map[string]int `json:"slices"`
			Tasks     []struct {
				TaskID  string `json:"task_id"`
				Status  string `json:"status"`
				UsualS  *int64 `json:"usual_s"`
				Attempt *struct {
					HostID string `json:"host_id"`
				} `json:"attempt"`
			} `json:"tasks"`
		} `json:"work"`
		Live   []map[string]any `json:"live"`
		Recent []map[string]any `json:"recent"`
		Plan   []struct {
			PlanNodeID string         `json:"plan_node_id"`
			Count      map[string]any `json:"count"`
			Items      []struct {
				PlanNodeID  string         `json:"plan_node_id"`
				Count       map[string]any `json:"count"`
				ActiveSteps []string       `json:"active_steps"`
			} `json:"items"`
		} `json:"plan"`
		Hosts []map[string]any `json:"hosts"`
	}
	ok(t, json.Unmarshal(raw, &doc))

	status := map[string]string{}
	var m1, m2 string
	for _, m := range doc.Work {
		for _, task := range m.Tasks {
			status[task.TaskID] = task.Status
			if task.TaskID == working && (task.UsualS == nil || *task.UsualS != 100 || task.Attempt == nil || task.Attempt.HostID != "h1") {
				t.Errorf("the working task: %+v", task)
			}
		}
		switch m.MissionID {
		case "M-1":
			m1 = m.Status
			if m.Slices["done"] != 3 || m.Slices["total"] != 10 {
				t.Errorf("M-1 slices %v; want 3 of 10 (three accepted, seven open, the rejected one out)", m.Slices)
			}
		case "M-2":
			m2 = m.Status
		}
	}
	for task, want := range map[string]string{working: store.StatusWorking, stuck: store.StatusMaybeStuck, lost: store.StatusHostLost,
		slow: store.StatusSlow, queued: store.StatusQueued, blocked: store.StatusBlocked, review: store.StatusWaitingRole,
		waiting: store.StatusWaitingOwner} {
		if status[task] != want {
			t.Errorf("task %s is %q; want %q", task, status[task], want)
		}
	}
	if m1 != store.StatusHostLost || m2 != store.StatusWaitingOwner {
		t.Errorf("missions: M-1 %q, M-2 %q", m1, m2)
	}
	if len(doc.Live) != 4 {
		t.Errorf("%d live attempts; want 4", len(doc.Live))
	}
	kinds := map[string]string{}
	for _, it := range doc.NeedsYou.Items {
		kinds[it.Kind+":"+it.Ref] = fmt.Sprint(it.Action["where"])
	}
	if doc.NeedsYou.Count != 1 || kinds["mission:M-2"] != "none" || kinds["task_blocked:"+blocked] != "none" {
		t.Errorf("needs you: %d %v; want 1 to act on (the plan change), the mission and the blocked task listed without an action",
			doc.NeedsYou.Count, kinds)
	}
	var failedKept bool
	for _, r := range doc.Recent {
		if r["task_id"] == failedTask && r["conclusion"] == "failed" && r["state"] == "rejected" {
			failedKept = true
		}
	}
	if !failedKept || len(doc.Recent) != 4 {
		t.Errorf("recent %v; want the three accepted and the failed experiment", doc.Recent)
	}
	if len(doc.Plan) != 1 || doc.Plan[0].PlanNodeID != root || len(doc.Plan[0].Items) != 1 ||
		fmt.Sprint(doc.Plan[0].Items[0].ActiveSteps) != "["+s1+"]" || doc.Plan[0].Items[0].Count["state"] != "active" ||
		doc.Plan[0].Items[0].Count["total"] != float64(3) {
		t.Errorf("plan %+v", doc.Plan)
	}
	if len(doc.Hosts) != 8 {
		t.Errorf("%d hosts", len(doc.Hosts))
	}
}

// Task statuses decided in order (SPEC §3.1).
func TestTaskStatusOrder(t *testing.T) {
	if got := store.TaskStatus("ready", "active", nil, nil); got != store.StatusQueued {
		t.Errorf("ready: %s", got)
	}
	for state, want := range map[string]string{"submitted": store.StatusWaitingRole, "verifying": store.StatusWaitingRole,
		"rework": store.StatusWaitingRole, "blocked": store.StatusBlocked, "paused": store.StatusBlocked,
		"awaiting_owner": store.StatusWaitingOwner, "proposed": store.StatusQueued} {
		if got := store.TaskStatus(state, "active", nil, nil); got != want {
			t.Errorf("%s: %s; want %s", state, got, want)
		}
	}
	if got := store.TaskStatus("ready", "proposed", nil, nil); got != store.StatusWaitingOwner {
		t.Errorf("a task of a proposed mission: %s", got)
	}
}
