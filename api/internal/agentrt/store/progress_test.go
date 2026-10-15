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

// ended seeds a finished task of M-1 on s1 with its attempts, as the runtime
// leaves them: each attempt's rows, its computed and submitted events, a
// verification accepting the last attempt when the task is accepted, and the
// event that ended the task. outcomes are the attempts' conclusions, the
// last one deciding the task (done: accepted, failed: rejected).
func (w *world) ended(t *testing.T, outcomes ...string) (string, []string) {
	t.Helper()
	final := "accepted"
	if outcomes[len(outcomes)-1] == "failed" {
		final = "rejected"
	}
	return w.endedAs(t, final, outcomes...)
}

// endedAs is ended with the task's end given: a task rejected after an
// attempt that ended done (its verification failed) has no attempt
// accepted.
func (w *world) endedAs(t *testing.T, final string, outcomes ...string) (string, []string) {
	t.Helper()
	task := store.NewTaskID()
	var attempts []string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: an ended task"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, plan_node_id, role, task_class, title, spec_artifact_id,
		  effect_class, state, hard_limit_s, created_by_actor_id) VALUES ($1, 'M-1', $2, 'implement', 'go', 'ended', $3, 'branch', $4, 3600, 'controller')`,
			task, s1, w.spec, final); err != nil {
			return err
		}
		legacy := func() error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
				Body: map[string]any{"source": "test: an ended task"}})
			return err
		}
		for i, outcome := range outcomes {
			a := store.NewAttemptID()
			attempts = append(attempts, a)
			if err := legacy(); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
			  lease_expires_at, heartbeat_at, state, started_at, computed_at, persisted_at, ended_at, conclusion)
			  VALUES ($1, $2, $3, 'exec:codex', nextval('agentrt.lease_epoch_seq'), now(), now(), 'finished',
			    clock_timestamp() - interval '100 seconds', clock_timestamp(), clock_timestamp(), clock_timestamp(), $4)`, a, task, i+1, outcome); err != nil {
				return err
			}
			for _, ev := range []store.Event{
				{Type: "attempt_computed", Body: map[string]any{"end_how": "exit", "conclusion": outcome}},
				{Type: "attempt_submitted", Body: map[string]any{}},
			} {
				ev.Stream, ev.Actor, ev.MissionID, ev.TaskID, ev.AttemptID = "mission:M-1", "controller", "M-1", task, a
				if _, err := store.Append(ctx, tx, ev); err != nil {
					return err
				}
			}
		}
		if final == "accepted" {
			if err := legacy(); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
			  evidence_artifact_id, accepts_attempt) VALUES ($1, $2, $3, 'deterministic', 'pass', $4, true)`,
				store.NewVerificationID(), task, attempts[len(attempts)-1], w.spec); err != nil {
				return err
			}
		}
		_, err := store.Append(ctx, tx, store.Event{Stream: "mission:M-1", Type: "task_state_changed", Actor: "controller", MissionID: "M-1",
			TaskID: task, Body: map[string]any{"from": "verifying", "to": final}})
		return err
	}))
	return task, attempts
}

type progressDoc struct {
	AsOf     int64 `json:"as_of_event_id"`
	NeedsYou struct {
		Count     int              `json:"count"`
		Items     []map[string]any `json:"items"`
		Truncated bool             `json:"truncated"`
	} `json:"needs_you"`
	Work struct {
		Missions []struct {
			MissionID    string         `json:"mission_id"`
			Status       string         `json:"status"`
			Reason       string         `json:"reason"`
			Since        string         `json:"since"`
			WaitingOn    string         `json:"waiting_on"`
			WaitingRoles []string       `json:"waiting_roles"`
			Slices       map[string]any `json:"slices"`
			NextSteps    []any          `json:"next_steps"`
			ETA          map[string]any `json:"eta"`
			Tasks        []struct {
				TaskID       string   `json:"task_id"`
				Status       string   `json:"status"`
				Reason       string   `json:"reason"`
				Since        string   `json:"since"`
				WaitingRoles []string `json:"waiting_roles"`
				UsualS       *int64   `json:"usual_s"`
				DependsOn    []string `json:"depends_on"`
				OffPlan      bool     `json:"off_plan"`
			} `json:"tasks"`
		} `json:"missions"`
		Truncated bool `json:"truncated"`
	} `json:"work"`
	Live   []map[string]any `json:"live"`
	Recent struct {
		Tasks          []map[string]any `json:"tasks"`
		FailedAttempts []map[string]any `json:"failed_attempts"`
	} `json:"recent"`
	Plan []struct {
		PlanNodeID string           `json:"plan_node_id"`
		Kind       string           `json:"kind"`
		Count      map[string]any   `json:"count"`
		Rows       []map[string]any `json:"rows"`
		Items      []struct {
			PlanNodeID  string         `json:"plan_node_id"`
			Count       map[string]any `json:"count"`
			ActiveSteps []string       `json:"active_steps"`
		} `json:"items"`
	} `json:"plan"`
	Hosts []map[string]any `json:"hosts"`
}

func (w *world) progress(t *testing.T) progressDoc {
	t.Helper()
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc progressDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc
}

// The projection from real rows (SPEC-agentrt-progress-2026-10-14.md, second
// version).
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
	// Three accepted implementations make the usual time (100 s); one task
	// failed once and then was accepted: only the accepted attempt counts,
	// and the failed one stays in the recent failures (T06); one rejected.
	var accepted []string
	for range 3 {
		task, _ := w.ended(t, "done")
		accepted = append(accepted, task)
	}
	retried, retriedAttempts := w.ended(t, "failed", "done")
	rejected, rejectedAttempts := w.ended(t, "failed")
	working, _ := claimOne("h1", time.Hour)
	stuck, _ := claimOne("h2", time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	lost, _ := claimOne("h3", time.Hour)
	slow, slowClaim := claimOne("h4", time.Hour)
	queued := w.ready(t, store.Task{})
	blocked := w.ready(t, store.Task{})
	// Waiting for a role: a submitted task with its verification proposed,
	// one with none yet, and one in rework (its own role's next round).
	review := w.ready(t, store.Task{})
	unchecked := w.ready(t, store.Task{})
	reworked := w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if merr := store.MoveTask(ctx, tx, "controller", blocked, "blocked", "the executor cannot reach Gitea"); merr != nil {
			return merr
		}
		for task, moves := range map[string][]string{review: {"leased", "running", "submitted"}, unchecked: {"leased", "running", "submitted"},
			reworked: {"leased", "running", "submitted", "verifying", "rework"}} {
			for _, to := range moves {
				if merr := store.MoveTaskForTest(ctx, tx, "controller", task, to); merr != nil {
					return merr
				}
			}
		}
		return nil
	}))
	checker, err := w.task(t, "M-1", s1, review, "verify")
	ok(t, err)
	w.legacy(t,
		`UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '10 minutes' WHERE host_id = 'h3'`,
		fmt.Sprintf(`UPDATE agentrt.attempt SET started_at = clock_timestamp() - interval '200 seconds' WHERE attempt_id = '%s'`, slowClaim.AttemptID))
	var blockedSince string
	ok(t, w.rt.QueryRow(ctx, `SELECT to_char(max(at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS') FROM agentrt.event
	  WHERE task_id = $1 AND event_type = 'task_state_changed'`, blocked).Scan(&blockedSince))
	// A plan change later moves updated_at of the blocked task and of an
	// accepted one: neither when it started waiting nor when it ended moves.
	time.Sleep(1100 * time.Millisecond)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, aerr := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "test: touch"}}); aerr != nil {
			return aerr
		}
		_, uerr := tx.Exec(ctx, `UPDATE agentrt.task SET updated_at = clock_timestamp() WHERE task_id IN ($1, $2)`, blocked, accepted[0])
		return uerr
	}))
	// Waiting for the Owner: a proposed mission with a task (no action of his
	// yet: not in needs-you); a plan change on a card (in needs-you).
	idle, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-2", []store.TaskProposal{offPlan(proposal("e", w.spec))})
	ok(t, err)
	waiting := taskIDs(idle)["e"]
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "c", root, "rename the item", []store.PlanChange{{Op: "retitle", Node: item, Title: "x"}},
		func(context.Context, store.Card) (string, error) { return "q_7", nil })
	ok(t, err)
	w.activate(t, s1)
	// A step of M-2 under the same item: a slice of M-2's, never of M-1's.
	// An architecture root with a row.
	for _, n := range []store.PlanNode{
		{ID: "R2:2.1-4", Parent: item, Kind: "step", Label: "2.1-4", Title: "s4", MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root},
		{ID: "ARCH", Kind: "arch_root", Label: "A", Title: "architecture", Root: "ARCH"},
		{ID: "ARCH:state-db", Parent: "ARCH", Kind: "arch_row", Label: "state-db", Title: "state in PG", Root: "ARCH"},
	} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, cerr := store.CreatePlanNode(ctx, tx, "controller", n); return cerr }))
	}

	doc := w.progress(t)
	if doc.AsOf == 0 {
		t.Error("no event cursor")
	}
	status := map[string]string{}
	reasons := map[string]string{}
	roles := map[string]string{}
	missions := map[string]int{}
	for i, m := range doc.Work.Missions {
		missions[m.MissionID] = i
		for _, task := range m.Tasks {
			status[task.TaskID], reasons[task.TaskID], roles[task.TaskID] = task.Status, task.Reason, fmt.Sprint(task.WaitingRoles)
			if task.TaskID == working && (task.UsualS == nil || *task.UsualS < 99 || *task.UsualS > 101) {
				t.Errorf("the working task's usual time: %v; want 100 s from the three accepted attempts and the retried one's last", task.UsualS)
			}
			if task.TaskID == blocked && (len(task.Since) < 19 || task.Since[:19] != blockedSince) {
				t.Errorf("the blocked task waits since %s; want %s, when it was blocked", task.Since, blockedSince)
			}
		}
	}
	for task, want := range map[string]string{working: store.StatusWorking, stuck: store.StatusMaybeStuck, lost: store.StatusHostLost,
		slow: store.StatusSlow, queued: store.StatusQueued, blocked: store.StatusBlocked, review: store.StatusWaitingRole,
		unchecked: store.StatusWaitingRole, reworked: store.StatusWaitingRole, checker: store.StatusQueued,
		waiting: store.StatusWaitingOwner} {
		if status[task] != want {
			t.Errorf("task %s is %q; want %q", task, status[task], want)
		}
	}
	if reasons[blocked] != "the task is blocked: the executor cannot reach Gitea" {
		t.Errorf("the blocked task's reason: %q", reasons[blocked])
	}
	// Which role each waits for: the verification's, nobody's yet (the
	// reason says so), its own for the next round.
	if roles[review] != "[verify]" || roles[unchecked] != "[]" || roles[reworked] != "[implement]" ||
		reasons[unchecked] != "waits for its verification or review; none is proposed yet" {
		t.Errorf("waiting roles: review %s, unchecked %s (%q), reworked %s", roles[review], roles[unchecked], reasons[unchecked], roles[reworked])
	}
	m1 := doc.Work.Missions[missions["M-1"]]
	m2 := doc.Work.Missions[missions["M-2"]]
	if m1.Status != store.StatusHostLost || m1.WaitingOn != "coordinator" || m2.Status != store.StatusWaitingOwner || m2.WaitingOn != "owner" {
		t.Errorf("missions: M-1 %s/%s, M-2 %s/%s", m1.Status, m1.WaitingOn, m2.Status, m2.WaitingOn)
	}
	// Slices are M-1's steps (s1 active, s3 planned), not its tasks, nor the
	// other steps under the item (s2 of no mission, s4 of M-2).
	if m1.Slices["total"] != float64(2) || m1.Slices["done"] != float64(0) || m1.Slices["state"] != "active" || len(m1.NextSteps) != 1 {
		t.Errorf("M-1 slices %v, next %v", m1.Slices, m1.NextSteps)
	}
	if m2.Slices["total"] != float64(1) {
		t.Errorf("M-2 slices %v; want its one step", m2.Slices)
	}
	if m1.ETA["state"] != "unknown" || m1.ETA["reason"] == "" {
		t.Errorf("eta %v; want unknown with its reason", m1.ETA)
	}
	if doc.NeedsYou.Count != 1 || len(doc.NeedsYou.Items) != 1 || doc.NeedsYou.Items[0]["id"] != "question:q_7" {
		t.Errorf("needs you: %d %v; want the plan change on its card only", doc.NeedsYou.Count, doc.NeedsYou.Items)
	} else {
		item := doc.NeedsYou.Items[0]
		// The missions of the steps under the item it renames, and the ten
		// unfinished tasks on them (all on s1).
		if blocks, _ := item["blocks"].(map[string]any); fmt.Sprint(blocks["missions"]) != "[M-1 M-2]" || blocks["unfinished_tasks"] != float64(10) {
			t.Errorf("the plan change holds up %v; want M-1 and M-2 and the 10 unfinished tasks on the steps under the item", item["blocks"])
		}
		if v, has := item["can_act_now"]; !has || v != nil || item["can_act_now_reason"] == "" {
			t.Errorf("can_act_now %v (%v); want null with its reason: the card's state is the question store's", v, item["can_act_now_reason"])
		}
	}
	if len(doc.Live) != 4 {
		t.Errorf("%d live attempts; want 4", len(doc.Live))
	}
	failed := map[any]bool{}
	for _, a := range doc.Recent.FailedAttempts {
		failed[a["attempt_id"]] = true
	}
	if !failed[retriedAttempts[0]] || !failed[rejectedAttempts[0]] || len(failed) != 2 {
		t.Errorf("failed attempts %v; want the retried task's first and the rejected one's", doc.Recent.FailedAttempts)
	}
	endedAt := map[any]string{}
	for _, r := range doc.Recent.Tasks {
		endedAt[r["task_id"]] = fmt.Sprint(r["ended_at"])
	}
	if len(doc.Recent.Tasks) != 5 || endedAt[retried] == "" || endedAt[rejected] == "" {
		t.Errorf("recent tasks %v", doc.Recent.Tasks)
	}
	if len(doc.Recent.Tasks) > 0 && doc.Recent.Tasks[0]["task_id"] == accepted[0] {
		t.Error("a plan change brought an old ended task to the top of recent")
	}
	var round, arch bool
	for _, r := range doc.Plan {
		switch r.Kind {
		case "round":
			round = len(r.Items) == 1 && fmt.Sprint(r.Items[0].ActiveSteps) == "["+s1+"]" && r.Items[0].Count["total"] == float64(4)
		case "arch_root":
			arch = len(r.Rows) == 1 && r.Rows[0]["tick"] == "unknown" && r.Rows[0]["reason"] != ""
		}
	}
	if !round || !arch {
		t.Errorf("plan %+v", doc.Plan)
	}
	for _, h := range doc.Hosts {
		if _, has := h["live_attempts"]; !has {
			t.Errorf("host without live_attempts: %v", h)
		}
		if _, has := h["controls"]; !has {
			t.Errorf("host without controls: %v", h)
		}
	}
}

// A mission without tasks: proposed waits for the Owner (with what for);
// active with steps not done is idle (the coordinator's turn).
func TestProgressMissionWithoutTasks(t *testing.T) {
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	var lastM1, proposedM2 time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT max(at) FILTER (WHERE mission_id = 'M-1'),
	    max(at) FILTER (WHERE mission_id = 'M-2' AND event_type = 'mission_proposed') FROM agentrt.event`).Scan(&lastM1, &proposedM2))
	doc := w.progress(t)
	got := map[string]string{}
	since := map[string]string{}
	for _, m := range doc.Work.Missions {
		got[m.MissionID], since[m.MissionID] = m.Status+"/"+m.WaitingOn, m.Since
	}
	if got["M-1"] != "idle/coordinator" || got["M-2"] != "waiting_owner/owner" {
		t.Fatalf("missions %v", got)
	}
	// Idle since its last event; waiting since it was proposed.
	if !sameMoment(t, since["M-1"], lastM1) || !sameMoment(t, since["M-2"], proposedM2) {
		t.Fatalf("since %v; want M-1 %s, M-2 %s", since, lastM1, proposedM2)
	}
}

