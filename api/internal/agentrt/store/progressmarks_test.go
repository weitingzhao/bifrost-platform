package store_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of T24's moments (console/src/api/__fixtures__/agentrt-progress-marks.testdb.json):
// GET /progress with one attempt of each kind a host can be found in and one task or step of each mark,
// written when CONSOLE_MARKS_FIXTURE_WRITE=1; the Console's tests read it.
const consoleMarksFixture = "../../../../console/src/api/__fixtures__/agentrt-progress-marks.testdb.json"

// marksAttempt is a live attempt as Progress lists it.
type marksAttempt struct {
	AttemptID         string `json:"attempt_id"`
	TaskID            string `json:"task_id"`
	HostID            string `json:"host_id"`
	State             string `json:"state"`
	Status            string `json:"status"`
	Persisted         bool   `json:"persisted"`
	LastCheckpointSeq *int64 `json:"last_checkpoint_seq"`
}

// marksDoc is the part of Progress the marks are read from.
type marksDoc struct {
	Hosts []struct {
		HostID string `json:"host_id"`
		Online *bool  `json:"online"`
	} `json:"hosts"`
	Live []marksAttempt `json:"live"`
	Work struct {
		Missions []struct {
			MissionID string         `json:"mission_id"`
			ETA       map[string]any `json:"eta"`
			Tasks     []struct {
				TaskID               string `json:"task_id"`
				Status               string `json:"status"`
				OffPlan              bool   `json:"off_plan"`
				OffPlanReason        string `json:"off_plan_reason"`
				ComputedNotPersisted bool   `json:"computed_not_persisted"`
			} `json:"tasks"`
		} `json:"missions"`
	} `json:"work"`
	Plan []struct {
		ID    string `json:"plan_node_id"`
		Kind  string `json:"kind"`
		Items []struct {
			Label string `json:"label"`
			Count struct {
				Done, Total, Manual, Stopped int
				State                        string
			} `json:"count"`
		} `json:"items"`
	} `json:"plan"`
}

