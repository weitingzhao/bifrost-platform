package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// What each task of the plan view waits on (waiting: on, reason, since, and
// question_id or checks when they apply), so the Owner reads where a task
// waits without asking. An unfinished task waits on, in this order: a stop
// that holds it, the Owner (an open card on it, or awaiting_owner), its
// checks (submitted or verifying), else what TaskStatus (progress.go) says
// of it. An accepted task waits on its landing while its latest landing
// operation has not ended. A finished task with no landing to wait on has no
// waiting, nor has a task on main (on_main, progressstop.go), whatever its
// state.

// The values of waiting.on.
const (
	WaitStop         = "stop"
	WaitOwner        = "owner"
	WaitChecks       = "checks"
	WaitLanding      = "landing"
	WaitDependencies = "dependencies"
	WaitHost         = "host"
	WaitQueued       = "queued"
	WaitRework       = "rework"
	WaitBlocked      = "blocked"
	WaitWorking      = "working"
)

// planViewWaitFacts are, per unfinished task of the root, the facts its wait
// is read from: when and why it came into its state, its mission's state and
// why, the open Owner card on it and the reason it was blocked with, and its
// unfinished verify and review tasks, its executor pin and the executors its
// route's candidates name.
const planViewWaitFacts = `SELECT t.task_id, s.at, COALESCE(s.body_json->>'reason', ''),
    COALESCE(m.state, ''), COALESCE(ms.body_json->>'reason', ''), ms.at,
    card.question_id, card.at, COALESCE(card.reason, ''),
    COALESCE((SELECT jsonb_agg(jsonb_build_object('task_id', c.task_id, 'role', c.role, 'state', c.state) ORDER BY c.task_id)
      FROM agentrt.task c WHERE c.subject_task_id = t.task_id AND c.role IN ('verify','review')
        AND c.state NOT IN ('accepted','rejected','cancelled')), '[]'),
    COALESCE(t.executor_pin, ''),
    ARRAY(SELECT DISTINCT j.executor_actor_id FROM jsonb_array_elements_text(t.route_json) r(id)
      JOIN agentrt.job_candidate j ON j.job_candidate_id = r.id ORDER BY 1)
  FROM agentrt.task t JOIN tree USING(plan_node_id)
  LEFT JOIN agentrt.mission m ON m.mission_id = t.mission_id
  LEFT JOIN LATERAL (SELECT e.at, e.body_json FROM agentrt.event e WHERE e.task_id = t.task_id
    AND e.event_type IN ('task_proposed','task_state_changed') ORDER BY e.event_id DESC LIMIT 1) s ON true
  LEFT JOIN LATERAL (SELECT e.at, e.body_json FROM agentrt.event e WHERE e.mission_id = t.mission_id
    AND e.event_type IN ('mission_proposed','mission_state_changed') ORDER BY e.event_id DESC LIMIT 1) ms ON true
  LEFT JOIN LATERAL (SELECT o.body_json->>'question_id' AS question_id, o.at, b.body_json->>'reason' AS reason
    FROM agentrt.event o LEFT JOIN agentrt.event b ON b.event_id = (o.body_json->>'blocked_event_id')::bigint
    WHERE o.task_id = t.task_id AND o.event_type = 'owner_wait_opened' AND ` + cardOpen + `
    ORDER BY o.event_id DESC LIMIT 1) card ON true
  WHERE t.state NOT IN ('accepted','rejected','cancelled')`

type planViewWaitFact struct {
	since, missionSince, cardAt         *time.Time
	reason, mission, missionReason, why string
	question                            *string
	checks                              []map[string]any
	pin                                 string
	routeExecutors                      []string
}