// Task statuses where two hold at once (SPEC §3.1): the first in the order
// wins.
func TestTaskStatusOrder(t *testing.T) {
	now := time.Now()
	fresh, old := now.Add(-time.Second), now.Add(-10*time.Minute)
	long, short, atBound, pastBound := int64(400), int64(10), int64(150), int64(151)
	usual := 100 * time.Second
	live := func(state string, known bool, polled *time.Time, elapsed *int64) *store.ProgressAttempt {
		return store.LiveForTest("a_1", state, known, polled, elapsed, now)
	}
	for name, c := range map[string]struct {
		in   store.StatusInput
		want string
	}{
		"host lost over stale":          {store.StatusInput{TaskState: "running", Attempt: live("stale", true, &old, &short)}, store.StatusHostLost},
		"host never polled":             {store.StatusInput{TaskState: "running", Attempt: live("running", true, nil, &short)}, store.StatusHostLost},
		"unknown host over all":         {store.StatusInput{TaskState: "running", Attempt: live("running", false, nil, &short)}, store.StatusUnknown},
		"unknown host over stale":       {store.StatusInput{TaskState: "running", Attempt: live("stale", false, nil, &short)}, store.StatusUnknown},
		"stale over a waiting mission":  {store.StatusInput{TaskState: "running", MissionState: "proposed", Attempt: live("stale", true, &fresh, &short)}, store.StatusMaybeStuck},
		"waiting owner":                 {store.StatusInput{TaskState: "awaiting_owner"}, store.StatusWaitingOwner},
		"waiting owner over blocked":    {store.StatusInput{TaskState: "blocked", MissionState: "awaiting_owner"}, store.StatusWaitingOwner},
		"a proposed mission's task":     {store.StatusInput{TaskState: "ready", MissionState: "proposed"}, store.StatusWaitingOwner},
		"blocked over working":          {store.StatusInput{TaskState: "paused", Attempt: live("running", true, &fresh, &short)}, store.StatusBlocked},
		"a paused mission's task":       {store.StatusInput{TaskState: "ready", MissionState: "paused"}, store.StatusBlocked},
		"working in a paused mission":   {store.StatusInput{TaskState: "running", MissionState: "paused", Attempt: live("running", true, &fresh, &short)}, store.StatusWorking},
		"slow past 1.5 times":           {store.StatusInput{TaskState: "running", Attempt: live("running", true, &fresh, &long), Usual: &usual}, store.StatusSlow},
		"not slow at 1.5 times":         {store.StatusInput{TaskState: "running", Attempt: live("running", true, &fresh, &atBound), Usual: &usual}, store.StatusWorking},
		"slow just past 1.5 times":      {store.StatusInput{TaskState: "running", Attempt: live("running", true, &fresh, &pastBound), Usual: &usual}, store.StatusSlow},
		"not slow without a usual time": {store.StatusInput{TaskState: "running", Attempt: live("running", true, &fresh, &long)}, store.StatusWorking},
		"working":                       {store.StatusInput{TaskState: "running", Attempt: live("running", true, &fresh, &short), Usual: &usual}, store.StatusWorking},
		"submitted":                     {store.StatusInput{TaskState: "submitted"}, store.StatusWaitingRole},
		"rework":                        {store.StatusInput{TaskState: "rework"}, store.StatusWaitingRole},
		"ready":                         {store.StatusInput{TaskState: "ready"}, store.StatusQueued},
		"proposed with dependencies":    {store.StatusInput{TaskState: "proposed", WaitingDeps: 2}, store.StatusQueued},
	} {
		c.in.Now = now
		if got, reason := store.TaskStatus(c.in); got != c.want || reason == "" {
			t.Errorf("%s: %s (%q); want %s", name, got, reason, c.want)
		}
	}
	at := 15 * time.Second
	if _, reason := store.TaskStatus(store.StatusInput{TaskState: "running", Attempt: live("running", true, &fresh, &long), Usual: &at, Now: now}); reason != "running 400 s, usually 15 s" {
		t.Errorf("a slow task's reason: %q", reason)
	}
	if _, reason := store.TaskStatus(store.StatusInput{TaskState: "ready", MissionState: "paused", MissionReason: "budget spent", Now: now}); reason != "the mission is paused: budget spent" {
		t.Errorf("a paused mission's task's reason: %q", reason)
	}
	// A mission shows the first of every two neighbours in the order (§3.2).
	order := []string{store.StatusHostLost, store.StatusMaybeStuck, store.StatusUnknown, store.StatusWaitingOwner, store.StatusBlocked,
		store.StatusSlow, store.StatusWorking, store.StatusWaitingRole, store.StatusQueued}
	for i := 1; i < len(order); i++ {
		if got := store.WorstForTest(order[i], order[i-1]); got != order[i-1] {
			t.Errorf("%s and %s show %s; want %s", order[i], order[i-1], got, order[i-1])
		}
	}
}

