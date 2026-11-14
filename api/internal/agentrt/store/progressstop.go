package store

import ()

// A stopped task reads stopped, a withdrawn one withdrawn (the progress
// projection and the plan view). A stop control holds a task when it names
// the task, its mission or, for a verify or review task, its subject; only
// the Owner lifts it, so a stopped task waits on him, never as queued work.
// A task is withdrawn when the event that cancelled it is a task_state_changed
// written by its proposer's side (a thread:, seat: or agent: actor), never by
// the controller.

// ProgressStop is the stop control that holds a task.
type ProgressStop struct {
	ControlID string `json:"control_id"`
	By        string `json:"by"`
	Reason    string `json:"reason"`
	Since     string `json:"since"`
}

// A task whose change is on main (a landed note, RecordLandings in prune.go)
// reads finished whatever its state: it is not open work, not stopped,
// blocked or queued, not withdrawn, waits for nothing and adds nothing to an
// estimate. A cancelled one stays cancelled; it is only read as finished.

// ProgressOnMain is the commit main holds a task's change as and how the
// change got there (the note's landed_via; "on main" for a note older than
// that key).
type ProgressOnMain struct {
	Commit string `json:"commit"`
	Via    string `json:"via"`
}
