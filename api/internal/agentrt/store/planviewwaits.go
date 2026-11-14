package store

import (

)

// What each task of the plan view waits on (waiting: on, reason, since, and
// question_id or checks when they apply), so the Owner reads where a task
// waits without asking. An unfinished task waits on, in this order: a stop
// that holds it, the Owner (an open card on it, or awaiting_owner), its
// checks (submitted or verifying), else what TaskStatus (progress.go) says
// of it. An accepted task waits on its landing while its latest landing
// operation has not ended. A finished task with no landing to wait on has no
// waiting, nor has a task on main (on_main, progressstop.go), whatever its
// state.

// The values of waiting.on.
const (
	WaitStop         = "stop"
	WaitOwner        = "owner"
	WaitChecks       = "checks"
	WaitLanding      = "landing"
	WaitDependencies = "dependencies"
	WaitHost         = "host"
	WaitQueued       = "queued"
	WaitRework       = "rework"
	WaitBlocked      = "blocked"
	WaitWorking      = "working"
)