// Live attempts in start order by time, not by the text of the time (a
// fraction of a second sorts after the whole second).
func TestLiveOrder(t *testing.T) {
	base := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	// The ids sort the other way round from the times; as text,
	// "…00.1Z" sorts before "…00Z".
	got := store.SortLiveForTest([]*store.ProgressAttempt{
		store.LiveForTest("a_a_late", "running", true, nil, nil, base.Add(100*time.Millisecond)),
		store.LiveForTest("a_b_first", "running", true, nil, nil, base),
		store.LiveForTest("a_0_none", "leased", true, nil, nil, time.Time{}),
	})
	if fmt.Sprint(got) != "[a_b_first a_a_late a_0_none]" {
		t.Fatalf("order %v", got)
	}
}

// Review 181 B2: the usual time is read from accepted attempts only: done
// attempts of tasks that were then rejected give none.
func TestUsualCountsAcceptedAttemptsOnly(t *testing.T) {
	w := newKernelWorld(t)
	for range 3 {
		w.endedAs(t, "rejected", "done")
	}
	id := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", time.Hour, codex); c == nil || c.TaskID != id {
		t.Fatalf("claim: %+v", c)
	}
	doc := w.progress(t)
	for _, m := range doc.Work.Missions {
		for _, task := range m.Tasks {
			if task.TaskID == id && task.UsualS != nil {
				t.Fatalf("a usual time of %d s from attempts nobody accepted", *task.UsualS)
			}
		}
	}
}

