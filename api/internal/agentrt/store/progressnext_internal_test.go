package store

import (
	"reflect"
	"testing"
	"time"
)

// When the Owner is next needed (W-31 2.5 gap 1; v4 §20.2, §20.3 rules 6
// and 7, §20.5 scenario 8), from the listed missions and their etas.
func TestOwnerNext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	at := func(min int) string { return now.Add(time.Duration(min) * time.Minute).Format(time.RFC3339) }
	est := func(lo, hi int) map[string]any {
		return map[string]any{"state": "estimated", "earliest": at(lo), "latest": at(hi)}
	}
	unk := func(why string) map[string]any { return map[string]any{"state": "unknown", "reason": why} }
	step := func(id string, owner bool) any {
		return map[string]any{"plan_node_id": id, "title": "step " + id, "owner_step": owner}
	}
	task := func(id string, deps ...string) *ProgressTask { return &ProgressTask{TaskID: id, DependsOn: deps} }
	mission := func(id, state string, next []any, tasks ...*ProgressTask) *progressMission {
		return &progressMission{id: id, state: state, next: next, tasks: tasks}
	}
	type want struct {
		state, reason, mission, earliest, latest string
		after                                    []string
		order                                    []string // candidates' missions, in order
	}
	check := func(name string, got map[string]any, w want) {
		t.Helper()
		if got["state"] != w.state || got["reason"] != w.reason {
			t.Errorf("%s: state %v, reason %q; want %s, %q", name, got["state"], got["reason"], w.state, w.reason)
		}
		if w.state == "estimated" {
			if got["mission_id"] != w.mission || got["earliest"] != w.earliest || got["latest"] != w.latest ||
				!reflect.DeepEqual(got["after"], nonNil(w.after)) {
				t.Errorf("%s: next %v %v–%v after %v; want %s %s–%s after %v", name, got["mission_id"], got["earliest"], got["latest"],
					got["after"], w.mission, w.earliest, w.latest, w.after)
			}
			if s, _ := got["step"].(map[string]any); s["plan_node_id"] == "" || s["title"] == "" {
				t.Errorf("%s: step %v", name, got["step"])
			}
		} else if _, has := got["mission_id"]; has {
			t.Errorf("%s: a %s answer names %v", name, w.state, got["mission_id"])
		}
		var order []string
		for _, c := range got["candidates"].([]any) {
			order = append(order, c.(map[string]any)["mission_id"].(string))
		}
		if !reflect.DeepEqual(order, w.order) {
			t.Errorf("%s: candidates %v; want %v", name, order, w.order)
		}
	}

	// None: no goal's next step is his (the second one being his does not count yet).
	ms := []*progressMission{mission("M-1", "active", []any{step("a", false), step("b", true)}, task("t1"))}
	check("none", ownerNext(ms, map[string]map[string]any{"M-1": est(10, 15)}, false, now),
		want{state: "none", reason: "no open goal's next step is yours"})

	// One, two: the one due first; ties broken by the latest; candidates in that order.
	ms = []*progressMission{
		mission("M-1", "active", []any{step("a", true)}, task("t1")),
		mission("M-2", "active", []any{step("b", true)}, task("t2")),
		mission("M-3", "active", []any{step("c", true)}, task("t3")),
	}
	etas := map[string]map[string]any{"M-1": est(30, 45), "M-2": est(10, 20), "M-3": est(10, 15)}
	check("earliest", ownerNext(ms, etas, false, now), want{state: "estimated", mission: "M-3", earliest: at(10), latest: at(15),
		reason: "M-3's next step is yours once its unfinished work is done; work not proposed yet is not counted", order: []string{"M-3", "M-2", "M-1"}})

	// Nothing unfinished before his step: due now.
	ms = []*progressMission{mission("M-4", "active", []any{step("d", true)})}
	check("now", ownerNext(ms, map[string]map[string]any{"M-4": unk("no unfinished task: nothing proposed to estimate")}, false, now),
		want{state: "estimated", mission: "M-4", earliest: at(0), latest: at(0),
			reason: "M-4's next step is yours: nothing unfinished comes before it; whether a request is up for you, Needs you says", order: []string{"M-4"}})

	// One that cannot be timed: the next time cannot be told; the known one is still listed, first.
	ms = []*progressMission{
		mission("M-1", "active", []any{step("a", true)}, task("t1")),
		mission("M-2", "paused", []any{step("b", true)}),
		mission("M-5", "active", []any{step("e", true)}, task("t5")),
	}
	etas = map[string]map[string]any{"M-1": est(10, 15), "M-2": unk("the mission is paused: a wait for a decision has no usual time"),
		"M-5": unk("task t5 is host lost: no time can be told until it is heard from")}
	check("unknown", ownerNext(ms, etas, false, now), want{state: "unknown",
		reason: "M-2: the mission is paused: a wait for a decision has no usual time; and 1 more cannot be timed", order: []string{"M-1", "M-2", "M-5"}})

	// Scenario 8: a goal whose task waits on another goal's is due at the later time, followed through.
	ms = []*progressMission{
		mission("M-6", "active", []any{step("f", true)}, task("t6", "t7")),
		mission("M-7", "active", []any{step("g", false)}, task("t7", "t8")),
		mission("M-8", "active", nil, task("t8")),
	}
	etas = map[string]map[string]any{"M-6": est(5, 10), "M-7": est(20, 30), "M-8": est(25, 40)}
	check("after", ownerNext(ms, etas, false, now), want{state: "estimated", mission: "M-6", earliest: at(25), latest: at(40),
		reason: "M-6's next step is yours once its unfinished work is done; work not proposed yet is not counted, after M-7, M-8", after: []string{"M-7", "M-8"}, order: []string{"M-6"}})
	etas["M-8"] = unk("no usual time for implement go tasks: fewer than 3 accepted in 30 days")
	check("after unknown", ownerNext(ms, etas, false, now), want{state: "unknown",
		reason: "M-6 waits on M-8, whose time cannot be told: no usual time for implement go tasks: fewer than 3 accepted in 30 days", order: []string{"M-6"}})
	ms[0].tasks = []*ProgressTask{task("t6", "t9")}
	check("after unlisted", ownerNext(ms, etas, false, now), want{state: "unknown",
		reason: "M-6 waits on task t9, which is in no open goal listed here", order: []string{"M-6"}})

	// Ready only when nothing unfinished comes before his step (review 477 B1).
	ms = []*progressMission{mission("M-4", "active", []any{step("d", true)}), mission("M-1", "active", []any{step("a", true)}, task("t1"))}
	got := ownerNext(ms, map[string]map[string]any{"M-4": unk("no unfinished task"), "M-1": est(10, 15)}, false, now)
	for _, c := range got["candidates"].([]any) {
		c := c.(map[string]any)
		if c["ready"] != (c["mission_id"] == "M-4") {
			t.Errorf("ready: %v", c)
		}
	}

	// A dependency within the goal adds nothing; goals that wait on each other end, with the tasks a DAG (the
	// database refuses a cycle of tasks): M-6's t6a waits on M-7's t7b, M-7's t7c on M-6's t6b (review 480 C1).
	ms = []*progressMission{
		mission("M-6", "active", []any{step("f", true)}, task("t6a", "t7b"), task("t6b"), task("t6c", "t6b")),
		mission("M-7", "active", nil, task("t7b"), task("t7c", "t6b")),
	}
	etas = map[string]map[string]any{"M-6": est(5, 10), "M-7": est(20, 30)}
	check("cycle", ownerNext(ms, etas, false, now), want{state: "estimated", mission: "M-6", earliest: at(20), latest: at(30),
		reason: "M-6's next step is yours once its unfinished work is done; work not proposed yet is not counted, after M-7", after: []string{"M-7"}, order: []string{"M-6"}})

	// An eta missing, one whose times do not read, or whose range runs backwards: unknown, never a guess (review 477 C1).
	for name, eta := range map[string]map[string]any{
		"missing":   nil,
		"bad time":  {"state": "estimated", "earliest": "soon", "latest": at(10)},
		"backwards": {"state": "estimated", "earliest": at(20), "latest": at(10)},
	} {
		ms = []*progressMission{mission("M-1", "active", []any{step("a", true)}, task("t1"))}
		why := "M-1: its estimate does not read"
		if eta == nil {
			why = "M-1: its time is not known"
		}
		check(name, ownerNext(ms, map[string]map[string]any{"M-1": eta}, false, now), want{state: "unknown", reason: why, order: []string{"M-1"}})
	}

	// More goals than are listed: not all were read.
	ms = []*progressMission{mission("M-1", "active", []any{step("a", true)}, task("t1"))}
	check("truncated", ownerNext(ms, map[string]map[string]any{"M-1": est(10, 15)}, true, now), want{state: "unknown",
		reason: "more than 100 open goals: not all were read", order: []string{"M-1"}})
}