// planViewWaits adds waiting to the plan view's tasks, read in the view's
// transaction after planViewHolds (whose stopped and on_main it reads). The task query's
// depends_on (its dependencies not yet accepted) is read here and not shown
// as such: a task waiting on dependencies names each in waiting.depends_on.
func planViewWaits(ctx context.Context, tx pgx.Tx, root string, tasks []map[string]any, snap *planFactsSnapshot, live map[string]*ProgressAttempt, now time.Time) error {
	open := false
	for _, t := range tasks {
		open = open || !taskFinished(t["state"])
	}
	facts := map[string]planViewWaitFact{}
	if open {
		rows, err := tx.Query(ctx, viewTree+planViewWaitFacts, root)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var f planViewWaitFact
			var checks []byte
			if err = rows.Scan(&id, &f.since, &f.reason, &f.mission, &f.missionReason, &f.missionSince, &f.question, &f.cardAt, &f.why, &checks, &f.pin, &f.routeExecutors); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal(checks, &f.checks); err != nil {
				rows.Close()
				return err
			}
			facts[id] = f
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	// Each wait names what it waits on: the dependencies and the task holding
	// the landing slot are read by id (they may be tasks of another root).
	deps := map[string][]string{}
	holders := map[string]slotHolder{}
	named := map[string]bool{}
	for _, t := range tasks {
		raw, _ := t["depends_on"].([]any)
		delete(t, "depends_on")
		id, _ := t["task_id"].(string)
		for _, d := range raw {
			if d, _ := d.(string); d != "" {
				deps[id] = append(deps[id], d)
				named[d] = true
			}
		}
		if t["state"] == "accepted" {
			if h, ok := landingSlotHolder(snap, id); ok {
				holders[id] = h
				if h.o.Task != "" {
					named[h.o.Task] = true
				}
			}
		}
	}
	names, err := planViewTaskNames(ctx, tx, named)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		id, _ := t["task_id"].(string)
		state, _ := t["state"].(string)
		if t["on_main"] != nil {
			continue
		}
		if taskFinished(state) {
			if state == "accepted" {
				if w := landingWait(snap, id); w != nil {
					if h, ok := holders[id]; ok {
						w["slot_holder"] = h.view(names)
					}
					t["waiting"] = w
				}
			}
			continue
		}
		f := facts[id]
		stop, _ := t["stopped"].(*ProgressStop)
		w := taskWait(state, f, stop, len(deps[id]), live[id], now)
		switch w["on"] {
		case WaitDependencies:
			list := []map[string]any{}
			for _, d := range deps[id] {
				n := names[d]
				list = append(list, map[string]any{"task_id": d, "title": n.title, "state": n.state})
			}
			w["depends_on"] = list
		case WaitHost:
			if e := hostExecutor(live[id]); e != nil {
				w["executor"] = e
			}
		case WaitQueued:
			if e := f.executor(); e != nil {
				w["executor"] = e
			} else {
				w["reason"] = w["reason"].(string) + "; the store names no executor or pool it requires"
			}
		}
		t["waiting"] = w
	}
	return nil
}

type taskName struct{ title, state string }