// Review 181 B3: the recent ended tasks are picked by when they ended, not
// by updated_at, which a plan change moves: of 21 ended tasks the oldest is
// not among the 20, even after a later change touches it.
func TestRecentPicksByEndTime(t *testing.T) {
	w := newKernelWorld(t)
	var first string
	for i := range 21 {
		task, _ := w.ended(t, "done")
		if i == 0 {
			first = task
		}
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "test: touch"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET updated_at = clock_timestamp() WHERE task_id = $1`, first)
		return err
	}))
	doc := w.progress(t)
	if len(doc.Recent.Tasks) != 20 {
		t.Fatalf("%d recent tasks; want 20", len(doc.Recent.Tasks))
	}
	for _, r := range doc.Recent.Tasks {
		if r["task_id"] == first {
			t.Fatal("the oldest ended task is among the recent 20 because a plan change touched it")
		}
	}
}

// sameMoment says a projection time is the database's time t.
func sameMoment(t *testing.T, got string, want time.Time) bool {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, got)
	return err == nil && at.Equal(want)
}

// Review 184 A4: a wait the mission's state decides starts when the mission
// came into that state, not when its task came into its own; a paused
// mission says why, by whom and since when, with tasks or without.
func TestProgressMissionSince(t *testing.T) {
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	time.Sleep(1100 * time.Millisecond)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "budget spent")
	}))
	// A task proposed to M-2 a second after M-2 was: it waits for the Owner
	// since M-2 was proposed.
	_, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-2", []store.TaskProposal{offPlan(proposal("e", w.spec))})
	ok(t, err)
	var pausedAt, proposedAt time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT max(at) FILTER (WHERE mission_id = 'M-1' AND event_type = 'mission_state_changed'),
	    max(at) FILTER (WHERE mission_id = 'M-2' AND event_type = 'mission_proposed') FROM agentrt.event`).Scan(&pausedAt, &proposedAt))
	doc := w.progress(t)
	seen := 0
	for _, m := range doc.Work.Missions {
		switch m.MissionID {
		case "M-1":
			seen++
			if m.Status != store.StatusBlocked || m.Reason != "the mission is paused: budget spent" || !sameMoment(t, m.Since, pausedAt) {
				t.Errorf("M-1 %s %q since %s; want blocked by the pause since %s", m.Status, m.Reason, m.Since, pausedAt)
			}
			if len(m.Tasks) != 1 || m.Tasks[0].TaskID != task || !sameMoment(t, m.Tasks[0].Since, pausedAt) {
				t.Errorf("M-1's task %+v; want %s waiting since the pause, not since it was readied", m.Tasks, task)
			}
		case "M-2":
			seen++
			if m.Status != store.StatusWaitingOwner || len(m.Tasks) != 1 || !sameMoment(t, m.Since, proposedAt) || !sameMoment(t, m.Tasks[0].Since, proposedAt) {
				t.Errorf("M-2 %s since %s, tasks %+v; want its task waiting for the Owner since M-2 was proposed at %s", m.Status, m.Since, m.Tasks, proposedAt)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("missions %+v", doc.Work.Missions)
	}
}

// A paused mission without tasks is blocked by its pause, not idle.
func TestProgressPausedMissionWithoutTasks(t *testing.T) {
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "the Owner asked")
	}))
	for _, m := range w.progress(t).Work.Missions {
		if m.MissionID == "M-1" && (m.Status != store.StatusBlocked || m.Reason != "the mission is paused by controller: the Owner asked" ||
			m.WaitingOn != "coordinator" || m.Since == "") {
			t.Fatalf("M-1 %s %q %s since %q", m.Status, m.Reason, m.WaitingOn, m.Since)
		}
	}
}