// T24 (design v5 §5.5): with Agent hosts stopped or gone, Progress still
// tells each attempt apart, and carries the three marks the page shows. One
// world holds an attempt working on a host that polls (h1), one on a host
// that stopped polling 10 minutes ago with its lease lapsed (h2: host_lost,
// stale, not persisted), one whose result is computed and not yet persisted
// (h3: still working), and one whose lease lapsed on a host that still polls
// (h4: maybe_stuck); a task outside the plan, with its reason; and a step
// done by a manual record, counted apart. Only h1's and h3's attempts are
// running; the host-lost one is not (T24: "its attempts are not shown as
// doing"), and the goal's time cannot be told while it is so.
func TestProgressCarriesT24sMarks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	claimed := func(host string) (string, store.Fence, string) {
		t.Helper()
		id := w.ready(t, store.Task{Effect: "none"})
		c := w.claim(t, host, time.Hour, codex)
		if c == nil || c.TaskID != id {
			t.Fatalf("claim on %s: %+v; want %s", host, c, id)
		}
		f := fence(c, host)
		ok(t, store.Start(ctx, w.rt, f, "", ""))
		return id, f, c.AttemptID
	}
	lapse := func(attempt string) {
		t.Helper()
		// A lease moves back only with another column and the import event.
		w.legacy(t, `UPDATE agentrt.attempt SET lease_expires_at = clock_timestamp() - interval '1 second',
		  started_at = started_at - interval '1 second' WHERE attempt_id = '`+attempt+`'`)
		for range 2 {
			_, _, err := store.SweepLeases(ctx, w.rt)
			ok(t, err)
		}
	}

	working, _, workingAttempt := claimed("h1")
	gone, goneFence, goneAttempt := claimed("h2")
	_, err := store.Checkpoint(ctx, w.rt, goneFence, 1, nil, false)
	ok(t, err)
	computed, computedFence, computedAttempt := claimed("h3")
	exit := 0
	_, err = store.Computed(ctx, w.rt, computedFence, &exit, "exit", "done")
	ok(t, err)
	stuck, _, stuckAttempt := claimed("h4")
	lapse(goneAttempt)
	lapse(stuckAttempt)
	w.legacy(t, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '10 minutes' WHERE host_id = 'h2'`)
	offPlan := w.ready(t, store.Task{Effect: "none", OffPlanReason: "T24 drill: an errand outside the plan"})

	// A step of M-1 done by a manual record with its evidence.
	const manualStep = "R2:2.1-4"
	w.step(t, manualStep, 3, `{"manual":"checked by hand"}`)
	proof := w.artifact(t, "other", "screenshot")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.RecordManual(ctx, tx, "owner", manualStep, "checked by hand", "looked at it", []string{proof})
	}))
	if moved := w.evaluate(t, manualStep, &deployments{}); moved != "done" {
		t.Fatalf("the manual step moved to %q; want done", moved)
	}

	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.MarshalIndent(out, "", "  ")
	ok(t, err)
	var doc marksDoc
	ok(t, json.Unmarshal(raw, &doc))

	live := map[string]marksAttempt{}
	for _, a := range doc.Live {
		live[a.AttemptID] = a
	}
	for _, want := range []struct{ attempt, task, host, state, status string }{
		{workingAttempt, working, "h1", "running", store.StatusWorking},
		{goneAttempt, gone, "h2", "stale", store.StatusHostLost},
		{computedAttempt, computed, "h3", "running", store.StatusWorking},
		{stuckAttempt, stuck, "h4", "stale", store.StatusMaybeStuck},
	} {
		a, found := live[want.attempt]
		if !found || a.TaskID != want.task || a.HostID != want.host || a.State != want.state || a.Status != want.status || a.Persisted {
			t.Errorf("live attempt on %s: %+v (listed %v); want %s %s, not persisted", want.host, a, found, want.state, want.status)
		}
	}
	if len(doc.Live) != 4 {
		t.Errorf("%d live attempts; want 4", len(doc.Live))
	}
	if seq := live[goneAttempt].LastCheckpointSeq; seq == nil || *seq != 1 {
		t.Errorf("the host-lost attempt's last checkpoint %v; want 1", seq)
	}
	online := map[string]*bool{}
	for _, h := range doc.Hosts {
		online[h.HostID] = h.Online
	}
	for host, want := range map[string]bool{"h1": true, "h2": false, "h3": true, "h4": true} {
		if online[host] == nil || *online[host] != want {
			t.Errorf("host %s online %v; want %v", host, online[host], want)
		}
	}

	tasks := map[string]bool{}
	m1 := 0
	for _, m := range doc.Work.Missions {
		if m.MissionID == "M-1" {
			m1++
		}
		// v4 §20.3 rule 7: a goal with a task on a lost host has no time; the first such task is named.
		if m.MissionID == "M-1" && (m.ETA["state"] != "unknown" || m.ETA["reason"] != "task "+gone+" is host lost: no time can be told until it is heard from") {
			t.Errorf("M-1's eta = %v; want unknown, naming %s as host lost", m.ETA, gone)
		}
		for _, x := range m.Tasks {
			tasks[x.TaskID] = true
			switch x.TaskID {
			case computed:
				if !x.ComputedNotPersisted || x.OffPlan {
					t.Errorf("the computed task: %+v; want computed_not_persisted, on the plan", x)
				}
			case offPlan:
				if !x.OffPlan || x.OffPlanReason != "T24 drill: an errand outside the plan" || x.ComputedNotPersisted || x.Status != store.StatusQueued {
					t.Errorf("the off-plan task: %+v", x)
				}
			default:
				if x.OffPlan || x.ComputedNotPersisted {
					t.Errorf("task %s: %+v; want no mark", x.TaskID, x)
				}
			}
		}
	}
	if m1 != 1 {
		t.Errorf("M-1 is listed %d times; want once", m1)
	}
	for _, id := range []string{working, gone, computed, stuck, offPlan} {
		if !tasks[id] {
			t.Errorf("task %s is not in the projection", id)
		}
	}

	counted := false
	for _, r := range doc.Plan {
		if r.ID != root || r.Kind != "round" {
			continue
		}
		for _, i := range r.Items {
			if i.Label == "2.1" {
				counted = true
				if i.Count.Total != 4 || i.Count.Done != 1 || i.Count.Manual != 1 || i.Count.Stopped != 0 {
					t.Errorf("item 2.1 counts %+v; want 1 of 4 done, 1 of them by hand", i.Count)
				}
			}
		}
	}
	if !counted {
		t.Errorf("the plan does not count item 2.1: %+v", doc.Plan)
	}
	if t.Failed() {
		return
	}
	if os.Getenv("CONSOLE_MARKS_FIXTURE_WRITE") == "1" {
		ok(t, os.WriteFile(consoleMarksFixture, append(raw, '\n'), 0o644))
	}
}
