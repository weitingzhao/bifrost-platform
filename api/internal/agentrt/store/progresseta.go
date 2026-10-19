package store

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// When a mission should be done (SPEC §2.2 eta; 2.5 G8,
// CUT-round2-2.5-interface-2026-10-19.md §2; Owner 2026-10-19 「2.5 八条照建议」):
// its unfinished tasks done one after another, each in its usual time (the
// median of the accepted attempts of its role and task class over 30 days,
// usualTimes) — from the median to 1.5 times it — less what a running
// attempt has already spent. A task waiting on its acceptance or review is
// counted by those tasks, not again. What cannot be estimated is said
// (v4 §20.3: 估不准就说估不准): a mission not active, a task that waits on a
// decision, a role and class with fewer than UsualMinSamples samples. Tasks
// the controller adds later (an acceptance, a review) are not counted, so
// the range is a floor of the work proposed now, and the reason says so.

// ETAHighFactor is the top of the range, in usual times.
const ETAHighFactor = 1.5

func unknownETA(reason string) map[string]any {
	return map[string]any{"state": "unknown", "reason": reason}
}

func missionETA(m *progressMission, usual map[string]usualTime, now time.Time) map[string]any {
	if m.state != "active" {
		return unknownETA("the mission is " + m.state + ": a wait for a decision has no usual time")
	}
	if len(m.tasks) == 0 {
		return unknownETA("no unfinished task: nothing proposed to estimate")
	}
	var low, high time.Duration
	var late []string
	type basisKey struct{ role, class string }
	basis := map[basisKey]usualTime{}
	for _, t := range m.tasks {
		switch t.State {
		case "submitted", "verifying":
			continue // its acceptance and review are tasks of their own
		case "blocked", "paused", "awaiting_owner", "proposed":
			if t.State != "proposed" || t.Status == StatusWaitingOwner {
				return unknownETA(fmt.Sprintf("task %s is %s: a wait has no usual time", t.TaskID, t.State))
			}
		}
		u, ok := usual[t.Role+"|"+t.class]
		if !ok {
			return unknownETA(fmt.Sprintf("no usual time for %s %s tasks: fewer than %d accepted in 30 days", t.Role, t.class, UsualMinSamples))
		}
		basis[basisKey{t.Role, t.class}] = u
		var spent time.Duration
		if t.Attempt != nil && t.Attempt.ElapsedS != nil {
			spent = time.Duration(*t.Attempt.ElapsedS) * time.Second
		}
		top := time.Duration(ETAHighFactor * float64(u.d))
		low += max(0, u.d-spent)
		high += max(0, top-spent)
		if spent > top {
			late = append(late, t.TaskID)
		}
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
		rows = append(rows, map[string]any{"role": k.role, "task_class": k.class, "usual_s": int64(basis[k].d / time.Second), "samples": basis[k].n})
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