// Review 184 B3: more tasks ended and attempts failed at one moment than the
// lists hold: each list is the newest by event, ties of time broken by event
// id, the same on every read.
func TestRecentTiesAtOneMoment(t *testing.T) {
	w := newKernelWorld(t)
	undo := store.FreezeEventClock(time.Now())
	defer undo()
	for range 45 {
		w.ended(t, "failed")
	}
	undo()
	newest := func(q string) []string {
		t.Helper()
		rows, err := w.rt.Query(ctx, q)
		ok(t, err)
		var ids []string
		for rows.Next() {
			var id string
			ok(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		ok(t, rows.Err())
		return ids
	}
	wantTasks := newest(`SELECT task_id FROM agentrt.event WHERE event_type = 'task_state_changed' ORDER BY event_id DESC LIMIT 20`)
	wantFailed := newest(`SELECT attempt_id FROM agentrt.event WHERE event_type = 'attempt_computed' ORDER BY event_id DESC LIMIT 20`)
	for range 2 {
		doc := w.progress(t)
		var tasks, failed []string
		for _, r := range doc.Recent.Tasks {
			tasks = append(tasks, fmt.Sprint(r["task_id"]))
		}
		for _, r := range doc.Recent.FailedAttempts {
			failed = append(failed, fmt.Sprint(r["attempt_id"]))
		}
		if fmt.Sprint(tasks) != fmt.Sprint(wantTasks) {
			t.Fatalf("recent tasks\n%v\nwant the newest by event\n%v", tasks, wantTasks)
		}
		if fmt.Sprint(failed) != fmt.Sprint(wantFailed) {
			t.Fatalf("failed attempts\n%v\nwant the newest by event\n%v", failed, wantFailed)
		}
	}
}

// Review 184 B4: what a plan change holds up comes from one set: the steps it
// touches (an item's are the steps under it), the tasks it moves, and the
// missions of these and the ones it binds to.
func TestChangeBlocks(t *testing.T) {
	w := newKernelWorld(t)
	const s4 = "R2:2.1-4"
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: s4, Parent: item, Kind: "step", Label: "2.1-4", Title: "s4",
			MissionID: "M-2", StepKind: "agent", Ordinal: 3, Root: root})
		return err
	}))
	moved := w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	_, err := w.task(t, "M-2", s4, "", "implement")
	ok(t, err)
	w.ended(t, "done") // an ended task on s1 holds nothing up
	for name, c := range map[string]struct {
		changes []store.PlanChange
		want    string
	}{
		"retitle an item: the tasks on its steps": {[]store.PlanChange{{Op: "retitle", Node: item, Title: "x"}}, "map[missions:[M-1 M-2] unfinished_tasks:3]"},
		"retitle a step":                        {[]store.PlanChange{{Op: "retitle", Node: s1, Title: "x"}}, "map[missions:[M-1] unfinished_tasks:2]"},
		"rebind a step: both missions":          {[]store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, "map[missions:[M-1 M-2] unfinished_tasks:2]"},
		"bind a step with no mission":           {[]store.PlanChange{{Op: "rebind_step", Node: s2, MissionID: "M-2"}}, "map[missions:[M-2] unfinished_tasks:0]"},
		"move a task: where from and where to":  {[]store.PlanChange{{Op: "move_task", Task: moved, Node: s4}}, "map[missions:[M-1 M-2] unfinished_tasks:2]"},
		"add a step to an item":                 {[]store.PlanChange{{Op: "add_node", Parent: item, Kind: "step", Title: "x"}}, "map[missions:[M-1 M-2] unfinished_tasks:3]"},
		"add an item to a round: nothing known": {[]store.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "x"}}, "map[unknown:the change touches no step a mission's work sits on]"},
	} {
		got, err := store.ChangeBlocksForTest(ctx, w.rt, c.changes)
		ok(t, err)
		if fmt.Sprint(got) != c.want {
			t.Errorf("%s: %v; want %s", name, got, c.want)
		}
	}
}

