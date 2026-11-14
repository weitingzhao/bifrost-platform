package store

import (
	"time"
)

// Waits past their limit (W-31 2.3; RULE-waits-2026-10-12 总则 1–4, design
// v5 §5.3 「等待计时、看护 → 控制器的 wait_limit_passed」): the platform keeps the
// clock of the waits the runtime itself knows, not a thread or an Agent host,
// so a wait is timed while the thread is gone and the host asleep. Each pass
// records, once a wait, a wait_limit_passed for:
//
//   - attempt_running (RULE-waits classes 1 and 2): an attempt running past
//     twice its task's usual time, and at least the floor of its kind: 30
//     minutes for a plan, an implementation or other work, 20 for a
//     verification, a review or a brief. The usual time is the one the task
//     was proposed with, else the median of its role and class's accepted
//     attempts (the Progress projection's, usualTimes), else only the floor.
//     A stale attempt is the lease loop's, not timed here.
//   - task_ready (class 7): a ready task no host has claimed, its mission
//     active and no stop or pause holding it, past twice its usual time and
//     at least 30 minutes (the class's "twice the usual time of the one in
//     front" taken as its own role and class's).
//   - message_unread (class 3): a message to an interactive thread it has not
//     acknowledged after 10 minutes, for at most WaitMessageHorizon.
//
// The Owner's waits (classes 4 and 5) are timed by the nudges (briefdue.go),
// and the runtime records nothing of external waits (class 6) in round 2.
// The record is the first of the rule's three steps; it names whom the wait
// calls (calls) and changes nothing else: no state, no message, no notice.
// The Owner reads the records in the scheduled brief that covers them
// (waitsPassed). One wait is told apart from its record: a brief of the chief of staff's
// ready past its limit is told to the Owner by the platform, for the seat
// that would tell him is what does not run (briefwait.go; review 487 A1).

// WaitsStream is the stream the records are on, apart from the missions':
// a record's references (its mission, task, attempt) are checked at its
// commit, which waits for a transaction holding one of those rows, and
// such a transaction (a start, a checkpoint, a submit: the attempt's row,
// then its mission's stream) never waits for this stream, so the two never
// deadlock.
const WaitsStream = "waits"

// The waits.
const (
	WaitAttemptRunning = "attempt_running"
	WaitTaskReady      = "task_ready"
	WaitMessageUnread  = "message_unread"
)

// The limits (RULE-waits §2): a limit is WaitFactor times the usual time,
// and never below its floor.
const (
	WaitFactor         = 2
	WaitWorkFloor      = 30 * time.Minute // a plan, an implementation, other work; a ready task
	WaitCheckFloor     = 20 * time.Minute // a verification, a review, a brief
	WaitMessageLimit   = 10 * time.Minute
	WaitMessageHorizon = 7 * 24 * time.Hour // older unread mail is no longer a wait anyone is in
	waitsPerPass       = 20                 // of each wait, the oldest first; the rest on the next pass
)

// What a wait's limit came from.
const (
	basisTaskUsual    = "task_usual"
	basisHistoryUsual = "history_usual"
	basisFloor        = "floor"
)
