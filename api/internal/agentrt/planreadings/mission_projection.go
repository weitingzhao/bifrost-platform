package planreadings

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"

// MissionTaskProjection contains display fields and its unchanged response document.
// Document is opaque data supplied by the reader; no store dependency or execution hook.
type MissionTaskProjection struct {
	Status, Reason, Since string
	WaitingRoles          []string
	Document              any
}

// MissionProjection is the read-only input for the mission card. Ranking,
// step counting, on-main evidence, stopped status and transaction ownership
// remain with the reader. This projection only formats their existing results.
type MissionProjection struct {
	ID, Title, Goal, State, Deadline, Work               string
	StateAt, StateReason, StateBy, StateQuestion, LastAt string
	Count                                                plan.Count
	SelectedStatus                                       string
	Tasks                                                []MissionTaskProjection
	OnMain                                               []any
	Next                                                 []any
}

// MissionDocument formats one mission with the reader-selected status (SPEC §3.2)
// and its already-computed ETA. It grants no permission and changes no state.
func MissionDocument(m MissionProjection, eta map[string]any) map[string]any {
	tasks := make([]any, 0, len(m.Tasks)+len(m.OnMain))
	for _, t := range m.Tasks {
		tasks = append(tasks, t.Document)
	}
	for _, t := range m.OnMain {
		tasks = append(tasks, t)
	}
	count := m.Count
	status, reason, since, waiting := m.SelectedStatus, "", "", "none"
	var roles []string
	if status != "" {
		for _, t := range m.Tasks {
			if t.Status == status {
				reason, since, roles = t.Reason, t.Since, t.WaitingRoles
				break
			}
		}
		// A mission on the Owner's card says so, with or without tasks
		// (review 190 B4).
		if status == "waiting_owner" && m.State == "awaiting_owner" && m.StateQuestion != "" {
			reason = missionCardReason(m)
		}
		switch status {
		case "waiting_owner", "stopped":
			waiting = "owner"
		case "waiting_role":
			waiting = "role"
			if len(roles) == 0 {
				waiting = "coordinator"
			}
		case "host_lost", "maybe_stuck", "unknown", "blocked", "queued":
			waiting = "coordinator"
		}
	} else {
		switch {
		case m.State == "proposed" || m.State == "awaiting_owner":
			status, waiting, since = "waiting_owner", "owner", m.StateAt
			reason = "the mission is " + m.State + ": it waits for a signed standing-envelope rule, which nothing the Owner can click gives yet"
			if m.StateQuestion != "" {
				reason = missionCardReason(m)
			}
		case m.State == "paused":
			status, waiting, since = "blocked", "coordinator", m.StateAt
			reason = "the mission is paused"
			if m.StateBy != "" {
				reason += " by " + m.StateBy
			}
			if m.StateReason != "" {
				reason += ": " + m.StateReason
			}
		case count.Total > count.Done:
			status, waiting, reason, since = "idle", "coordinator", "no task proposed for the next step", m.LastAt
		default:
			status, reason, since = "done", "every step is done", m.LastAt
		}
	}
	return map[string]any{"mission_id": m.ID, "title": m.Title, "goal": m.Goal, "state": m.State, "deadline_at": m.Deadline,
		"work_id": m.Work, "slices": map[string]any{"done": count.Done, "total": count.Total, "manual": count.Manual, "stopped": count.Stopped, "state": count.State}, "status": status, "reason": reason, "since": since, "waiting_on": waiting,
		"waiting_roles": nonNilStrings(roles), "tasks": tasks, "next_steps": nonNilAny(m.Next),
		"eta": eta}
}

// missionCardReason is why a mission on the Owner's card waits.
func missionCardReason(m MissionProjection) string {
	return "the mission waits for the Owner's answer on question " + m.StateQuestion + ": " + m.StateReason
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
func nonNilAny(s []any) []any {
	if s == nil {
		return []any{}
	}
	return s
}