// Review 184 B4: a card is one item however many proposals name it, a card
// decided is no item, and the list stops at its bound and says so.
func TestNeedsYouOneItemPerCard(t *testing.T) {
	w := newWorld(t)
	card := func(id string) func(context.Context, store.Card) (string, error) {
		return func(context.Context, store.Card) (string, error) { return id, nil }
	}
	for key, q := range map[string]string{"c1": "q_same", "c2": "q_same", "c3": "q_other", "c4": "q_done"} {
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename the item "+key,
			[]store.PlanChange{{Op: "retitle", Node: item, Title: key}}, card(q))
		ok(t, err)
	}
	refused := func(_ context.Context, id string) (store.Question, bool) {
		if id == "q_done" {
			return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardRefuseKey, AnsweredBy: "owner",
				AnsweredAt: "2026-10-14T20:00:00Z"}, true
		}
		return store.Question{ID: id, Status: "open"}, true
	}
	if n, err := store.DecidePlanChanges(ctx, w.rt, refused, nil); err != nil || n != 1 {
		t.Fatalf("decided %d (%v); want the one answered card", n, err)
	}
	doc := w.progress(t)
	byID := map[any]map[string]any{}
	for _, it := range doc.NeedsYou.Items {
		byID[it["id"]] = it
	}
	if doc.NeedsYou.Count != 2 || len(byID) != 2 || doc.NeedsYou.Truncated {
		t.Fatalf("needs you %d %v (truncated %v); want one item per card", doc.NeedsYou.Count, doc.NeedsYou.Items, doc.NeedsYou.Truncated)
	}
	if refs, _ := byID["question:q_same"]["also_refs"].([]any); len(refs) != 1 {
		t.Errorf("the shared card's other proposals %v; want one", byID["question:q_same"]["also_refs"])
	}
	defer store.LimitPendingChangesForTest(1)()
	doc = w.progress(t)
	if doc.NeedsYou.Count != 1 || !doc.NeedsYou.Truncated {
		t.Fatalf("needs you at a bound of 1: %d, truncated %v", doc.NeedsYou.Count, doc.NeedsYou.Truncated)
	}
}
