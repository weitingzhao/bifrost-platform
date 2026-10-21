package store

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// When the Owner is next needed (SPEC §2.6 owner_next; v4 §20.2 the top line,
// §20.3 rule 6, §20.5 scenario 8; W-31 2.5 gap 1, Owner 2026-10-21 「缺口 1、2
// 照推荐加」): after what waits for him now (Needs you lists that: approvals,
// questions, cards), the open goal whose next step not started is the Owner's
// and is due first. A goal is due when its unfinished tasks are done (its
// eta, progresseta.go); one whose tasks wait on tasks of other goals is due at
// the later of its own time and theirs, followed through every goal it waits
// on. What cannot be timed is said, never guessed (§20.3 rule 7): while any
// such goal cannot be timed, the next time cannot be told either, and each
// goal says what it knows.

// ownerNextCandidate is one open goal whose next step is the Owner's. ready:
// an active goal with nothing unfinished before his step, so by the plan it
// is his turn now; whether a request is up for him to act on is Needs you's
// to say (review 477 B1).
type ownerNextCandidate struct {
	mission, stepID, stepTitle string
	known, ready               bool
	earliest, latest           time.Time
	reason                     string
	after                      []string
}

func (c ownerNextCandidate) doc() map[string]any {
	d := map[string]any{"mission_id": c.mission, "step": map[string]any{"plan_node_id": c.stepID, "title": c.stepTitle},
		"reason": c.reason, "after": nonNil(c.after), "ready": c.ready}
	if c.known {
		d["state"] = "estimated"
		d["earliest"], d["latest"] = c.earliest.UTC().Format(time.RFC3339), c.latest.UTC().Format(time.RFC3339)
	} else {
		d["state"] = "unknown"
	}
	return d
}

// ownerNext is the owner_next document from the listed missions and each
// one's eta; truncated says the list stopped at progressMissions.
func ownerNext(missions []*progressMission, etas map[string]map[string]any, truncated bool, now time.Time) map[string]any {
	byID := map[string]*progressMission{}
	taskMission := map[string]string{}
	for _, m := range missions {
		byID[m.id] = m
		for _, t := range m.tasks {
			taskMission[t.TaskID] = m.id
		}
	}
	var known, unknown []ownerNextCandidate
	for _, m := range missions {
		step, ok := ownerNextStep(m)
		if !ok {
			continue
		}
		c := ownerNextCandidate{mission: m.id, stepID: step[0], stepTitle: step[1]}
		c.known, c.earliest, c.latest, c.reason = dueOf(m, etas[m.id], now)
		if c.known {
			// The eta counts the work proposed now; checks and reviews proposed later are not (progresseta.go).
			c.reason = fmt.Sprintf("%s's next step is yours once its unfinished work is done; work not proposed yet is not counted", m.id)
			if c.ready = len(m.tasks) == 0; c.ready {
				c.reason = fmt.Sprintf("%s's next step is yours: nothing unfinished comes before it; whether a request is up for you, Needs you says", m.id)
			}
		} else {
			c.reason = m.id + ": " + c.reason
		}
		// The goals its tasks wait on, followed through (scenario 8: the later time wins).
		seen := map[string]bool{m.id: true}
		queue := []*progressMission{m}
		for len(queue) > 0 && c.known {
			cur := queue[0]
			queue = queue[1:]
			for _, t := range cur.tasks {
				for _, dep := range t.DependsOn {
					other, listed := taskMission[dep]
					if !listed {
						c.known, c.reason = false, fmt.Sprintf("%s waits on task %s, which is in no open goal listed here", m.id, dep)
						break
					}
					if seen[other] {
						continue
					}
					seen[other] = true
					c.after = append(c.after, other)
					ok, e, l, why := dueOf(byID[other], etas[other], now)
					if !ok {
						c.known, c.reason = false, fmt.Sprintf("%s waits on %s, whose time cannot be told: %s", m.id, other, why)
						break
					}
					c.earliest, c.latest = later(c.earliest, e), later(c.latest, l)
					queue = append(queue, byID[other])
				}
				if !c.known {
					break
				}
			}
		}
		if c.known && len(c.after) > 0 {
			c.reason += ", after " + strings.Join(c.after, ", ")
		}
		if c.known {
			known = append(known, c)
		} else {
			unknown = append(unknown, c)
		}
	}
	slices.SortStableFunc(known, func(a, b ownerNextCandidate) int {
		if d := a.earliest.Compare(b.earliest); d != 0 {
			return d
		}
		return a.latest.Compare(b.latest)
	})
	candidates := make([]any, 0, len(known)+len(unknown))
	for _, c := range append(known, unknown...) {
		candidates = append(candidates, c.doc())
	}
	out := map[string]any{"candidates": candidates}
	switch {
	case truncated:
		out["state"], out["reason"] = "unknown", fmt.Sprintf("more than %d open goals: not all were read", progressMissions)
	case len(unknown) > 0:
		out["state"], out["reason"] = "unknown", unknown[0].reason
		if n := len(unknown) - 1; n > 0 {
			out["reason"] = fmt.Sprintf("%s; and %d more cannot be timed", unknown[0].reason, n)
		}
	case len(known) == 0:
		out["state"], out["reason"] = "none", "no open goal's next step is yours"
	default:
		next := known[0]
		for k, v := range next.doc() {
			if k != "state" {
				out[k] = v
			}
		}
		out["state"] = "estimated"
	}
	return out
}

// ownerNextStep is the mission's next step not started when it is the
// Owner's: its node id and title.
func ownerNextStep(m *progressMission) ([2]string, bool) {
	if len(m.next) == 0 {
		return [2]string{}, false
	}
	step, _ := m.next[0].(map[string]any)
	if owner, _ := step["owner_step"].(bool); !owner {
		return [2]string{}, false
	}
	id, _ := step["plan_node_id"].(string)
	title, _ := step["title"].(string)
	return [2]string{id, title}, true
}

// dueOf is when a mission's unfinished work is done: its eta's range, now
// for an active mission with nothing unfinished, or why it cannot be told.
func dueOf(m *progressMission, eta map[string]any, now time.Time) (bool, time.Time, time.Time, string) {
	if m.state == "active" && len(m.tasks) == 0 {
		return true, now, now, ""
	}
	if eta == nil || eta["state"] != "estimated" {
		reason, _ := eta["reason"].(string)
		if reason == "" {
			reason = "its time is not known"
		}
		return false, time.Time{}, time.Time{}, reason
	}
	e, err1 := time.Parse(time.RFC3339, fmt.Sprint(eta["earliest"]))
	l, err2 := time.Parse(time.RFC3339, fmt.Sprint(eta["latest"]))
	if err1 != nil || err2 != nil || l.Before(e) {
		return false, time.Time{}, time.Time{}, "its estimate does not read"
	}
	return true, e, l, ""
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
