package planpace

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// When a mission should be done (SPEC §2.2 eta; 2.5 G8,
// CUT-round2-2.5-interface-2026-10-19.md §2; Owner 2026-10-19 「2.5 八条照建议」):
// its unfinished tasks done one after another, each in its usual time (the
// median of the accepted attempts of its role and task class over 30 days,
// usual times) — from the median to 1.5 times it — less what a running
// attempt has already spent. A task waiting on its acceptance or review is
// counted by those tasks, not again. What cannot be estimated is said
// (v4 §20.3: 估不准就说估不准): a mission not active, a task that waits on a
// decision, a role and class with fewer than minSamples samples. Tasks
// the controller adds later (an acceptance, a review) are not counted, so
// the range is a floor of the work proposed now, and the reason says so.

// ETAHighFactor is the top of the range, in usual times.
const ETAHighFactor = 1.5

func unknownETA(reason string) map[string]any {
	return map[string]any{"state": "unknown", "reason": reason}
}

// MissionETA projects already-read task observations; it changes no task state.
func MissionETA(m *ETAMission, usual map[string]UsualTime, now time.Time, minSamples int) map[string]any {
	if m.State != "active" {
		return unknownETA("the mission is " + m.State + ": a wait for a decision has no usual time")
	}
	if len(m.Tasks) == 0 {
		return unknownETA("no unfinished task: nothing proposed to estimate")
	}
	// A task maybe stuck, on a lost host or of unknown status makes the time
	// one that cannot be told; the time from before is not kept (v4 §20.3
	// rule 7, §20.5: suspend a task and its goal's time is unknown).
	for _, t := range m.Tasks {
		switch t.Status {
		case "maybe_stuck", "host_lost":
			return unknownETA(fmt.Sprintf("task %s is %s: no time can be told until it is heard from", t.TaskID, strings.ReplaceAll(t.Status, "_", " ")))
		case "unknown":
			return unknownETA(fmt.Sprintf("task %s is unknown: no time can be told until its state is known", t.TaskID))
		}
	}
	var low, high time.Duration
	var late []string
	counted := 0
	type basisKey struct{ role, class string }
	basis := map[basisKey]UsualTime{}
	for _, t := range m.Tasks {
		if t.OnMain {
			continue // its change is on main: no work remains
		}
		if t.Status == "stopped" {
			// It waits for the Owner to lift the stop, not as queued work.
			return unknownETA(fmt.Sprintf("task %s is stopped: a wait for the Owner has no usual time", t.TaskID))
		}
		switch t.State {
		case "submitted", "verifying":
			continue // its acceptance and review are tasks of their own
		case "blocked", "paused", "awaiting_owner", "proposed":
			if t.State != "proposed" || t.Status == "waiting_owner" {
				return unknownETA(fmt.Sprintf("task %s is %s: a wait has no usual time", t.TaskID, t.State))
			}
		}
		u, ok := usual[t.Role+"|"+t.Class]
		if !ok {
			return unknownETA(fmt.Sprintf("no usual time for %s %s tasks: fewer than %d accepted in 30 days", t.Role, t.Class, minSamples))
		}
		basis[basisKey{t.Role, t.Class}] = u
		counted++
		var spent time.Duration
		if t.Attempt != nil && t.Attempt.ElapsedS != nil {
			spent = time.Duration(*t.Attempt.ElapsedS) * time.Second
		}
		top := time.Duration(ETAHighFactor * float64(u.Duration))
		low += max(0, u.Duration-spent)
		high += max(0, top-spent)
		if spent > top {
			late = append(late, t.TaskID)
		}
	}
	if counted == 0 {
		// Submitted work waits for its acceptance and review, which are not
		// proposed yet: nothing to time (review 350 A3).
		return unknownETA("every unfinished task is submitted or verifying: its acceptance and review are not proposed yet")
	}
	rows := make([]any, 0, len(basis))
	keys := make([]basisKey, 0, len(basis))
	for k := range basis {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b basisKey) int {
		if a.role != b.role {
			return cmp.Compare(a.role, b.role)
		}
		return cmp.Compare(a.class, b.class)
	})
	for _, k := range keys {
		rows = append(rows, map[string]any{"role": k.role, "task_class": k.class, "usual_s": int64(basis[k].Duration / time.Second), "samples": basis[k].Samples})
	}
	doc := map[string]any{"state": "estimated",
		"earliest": now.Add(low).UTC().Format(time.RFC3339), "latest": now.Add(high).UTC().Format(time.RFC3339),
		"basis":  rows,
		"reason": "the unfinished tasks one after another, each from its usual time to 1.5 times it; acceptance and review tasks not yet proposed are not counted"}
	if len(late) > 0 {
		doc["past_usual"] = late
	}
	return doc
}

// ETAMission and ETATask are read observations, not executable work or authority.
type ETAMission struct {
	State string
	Tasks []*ETATask
}
type ETATask struct {
	TaskID, Role, Class, State, Status string
	OnMain                             bool
	Attempt                            *ETAAttempt
}
type ETAAttempt struct{ ElapsedS *int64 }
type UsualTime struct {
	Duration time.Duration
	Samples  int
}
