package planstore

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// 2026-11-16 Plan projection extraction: preserve the complete progress card,
// including historical unknown/paused/question/on-main inputs and nil/empty lists.
// The reference below is the unchanged 9e5f7eb5 projection, frozen for comparison.
func TestMissionDocumentProjectionParity(t *testing.T) {
	t.Parallel()
	base := progressMission{id: "M-7", title: "mission", goal: "goal", state: "active", deadline: "2026-11-16T20:00:00Z", work: "W-7", stateAt: "state-time", lastAt: "last-time"}
	cases := map[string]progressMission{}
	for _, state := range []string{"proposed", "awaiting_owner", "active", "paused", "unknown", ""} {
		m := base
		m.state = state
		cases[state+"/empty"] = m
		m.stateQuestion, m.stateReason, m.stateBy = "Q-7", "owner reason", "owner"
		cases[state+"/question"] = m
	}
	for _, status := range []string{StatusHostLost, StatusMaybeStuck, StatusUnknown, StatusWaitingOwner, StatusStopped, statusBlocked, StatusSlow, StatusWorking, StatusWaitingRole, StatusQueued, StatusFinished, "unrecognized"} {
		m := base
		m.tasks = []*ProgressTask{{TaskID: "T-7", Status: status, Reason: "task reason", Since: "task-time", DependsOn: []string{"T-6"}}}
		cases[status+"/task"] = m
	}
	m := base
	m.tasks = []*ProgressTask{{TaskID: "T-7", Status: StatusWaitingRole, WaitingRoles: []string{"reviewer", "builder"}, Reason: "role wait"}}
	cases["waiting-role/roles"] = m
	m = base
	m.state, m.stateQuestion, m.stateReason = "awaiting_owner", "Q-7", "card reason"
	m.tasks = []*ProgressTask{{TaskID: "T-7", Status: StatusWaitingOwner, Reason: "task reason"}}
	cases["awaiting-owner/question-overrides-task-reason"] = m
	m = base
	m.tasks = []*ProgressTask{{TaskID: "T-1", Status: StatusWorking, Reason: "working"}, {TaskID: "T-2", Status: StatusUnknown, Reason: "first unknown"}, {TaskID: "T-3", Status: StatusUnknown, Reason: "second unknown"}}
	cases["rank-and-first-matching-task"] = m
	m = base
	m.onMain = []*ProgressTask{{TaskID: "T-main", Status: StatusUnknown, Reason: "main result"}}
	cases["on-main-not-open-status"] = m
	for _, state := range []string{plan.StatePlanned, plan.StateActive, plan.StateDone, plan.StateStopped, plan.StateDropped, "unrecognized"} {
		m = base
		m.steps = []plan.Step{{ID: "step", State: state, Manual: state == plan.StateDone}}
		cases["step/"+state] = m
	}
	m = base
	m.tasks = []*ProgressTask{}
	m.onMain = []*ProgressTask{}
	m.next = []any{}
	cases["explicit-empty-lists"] = m
	m = base
	m.next = []any{map[string]any{"plan_node_id": "P-8", "title": "next", "owner_step": true}}
	cases["next-step-document"] = m
	for name, mission := range cases {
		t.Run(name, func(t *testing.T) {
			for _, eta := range []map[string]any{nil, {"state": "unknown", "reason": "no samples", "seconds": nil}} {
				want, got := baselineMissionDoc(&mission, eta), missionDoc(&mission, eta)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("projection differs: got %#v want %#v", got, want)
				}
				before, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				after, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatalf("wire response differs: got %s want %s", after, before)
				}
			}
		})
	}
}

// missionDoc is one mission with its status (SPEC §3.2) and when it should
// be done (progresseta.go).
func baselineMissionDoc(m *progressMission, eta map[string]any) map[string]any {
	statuses := make([]string, 0, len(m.tasks))
	tasks := make([]any, 0, len(m.tasks)+len(m.onMain))
	for _, t := range m.tasks {
		statuses = append(statuses, t.Status)
		tasks = append(tasks, t)
	}
	for _, t := range m.onMain {
		tasks = append(tasks, t)
	}
	count := plan.CountSteps(m.steps)
	status, reason, since, waiting := worst(statuses), "", "", "none"
	var roles []string
	if status != "" {
		for _, t := range m.tasks {
			if t.Status == status {
				reason, since, roles = t.Reason, t.Since, t.WaitingRoles
				break
			}
		}
		// A mission on the Owner's card says so, with or without tasks
		// (review 190 B4).
		if status == StatusWaitingOwner && m.state == "awaiting_owner" && m.stateQuestion != "" {
			reason = baselineMissionCardReason(m)
		}
		switch status {
		case StatusWaitingOwner, StatusStopped:
			waiting = "owner"
		case StatusWaitingRole:
			waiting = "role"
			if len(roles) == 0 {
				waiting = "coordinator"
			}
		case StatusHostLost, StatusMaybeStuck, StatusUnknown, statusBlocked, StatusQueued:
			waiting = "coordinator"
		}
	} else {
		switch {
		case m.state == "proposed" || m.state == "awaiting_owner":
			status, waiting, since = StatusWaitingOwner, "owner", m.stateAt
			reason = "the mission is " + m.state + ": it waits for a signed standing-envelope rule, which nothing the Owner can click gives yet"
			if m.stateQuestion != "" {
				reason = baselineMissionCardReason(m)
			}
		case m.state == "paused":
			status, waiting, since = statusBlocked, "coordinator", m.stateAt
			reason = "the mission is paused"
			if m.stateBy != "" {
				reason += " by " + m.stateBy
			}
			if m.stateReason != "" {
				reason += ": " + m.stateReason
			}
		case count.Total > count.Done:
			status, waiting, reason, since = "idle", "coordinator", "no task proposed for the next step", m.lastAt
		default:
			status, reason, since = "done", "every step is done", m.lastAt
		}
	}
	return map[string]any{"mission_id": m.id, "title": m.title, "goal": m.goal, "state": m.state, "deadline_at": m.deadline,
		"work_id": m.work, "slices": countDoc(count), "status": status, "reason": reason, "since": since, "waiting_on": waiting,
		"waiting_roles": nonNil(roles), "tasks": tasks, "next_steps": nonNilAny(m.next),
		"eta": eta}
}

// missionCardReason is why a mission on the Owner's card waits.
func baselineMissionCardReason(m *progressMission) string {
	return "the mission waits for the Owner's answer on question " + m.stateQuestion + ": " + m.stateReason
}
