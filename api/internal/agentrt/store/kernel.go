package store

import (
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

// Controls.
