package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestSupersedeTasksAtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	goal := store.Task{MissionID: "M-3", OffPlanReason: "a goal"}
	old := w.ready(t, goal)
	other := w.ready(t, goal)
	replacement := w.ready(t, goal)
	w.accept(t, replacement)
	w.landedNote(t, "M-3", replacement, commitB, "")
	w.moveThrough(t, old, "leased", "running", "submitted")
	entry := func(id string) store.SupersedeEntry {
		var version int
		ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.task WHERE task_id=$1`, id).Scan(&version))
		return store.SupersedeEntry{Task: id, Version: version, Replacement: replacement, Commit: commitB}
	}
	req := store.SupersedeRequest{Env: "prod", Reason: "the listed work is delivered by the replacement", Entries: []store.SupersedeEntry{entry(old), entry(other)}}
	const approval = "appr_0123456789abcdef"
	bad := req
	bad.Entries = append([]store.SupersedeEntry(nil), req.Entries...)
	bad.Entries[1].Version++
	if err := store.SupersedeTasks(ctx, w.rt, bad, approval); err == nil {
		t.Fatal("accepted stale material")
	}
	if w.state(t, "task", old) != "submitted" || w.state(t, "task", other) != "ready" {
		t.Fatal("partial cancellation")
	}
	bad = req
	bad.Entries = append([]store.SupersedeEntry(nil), req.Entries...)
	bad.Entries[0].Commit = commitA
	if err := store.SupersedeTasks(ctx, w.rt, bad, approval); err == nil {
		t.Fatal("accepted missing delivery proof")
	}
	ok(t, store.SupersedeTasks(ctx, w.rt, req, ""))
	if w.state(t, "task", old) != "submitted" {
		t.Fatal("create check mutated tasks")
	}
	ok(t, store.SupersedeTasks(ctx, w.rt, req, approval))
	for _, id := range []string{old, other} {
		if w.state(t, "task", id) != "cancelled" {
			t.Fatal("not cancelled")
		}
	}
	var before, after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&before))
	ok(t, store.SupersedeTasks(ctx, w.rt, req, approval))
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&after))
	if before != after {
		t.Fatal("replay duplicated events")
	}
	if err := store.SupersedeTasks(ctx, w.rt, req, "appr_fedcba9876543210"); err == nil {
		t.Fatal("another approval claimed the cancellation")
	}
	notes, err := store.LandedOnMain(ctx, w.rt, []string{old, other})
	ok(t, err)
	if len(notes) != 0 {
		t.Fatal("superseded originals falsely marked landed")
	}
}

func TestSupersedeAcceptedTaskRequiresUndeliveredResult(t *testing.T) {
	t.Parallel()
	for _, delivered := range []bool{false, true} {
		w := newKernelWorld(t)
		w.goalMission(t, "M-3")
		goal := store.Task{MissionID: "M-3", OffPlanReason: "a goal"}
		old, replacement := w.ready(t, goal), w.ready(t, goal)
		w.accept(t, old)
		w.accept(t, replacement)
		w.landedNote(t, "M-3", replacement, commitB, "")
		if delivered {
			w.landedNote(t, "M-3", old, commitA, "")
		}
		var version int
		ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.task WHERE task_id=$1`, old).Scan(&version))
		req := store.SupersedeRequest{Env: "prod", Reason: "accepted work was replaced before landing", Entries: []store.SupersedeEntry{{Task: old, Version: version, Replacement: replacement, Commit: commitB}}}
		err := store.SupersedeTasks(ctx, w.rt, req, "appr_0123456789abcdef")
		if delivered {
			if err == nil || w.state(t, "task", old) != "accepted" {
				t.Fatal("changed an already delivered task", err)
			}
		} else {
			ok(t, err)
			if w.state(t, "task", old) != "cancelled" {
				t.Fatal("accepted obsolete task not cancelled")
			}
			ok(t, store.SupersedeTasks(ctx, w.rt, req, "appr_0123456789abcdef"))
		}
	}
}

// Serial: FreezeEventClock
func TestSupersedeAcceptedTaskWaitsForLandingToEnd(t *testing.T) {
	for _, terminal := range []string{"failed", "discarded"} {
		w, roots, ops, _ := batchWorld(t, 2, true)
		w.landedNote(t, "M-3", roots[1], commitB, "")
		var version int
		ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.task WHERE task_id=$1`, roots[0]).Scan(&version))
		req := store.SupersedeRequest{Env: "prod", Reason: "replace an unlanded task", Entries: []store.SupersedeEntry{{Task: roots[0], Version: version, Replacement: roots[1], Commit: commitB}}}
		if err := store.SupersedeTasks(ctx, w.rt, req, "appr_0123456789abcdef"); err == nil {
			t.Fatal("cancelled a task with an active landing")
		}
		reason := "ci_red"
		if terminal == "discarded" {
			reason = "main_changed"
		}
		ok(t, appendLanding(w, "landing:bifrost-platform", map[string]any{"v": landing.Version, "kind": terminal, "op": ops[0], "why": reason}))
		ok(t, store.SupersedeTasks(ctx, w.rt, req, "appr_0123456789abcdef"))
	}
}

func TestSupersedeTasksRefusesOpenChecks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	goal := store.Task{MissionID: "M-3", OffPlanReason: "a goal"}
	old := w.ready(t, goal)
	replacement := w.ready(t, goal)
	w.accept(t, replacement)
	w.landedNote(t, "M-3", replacement, commitB, "")
	w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal", Role: "verify", Subject: old, Effect: "none"})
	var version int
	ok(t, w.rt.QueryRow(ctx, `SELECT version FROM agentrt.task WHERE task_id=$1`, old).Scan(&version))
	req := store.SupersedeRequest{Env: "prod", Reason: "superseded", Entries: []store.SupersedeEntry{{Task: old, Version: version, Replacement: replacement, Commit: commitB}}}
	if err := store.SupersedeTasks(ctx, w.rt, req, "appr_0123456789abcdef"); err == nil {
		t.Fatal("cancelled task with an open check")
	}
	if w.state(t, "task", old) != "ready" {
		t.Fatal("changed the task")
	}
}
