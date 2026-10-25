// Package safego keeps one goroutine's panic from taking the whole process
// with it.
//
// platform-api runs four work surfaces in a single process: Mission Control,
// Launch Desk, Plugin and Engineer. chi's middleware.Recoverer
// guards only a request handler's own stack, so a panic in any background
// goroutine — the patrol autopilot, the three-second operate-queue drain, a
// probe fan-out — ended the process and took cluster monitoring down with the
// worker that actually failed. Until the bootstrap layers are separated for
// real (L-1 out of band, cicdBootstrapCatalog.ts), this is what stands between
// one bad tick and a blind Console.
//
// Containment is not silence: every contained panic is logged at error level
// with its stack and counted, and GET /health reports the count.
package safego

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

var contained atomic.Int64

// Contained counts panics stopped since start. A non-zero value means some
// worker died mid-flight and the surface it feeds may be stale.
func Contained() int64 { return contained.Load() }

// Recover is the deferred guard for a goroutine body written inline:
//
//	go func() {
//	    defer safego.Recover("satellite.fetchOps")
//	    ...
//	}()
//
// Safe to combine with other defers: a deferred wg.Done still runs, so a
// panicking worker never wedges the WaitGroup its caller is blocked on.
func Recover(name string) {
	if r := recover(); r != nil {
		record(name, r)
	}
}

// Go runs fn in a goroutine whose panic is logged and contained. platform-api
// runs Mission Control, Launch Desk, Plugin and Engineer in one process; chi's
// Recoverer covers only request handlers, so an unrecovered background panic
// would end the whole process and blind the Console because one worker failed.
//
// recover() swallows the panic and the stack trace the runtime would otherwise
// print. The deferred Recover hands it to record, which logs at error level with
// the goroutine name, panic value and stack, and increments the counter exposed
// by Contained and GET /health. Containment must stay visible, or a crash becomes
// a silent stop nobody sees. The process goes on, but the panicking goroutine
// does not resume: fn's remaining work is lost. Wrap each iteration of a
// long-running loop in Do so one bad tick does not end the worker.
func Go(name string, fn func()) {
	go func() {
		defer Recover(name)
		fn()
	}()
}

// Do runs fn on the calling goroutine and contains a panic, reporting whether
// fn finished. Use it for the body of a long-running loop so a bad iteration
// costs one tick instead of the worker: guarding only the outer goroutine
// would stop the loop for good, which is the silent-death failure this package
// exists to avoid.
func Do(name string, fn func()) (completed bool) {
	defer func() {
		if r := recover(); r != nil {
			record(name, r)
			completed = false
		}
	}()
	fn()
	return true
}

func record(name string, r any) {
	contained.Add(1)
	slog.Error("goroutine panic contained",
		"goroutine", name,
		"panic", fmt.Sprint(r),
		"contained_total", contained.Load(),
		"stack", string(debug.Stack()))
}
