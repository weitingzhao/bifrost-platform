package store

import (
	"time"
)

// What the runtime knows now, for the chief of staff (the five records of
// WORKMODE §四 the runtime holds): the missions open besides the one asking
// and their unfinished tasks, the latest decisions recorded, the standing
// envelope in force and the mission's budget, the repositories the runtime
// knows and their checks (a goal's plan names only the goal's: planSpec).
// The fifth, the design and its principles, is in the spec's own text.
//
// planSnapshot reads the records; RenderSnapshot writes them, and nothing
// else: the chief exam renders its fixed records with the same function, so
// what the seat reads in an exam is what it reads at work (the exam's cut
// §4.2).

// Snapshot is the records a snapshot is written from.
type Snapshot struct {
	Taken     time.Time
	Missions  []SnapshotMission  // open besides this one, newest first
	Tasks     []SnapshotTask     // theirs, not finished
	Decisions []SnapshotDecision // newest first
	// Envelope is the standing envelope in force, written as a line; ""
	// when none is.
	Envelope string
	Mission  string // the mission the snapshot is for
	Budget   string // its budget, as the database writes budget_json
}

// SnapshotMission is an open mission.
type SnapshotMission struct{ ID, State, Title, Goal string }

// SnapshotTask is an open mission's unfinished task.
type SnapshotTask struct{ Mission, ID, Role, State, Title string }

// SnapshotDecision is a decision recorded.
type SnapshotDecision struct {
	At                                       time.Time
	Mission, Decision, Why, Dropped, Reverse string
}