// planViewTaskNames reads the title and state of each task named.
func planViewTaskNames(ctx context.Context, tx pgx.Tx, named map[string]bool) (map[string]taskName, error) {
	names := map[string]taskName{}
	if len(named) == 0 {
		return names, nil
	}
	ids := make([]string, 0, len(named))
	for id := range named {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT task_id, title, state FROM agentrt.task WHERE task_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	var id string
	var n taskName
	_, err = pgx.ForEachRow(rows, []any{&id, &n.title, &n.state}, func() error {
		names[id] = n
		return nil
	})
	return names, err
}

// hostExecutor is the executor of a task waiting on its host: the live
// attempt's executor and host.
func hostExecutor(a *ProgressAttempt) map[string]any {
	if a == nil || a.Executor == "" && a.HostID == "" {
		return nil
	}
	e := map[string]any{}
	if a.Executor != "" {
		e["executor"] = a.Executor
	}
	if a.HostID != "" {
		e["host_id"] = a.HostID
	}
	return e
}

// executor is what a queued task requires to be claimed, as the claim path
// reads it (claim.go): its executor pin, else the one executor every
// candidate of its route names. nil when the store requires none.
func (f planViewWaitFact) executor() map[string]any {
	switch {
	case f.pin != "":
		return map[string]any{"executor": f.pin}
	case len(f.routeExecutors) == 1:
		return map[string]any{"executor": f.routeExecutors[0]}
	}
	return nil
}

func taskFinished(state any) bool {
	return state == "accepted" || state == "rejected" || state == "cancelled"
}

// taskWait is an unfinished task's waiting.
func taskWait(state string, f planViewWaitFact, stop *ProgressStop, deps int, a *ProgressAttempt, now time.Time) map[string]any {
	w := map[string]any{}
	since := stamp(f.since)
	switch {
	case stop != nil:
		w["on"], w["reason"] = WaitStop, stopWords(stop)
		since = stop.Since
	case f.question != nil:
		w["on"], w["question_id"] = WaitOwner, *f.question
		w["reason"] = "the task waits for the Owner's answer on question " + *f.question
		if f.why != "" {
			w["reason"] = w["reason"].(string) + ": " + f.why
		}
		since = stamp(f.cardAt)
	case state == "awaiting_owner":
		w["on"] = WaitOwner
		_, w["reason"] = TaskStatus(StatusInput{TaskState: state, Now: now})
	case state == "submitted" || state == "verifying":
		_, reason := TaskStatus(StatusInput{TaskState: state, Now: now})
		checks := f.checks
		if checks == nil {
			checks = []map[string]any{}
		}
		if len(checks) == 0 {
			reason += "; none is proposed yet"
		}
		w["on"], w["reason"], w["checks"] = WaitChecks, reason, checks
	default:
		status, reason := TaskStatus(StatusInput{TaskState: state, MissionState: f.mission, StateReason: f.reason,
			MissionReason: f.missionReason, Attempt: a, WaitingDeps: deps, Now: now})
		w["on"], w["reason"] = statusWait(status, state, deps, a), reason
		if byMission(status, state) {
			since = stamp(f.missionSince)
		}
	}
	if since != "" {
		w["since"] = since
	}
	return w
}

// statusWait is what a task TaskStatus gave status waits on.
func statusWait(status, state string, deps int, a *ProgressAttempt) string {
	switch status {
	case StatusHostLost, StatusMaybeStuck:
		return WaitHost
	case StatusUnknown:
		if a != nil {
			return WaitHost
		}
		return WaitBlocked
	case StatusWaitingOwner:
		return WaitOwner
	case StatusStopped:
		return WaitStop
	case StatusBlocked:
		return WaitBlocked
	case StatusSlow, StatusWorking:
		return WaitWorking
	case StatusWaitingRole:
		if state == "rework" {
			return WaitRework
		}
		return WaitChecks
	case StatusQueued:
		if deps > 0 {
			return WaitDependencies
		}
	}
	return WaitQueued
}

// landingWait is an accepted task's waiting on its latest landing operation,
// nil when it has none or that one has ended.
func landingWait(snap *planFactsSnapshot, task string) map[string]any {
	id, o, ok := latestTaskLanding(snap, task)
	if !ok || o.Phase == "" || o.Terminal() {
		return nil
	}
	reason := "landing " + id + " is in phase " + o.Phase
	if !o.Active() {
		reason = "landing " + id + " is " + o.Phase + ", not active"
	}
	if o.Why != "" {
		reason += ": " + o.Why
	}
	since := o.ClaimedAt
	if since.IsZero() {
		since = o.LastAt
	}
	return map[string]any{"on": WaitLanding, "reason": reason, "since": timeText(since)}
}

// latestTaskLanding is the task's landing operation last written, the one
// snapshotTaskLanding (planverificationview.go) shows.
func latestTaskLanding(s *planFactsSnapshot, task string) (string, landing.Operation, bool) {
	_, id, o, ok := latestTaskLandingStream(s, task)
	return id, o, ok
}

// latestTaskLandingStream is latestTaskLanding with the stream it is in.
func latestTaskLandingStream(s *planFactsSnapshot, task string) (string, string, landing.Operation, bool) {
	var latest time.Time
	var stream, id string
	var out landing.Operation
	found := false
	for name, state := range s.Landings {
		for op, o := range state.Operations {
			if o.Task != task || !o.LastAt.After(latest) {
				continue
			}
			latest, stream, id, out, found = o.LastAt, name, op, o, true
		}
	}
	return stream, id, out, found
}

// slotHolder is the operation holding the landing slot of a stream.
type slotHolder struct {
	op string
	o  landing.Operation
}

// landingSlotHolder is another operation of the same landing stream as the
// task's latest landing operation that holds the repository's slot
// (landing.Operation.HoldsSlot), one pushing to main first, then by id. An
// operation pushing to main itself is past the slot and waits on no holder.
func landingSlotHolder(s *planFactsSnapshot, task string) (slotHolder, bool) {
	stream, own, o, ok := latestTaskLandingStream(s, task)
	if !ok || o.Phase == "" || o.Terminal() || o.AtMain() {
		return slotHolder{}, false
	}
	var out slotHolder
	found := false
	for op, other := range s.Landings[stream].Operations {
		if op == own || !other.HoldsSlot() {
			continue
		}
		if found && (out.o.AtMain() && !other.AtMain() || out.o.AtMain() == other.AtMain() && op > out.op) {
			continue
		}
		out, found = slotHolder{op: op, o: other}, true
	}
	return out, found
}

func (h slotHolder) view(names map[string]taskName) map[string]any {
	v := map[string]any{"operation": h.op, "task_id": h.o.Task, "title": names[h.o.Task].title, "phase": h.o.Phase}
	if !h.o.LastAt.IsZero() {
		v["since"] = timeText(h.o.LastAt)
	}
	return v
}
