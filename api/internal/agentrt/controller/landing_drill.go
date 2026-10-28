package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingDecision uses the common store path, in a fresh transaction on every
// call. This is the S2 judgement path; S3 must add its final gate's locks and
// main_intent atomically before any production push.
func (c *Controller) LandingDecision(ctx context.Context, src store.LandingSource, task, op, stage string, now time.Time) (landing.Facts, landing.Decision, error) {
	var f landing.Facts
	d := landing.Decision{Reason: "facts_unreadable"}
	mode := mode(c.Env)
	err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var err error
		now := store.DecisionAt() // CUT §9: not the pass's start
		f, err = store.LandingFacts(ctx, tx, src, c.Env, mode, task, op, "", now)
		if err != nil {
			return err
		}
		switch stage {
		case "claim":
			d = landing.EligibleToClaim(f, now)
		case "push":
			d = landing.EligibleToPush(f, now)
		default:
			return fmt.Errorf("unsupported drill stage")
		}
		return nil
	})
	return f, d, err
}

// DrillSources is intentionally unable to push main or start a deployment.
// EnsureCI must use name as its idempotency key, looking it up before create.
type DrillSources struct {
	Facts         store.LandingSource
	Main          func(context.Context) (string, error)
	CI            func(context.Context, string, time.Time) (landing.CIRun, error)
	WindowBlocked func(context.Context) (bool, error)
	EnsureCI      func(context.Context, string, string) error
	Delete        func(context.Context, string, string) error
}

// LandingDrillCI resumes an existing branch_pushed/parked operation from its
// note state. No in-memory checkpoint is authoritative; P0 remains a separate
// prerequisite to enabling the workers loop.
func (c *Controller) LandingDrillCI(ctx context.Context, src DrillSources, op string, now time.Time) (landing.DrillAction, error) {
	if !c.landingEnv() {
		return landing.DrillAction{}, fmt.Errorf("landing requires stg (drill) or prod with live on")
	}
	var s landing.State
	err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		s, e = store.LandingState(ctx, tx, "landing:bifrost-platform")
		return e
	})
	if err != nil {
		return landing.DrillAction{}, err
	}
	o, ok := s.Operations[op]
	if !ok {
		return landing.DrillAction{}, fmt.Errorf("unknown landing operation")
	}
	appendRecord := func(kind string, fields map[string]any) error {
		fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
		return store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
			_, e := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": landingText(c.Env) + kind, "landing": fields}})
			return e
		})
	}
	if o.Terminal() {
		if o.Ref != "" && src.Delete != nil && c.Env != "prod" { // B-05
			return landing.DrillAction{Kind: "cleanup"}, src.Delete(ctx, o.Ref, o.R)
		}
		return landing.DrillAction{Kind: "cleanup"}, nil
	}
	if o.Phase != "branch_pushed" && o.Phase != "parked" {
		return landing.DrillAction{}, fmt.Errorf("operation not awaiting CI")
	}
	// Bounds are checked before any external read: an unavailable Gitea or
	// window must not keep the active slot forever.
	first, n := landing.RootBudget(s, o.Task)
	if landing.Bound(first, n-1, store.DecisionAt()) {
		return landing.DrillAction{Kind: "failed", Why: "bound"}, appendRecord("failed", map[string]any{"why": "bound"})
	}
	if src.Main == nil || src.CI == nil || src.WindowBlocked == nil {
		return landing.DrillAction{}, fmt.Errorf("drill sources unavailable")
	}
	main, err := src.Main(ctx)
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	if main == "" {
		return landing.DrillAction{Kind: "wait"}, fmt.Errorf("main head unreadable")
	}
	if main != o.M0 {
		if n >= 8 {
			return landing.DrillAction{Kind: "failed", Why: "bound"}, appendRecord("failed", map[string]any{"why": "bound"})
		}
		return landing.DrillAction{Kind: "replay"}, appendRecord("discarded", map[string]any{"why": "main_changed"})
	}
	f, _, err := c.LandingDecision(ctx, src.Facts, o.Task, op, "push", now)
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	ci, err := src.CI(ctx, o.R, o.PreparedAt)
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	mainCI := landing.CIRun{Status: "Unknown"}
	if ci.Status == "Failed" {
		mainCI, err = src.CI(ctx, o.M0, time.Time{})
		if err != nil {
			mainCI.Status = "Unknown"
		}
	}
	blocked, windowErr := src.WindowBlocked(ctx)
	blocked = blocked || windowErr != nil
	// Decided at the clock after the reads above, not the pass's start: the
	// ten-minute rebuild and thirty-minute stuck checks and the CI's creation
	// time are judged as of now (CUT §9, review 656 B-R1).
	at := store.DecisionAt()
	// The rule, the day's counts and the operation as of at, not as of the
	// decision before the reads (review 659 R659-1): a version that took
	// effect meanwhile decides whether CI is kicked.
	if f, err = store.RefreshLandingFacts(ctx, c.DB, src.Facts, c.Env, mode(c.Env), o.Task, op, f, at); err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	a := landing.NextDrill(s, op, landing.DrillObservation{Facts: f, Main: main, Candidate: ci, MainCI: mainCI, WindowBlocked: blocked}, at)
	if o.Phase == "branch_pushed" && (o.CI.String("run") != ci.Run || o.CI.String("status") != ci.Status || !o.CI.Time("created_at").Equal(ci.CreatedAt)) && ci.Run != "" && ci.R == o.R && ci.CreatedAt.After(o.PreparedAt) && !ci.CreatedAt.After(at) {
		if err = appendRecord("ci_seen", map[string]any{"run": ci.Run, "status": ci.Status, "R": o.R, "created_at": ci.CreatedAt.Format(time.RFC3339Nano)}); err != nil {
			return a, err
		}
	}
	switch a.Kind {
	case "ready":
		if c.Env == "prod" {
			return a, c.landLive(ctx, src, op, o.R)
		}
		return a, c.finishLandingDrill(ctx, src, op, now)
	case "replay":
		return a, appendRecord("discarded", map[string]any{"why": "main_changed"})
	case "failed":
		return a, appendRecord("failed", map[string]any{"why": a.Why})
	case "parked":
		if o.Phase == "parked" {
			if o.Why == a.Why {
				return a, nil
			}
			// The reason changed (a limit park whose CI then ran long): a new
			// park, so its start and reason are what the landing status and
			// the cards read (review 670 R-06). A crash between the two
			// leaves it unparked; the next pass parks it again.
			if err := appendRecord("unparked", map[string]any{}); err != nil {
				return a, err
			}
		}
		return a, appendRecord("parked", map[string]any{"why": a.Why, "retry_at": at.Add(time.Minute).Format(time.RFC3339Nano)})
	case "unparked":
		return a, appendRecord("unparked", map[string]any{})
	case "ci_kick", "ci_rerun":
		if src.EnsureCI == nil {
			return a, fmt.Errorf("CI creator unavailable")
		}
		if o.Phase == "parked" {
			return landing.DrillAction{Kind: "unparked"}, appendRecord("unparked", map[string]any{})
		}
		n := 1
		if a.Kind == "ci_rerun" {
			n = 2
		}
		if err = appendRecord(a.Kind, map[string]any{"try": n}); err != nil {
			return a, err
		}
		return a, src.EnsureCI(ctx, landing.CIName(op, n), o.R)
	}
	// Resume creation after a crash between intent and external result. The
	// same name is used on every pass; intentional rerun alone gets try 2.
	if src.EnsureCI != nil && (o.Kicked || o.Rerun) {
		n := 1
		if o.Rerun {
			n = 2
		}
		return a, src.EnsureCI(ctx, landing.CIName(op, n), o.R)
	}
	return a, nil
}

