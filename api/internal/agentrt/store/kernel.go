package store

import (
	"errors"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The kernel: the state machine of missions, tasks and attempts that the
// HTTP routes of design v5 §5.2 call. Every change is an event plus the row
// it names, in one transaction; missions, tasks and steps move only along
// their transition tables (T25).
//
// Lock order, everywhere: the writer of a route (its own lock and, when this
// write registers it, the registration lock and the system stream; review
// 176 A1), then the dependency graph, then steps, then attempts, then tasks
// (verification, Conclude, takes a subject before its check), then mission,
// then the event stream (Append): every write takes the row locks it needs
// before it appends (review 149 B1). A batch of proposals never waits while
// it holds a task lock: it takes steps and subjects NOWAIT and the tasks it
// depends on FOR KEY SHARE, which no state change conflicts with (review 176
// A2).
// Task rows are locked FOR NO KEY UPDATE, which foreign key checks (KEY
// SHARE) never wait for.

var (
	// ErrFenced: the caller does not hold this attempt (another host, an old
	// epoch, an attempt that is no longer live, or past expiry plus grace).
	ErrFenced = errors.New("store: this attempt's lease is not yours")
	// ErrConflict: an idempotency key reused with a different request.
	ErrConflict = actor.ErrConflict
	// ErrInGrace: salvage refused because the lease may still be renewed.
	ErrInGrace = errors.New("store: the attempt is still within its grace period")
	// ErrState: the operation does not fit the current state.
	ErrState = eventlog.ErrState
)

// DB is what the kernel needs: a pool or a connection (eventlog.DB).
type DB = eventlog.DB

// testHook, set only by tests, runs at named points so a test can hold a
// call there and observe or race it.
// It is loaded atomically: a test may remove it while a call it staged on
// another goroutine still passes a hook point.
var testHook = &actor.TestHook

func hook(point, arg string) {
	if f := testHook.Load(); f != nil {
		(*f)(point, arg)
	}
}

func timeText(t time.Time) string { return t.UTC().Truncate(time.Microsecond).Format(timeLayout) }

// Missions.

var missionMoves = map[string][]string{
	"proposed":       {"awaiting_owner", "active", "stopped"},
	"awaiting_owner": {"active", "stopped"},
	"active":         {"paused", "done", "stopped", "expired"},
	"paused":         {"active", "stopped", "expired"},
}

// Tasks.

// taskMoves is every task state change there is. The attempt routes below
// make the moves into and out of leased, running and submitted; verification
// makes the moves out of submitted and verifying; MoveTask makes the rest.
var taskMoves = map[string][]string{
	"proposed":       {"ready", "awaiting_owner", "blocked", "cancelled"},
	"awaiting_owner": {"ready", "cancelled"},
	"ready":          {"leased", "paused", "blocked", "cancelled"},
	"leased":         {"running", "ready", "blocked"},
	"running":        {"submitted", "ready", "blocked"},
	"submitted":      {"verifying", "accepted", "blocked"}, // accepted: a verification or review task once its result is used
	"verifying":      {"accepted", "rework", "rejected", "blocked"},
	"rework":         {"ready", "cancelled"},
	"blocked":        {"ready", "cancelled"},
	"paused":         {"ready", "cancelled"},
}

// Hosts.

// HostExternal is the kind of the external host.
const HostExternal = "external"

// Controls.
