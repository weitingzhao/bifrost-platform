package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Goals (W-31 2.2b, the thin chief of staff; the Owner chose D1 A on
// 2026-10-15). A goal typed in the Console goes to the chief of staff's seat
// first: it becomes a mission and one planning task, in one transaction under
// one key. The planning task's spec is the goal, a snapshot of what the
// runtime knows now and the format of a plan. A fresh session in the seat
// reads it, plans read-only and hands the plan in; the controller checks it
// and records it (planresult.go). The seat's memory is these records, not a
// conversation (WORKMODE-owner-chief-of-staff-2026-10-15 §四).
//
// The snapshot is taken once, when the goal is proposed: a retry under the
// same key gets the first answer, whatever changed since (review 213 A1's
// concern, a mission no retry can finish, cannot arise: mission, spec and
// task are written together or not at all).

// Goal is a goal to propose: its first line as the title, its text, the
// repositories it is about (the Owner 10-19, T26: a goal may name several),
// and how to read the commit of a repository's main its tasks start from.
type Goal struct {
	Title, Text string
	Repos       []string
	NoMerge     string // why its accepted results stay off main, "" for none (MissionProposal.NoMerge)
	NeedsOwner  string // why its cards wait for the Owner himself, "" for none (MissionProposal.NeedsOwner)
	Main        func(ctx context.Context, repo string) (string, error)
}

// ErrNoMain: a repository's main could not be read for a new goal.
var ErrNoMain = errors.New("store: the repository's main branch cannot be read")

// GoalSuccess is a goal's mission's success criterion until its plan names
// its own.
const GoalSuccess = "the goal's plan is accepted and every task it splits into is accepted"

// PlanOffPlanReason is why a goal's tasks are off the plan tree.
const PlanOffPlanReason = "a goal from the Console, planned by the chief of staff's seat; the runtime has no plan tree yet"

func firstTaskID(t map[string]any) string {
	if list, ok := t["tasks"].([]any); ok && len(list) > 0 {
		if first, ok := list[0].(map[string]any); ok {
			id, _ := first["task_id"].(string)
			return id
		}
	}
	return ""
}

// maxTaskTitle is the longest task title a goal or a plan gives, in runes.
const maxTaskTitle = 100

// cutRunes cuts s to at most n runes, ending in "…" when cut.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// Snapshot bounds: what a planner reads is a page, not the database.
const (
	// a title or a decision's field, and the whole snapshot (review 269 B4)
	snapTitleRunes = 200
	snapFieldRunes = 300
	snapMaxBytes   = 64 << 10
)

// oneLineRunes is s on one line, cut to n runes.
func oneLineRunes(s string, n int) string {
	return cutRunes(strings.Join(strings.Fields(s), " "), n)
}