func (c *Controller) finishLandingDrill(ctx context.Context, src DrillSources, op string, now time.Time) error {
	return store.FinishLandingDrill(ctx, c.DB, src.Facts, op, now, func(ctx context.Context, o landing.Operation) (landing.DrillObservation, error) {
		main, err := src.Main(ctx)
		if err != nil {
			return landing.DrillObservation{}, err
		}
		ci, err := src.CI(ctx, o.R, o.PreparedAt)
		if err != nil {
			return landing.DrillObservation{}, err
		}
		blocked, err := src.WindowBlocked(ctx)
		if err != nil {
			return landing.DrillObservation{}, err
		}
		return landing.DrillObservation{Main: main, Candidate: ci, WindowBlocked: blocked}, nil
	})
}

// landingEnv says whether this controller's environment lands: the STG
// drill, or PROD with Live set by the wiring.
func (c *Controller) landingEnv() bool {
	return c.Env == "stg" || c.Env == "prod" && c.Live
}

// landingRuns is landingEnv with the loop's sources and Git wired.
func (c *Controller) landingRuns() bool {
	return c.landingEnv() && c.Drill != nil && c.DrillGit != nil
}

// landingText starts a landing note's text.
func landingText(env string) string {
	if env == "prod" {
		return "PROD landing: "
	}
	return "STG landing drill: "
}

// mode is the landing mode an environment runs: live on PROD, dry_run else.
func mode(env string) string {
	if env == "prod" {
		return "live"
	}
	return "dry_run"
}
