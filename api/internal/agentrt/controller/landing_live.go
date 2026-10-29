package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LiveWorkspace is what the live landing needs of a workspace holding R:
// one push of main and its removal (landinggit.Workspace).
type LiveWorkspace interface {
	PushMain(ctx context.Context, r, m0 string, deadline time.Time) landinggit.PushOutcome
	Close() error
}

// recordTimeout bounds writing the push's answer; past it the reconciler
// reads main instead.
const recordTimeout = 30 * time.Second

// landLive is the live landing's last step (S3, CUT §2.5 steps 6–7, §11.5).
// With CI green on R and main at M0 (NextDrill "ready"), it opens a
// workspace holding R first, so the push does not spend its two minutes
// fetching; then the second gate (store.AuthorizeLandingPush) reads main,
// CI and the window again and writes reserved and main_intent; then this
// process pushes R to main once, never past the intent's deadline, and
// records what the remote answered. Nothing here retries a push: a crash
// or an unrecorded answer after the intent is the reconciler's, which reads
// main (R there, or R absent past the fence).
func (c *Controller) landLive(ctx context.Context, src DrillSources, op, r string) error {
	return c.landLiveWith(ctx, src, op, r, false)
}

// landLiveWith is landLive; recheck takes a waiting operation with no
// grant through the gate's rule recheck (store.RecheckWaitingLanding).
func (c *Controller) landLiveWith(ctx context.Context, src DrillSources, op, r string, recheck bool) error {
	if c.Env != "prod" || !c.Live || c.DrillGit == nil && c.OpenLive == nil {
		return fmt.Errorf("live landing requires prod with live on")
	}
	if src.Main == nil || src.CI == nil || src.WindowBlocked == nil {
		return fmt.Errorf("landing sources unavailable")
	}
	var w LiveWorkspace
	var err error
	if c.OpenLive != nil {
		w, err = c.OpenLive(ctx, r)
	} else {
		w, err = c.DrillGit.Open(ctx, r)
	}
	if err != nil {
		return err
	}
	defer w.Close()
	gate := store.AuthorizeLandingPush
	if recheck {
		gate = store.RecheckWaitingLanding
	}
	auth, d, err := gate(ctx, c.DB, src.Facts, op, func(ctx context.Context, o landing.Operation) (store.LiveObservation, error) {
		main, err := src.Main(ctx)
		if err != nil {
			return store.LiveObservation{}, err
		}
		ci, err := src.CI(ctx, o.R, o.PreparedAt)
		if err != nil {
			return store.LiveObservation{}, err
		}
		blocked, err := src.WindowBlocked(ctx)
		if err != nil {
			return store.LiveObservation{}, err
		}
		return store.LiveObservation{Main: main, CI: ci, WindowBlocked: blocked}, nil
	})
	if err != nil || !d.Allowed {
		return err
	}
	// The workspace was opened for the operation's R; an authorization for
	// another commit is a bug. Nothing is pushed or recorded: the intent
	// stands and the reconciler ends it from main.
	if auth.R != r {
		return fmt.Errorf("landing authorization for another commit")
	}
	// Just before the push, the intent must still be this one, open and in
	// time (review 663 R-01): if the reconciler moved it, or it cannot be
	// read, nothing is pushed or recorded.
	if current, err := store.AuthorizationCurrent(ctx, c.DB, auth); err != nil || !current {
		return err
	}
	// From the intent on the push is owed: neither the pass's budget nor a
	// shutdown cuts it short; its deadline (intent + PushTimeout) bounds it.
	out := w.PushMain(context.WithoutCancel(ctx), auth.R, auth.M0, auth.Deadline)
	outcome := string(out)
	if out == landinggit.PushNotSent {
		// Nothing reached the remote: not a verdict (never denied, which
		// stops the repository); the reconciler reads main past the fence.
		outcome = string(landinggit.PushUnknown)
	}
	rec, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	wrote, err := store.RecordPushResult(rec, c.DB, auth, outcome)
	c.logf("agentrt landing %s: push of main %s (recorded %v)", op, out, wrote)
	return err
}
