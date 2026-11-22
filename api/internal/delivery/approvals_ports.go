package delivery

import (
	"context"
	"fmt"
	"sync/atomic"
)

// How delivery meets the approvals domain's action runner (actions). A start
// that runs inside an approved action carries the approval's create attempt in
// its context; delivery names the PipelineRun after it, copies the runner's
// identity annotations onto it, and answers a create whose outcome is unknown
// the way the runner reads it. A window renewal is marked with the runner's
// not-recorded header. The server binds the runner's functions
// (server/delivery_ports.go). Until 2026-11-22 this package imported actions
// (the domain wall's row "delivery -> approvals").

// ActionRuns is what delivery uses of the action runner.
type ActionRuns struct {
	// AttemptName names an object for the approved attempt ctx carries
	// (base plus the approval and attempt), and reports whether there is one.
	AttemptName func(ctx context.Context, base string) (string, bool)
	// IdentityAnnotations are the annotations the runner puts on what an
	// approved action creates.
	IdentityAnnotations func(ctx context.Context) map[string]string
	// ObjectExists is the error for an object of that name that already exists.
	ObjectExists func(ctx context.Context, namespace, name string) error
	// ClassifyCreate turns a failed create into the runner's error: an
	// uncertain one when the outcome has to be checked by hand.
	ClassifyCreate func(err error) error
	// IsUncertain reports whether err is the runner's uncertain outcome.
	IsUncertain func(err error) bool
	// NotRecordedHeader is the response header that tells the recorder to
	// leave a route's answer out of the record.
	NotRecordedHeader string
}

var actionRunsBound atomic.Pointer[ActionRuns]

// BindActionRuns sets the runner's functions. It is called once, at
// start-up, before anything is served. Unbound: no start carries an approved
// attempt, a run gets no identity annotations, a failed create is reported as
// it is (never uncertain), and a renewal is not marked.
func BindActionRuns(a ActionRuns) { actionRunsBound.Store(&a) }

func actionRuns() ActionRuns {
	a := ActionRuns{}
	if b := actionRunsBound.Load(); b != nil {
		a = *b
	}
	if a.AttemptName == nil {
		a.AttemptName = func(context.Context, string) (string, bool) { return "", false }
	}
	if a.IdentityAnnotations == nil {
		a.IdentityAnnotations = func(context.Context) map[string]string { return nil }
	}
	if a.ObjectExists == nil {
		a.ObjectExists = func(_ context.Context, namespace, name string) error {
			return fmt.Errorf("%s/%s already exists", namespace, name)
		}
	}
	if a.ClassifyCreate == nil {
		a.ClassifyCreate = func(err error) error { return err }
	}
	if a.IsUncertain == nil {
		a.IsUncertain = func(error) bool { return false }
	}
	return a
}
