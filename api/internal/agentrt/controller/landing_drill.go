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
			d = landing.PreparationDecision(f, now)
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
	EnsureS5CI    func(context.Context, landing.S5CIKey) (string, error)
	Facts         store.LandingSource
	Main          func(context.Context, string) (string, error)
	CI            func(context.Context, string, string, time.Time) (landing.CIRun, error)
	WindowBlocked func(context.Context, string) (bool, error)
	EnsureCI      func(context.Context, string, string, string) error
	Delete        func(context.Context, string, string, string) error
	// EnsurePinnedCI creates CI pinned to a key's sibling commits, and
	// SiblingHeads reads the siblings' main heads to pin (C-5): only an
	// operation that needs a grant uses them. Nil: no operation can be
	// granted (its CI is not pinned).
	EnsurePinnedCI func(ctx context.Context, name string, key store.GrantCIKey) error
	SiblingHeads   func(ctx context.Context) (map[string]string, error)
	// Infra fixes its platform checkout at preparation, before branch push.
	InfraPlatformHead func(context.Context) (string, error)
	InfraCI           func(context.Context, landing.Operation) (landing.CIRun, error)
	EnsureInfraCI     func(context.Context, landing.Operation) error
}

// LandingDrillCI resumes an existing branch_pushed/parked operation from its
// note state. No in-memory checkpoint is authoritative; P0 remains a separate
// prerequisite to enabling the workers loop.
func (c *Controller) LandingDrillCI(ctx context.Context, src DrillSources, op string, now time.Time, repos ...string) (landing.DrillAction, error) {
	repo := landing.Repository(repos...)
	if !c.landingRepoEnabled(repo) {
		return landing.DrillAction{}, fmt.Errorf("landing requires stg (drill) or prod with live on")
	}
	var s landing.State
	var keys map[string]store.GrantCIKey
	err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		s, keys, e = store.LandingStateCIKeys(ctx, tx, "landing:"+repo)
		return e
	})
	if err != nil {
		return landing.DrillAction{}, err
	}
	key, keyed := keys[op]
	o, ok := s.Operations[op]
	if !ok {
		return landing.DrillAction{}, fmt.Errorf("unknown landing operation")
	}
	appendRecord := func(kind string, fields map[string]any) error {
		fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
		return store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
			_, e := store.Append(ctx, tx, store.Event{Stream: "landing:" + repo, Type: "note", Actor: "controller", Body: map[string]any{"text": landingText(c.Env) + kind, "landing": fields}})
			return e
		})
	}
	settleBound := func() (landing.DrillAction, error) {
		why, err := store.SettleLandingOperationBound(ctx, c.DB, op, o, repo)
		if why == "" {
			return landing.DrillAction{Kind: "wait"}, err
		}
		return landing.DrillAction{Kind: "failed", Why: why}, err
	}
	if o.Terminal() {
		if o.Ref != "" && src.Delete != nil && c.Env != "prod" { // B-05
			return landing.DrillAction{Kind: "cleanup"}, src.Delete(ctx, repo, o.Ref, o.R)
		}
		return landing.DrillAction{Kind: "cleanup"}, nil
	}
	if o.Phase != "branch_pushed" && o.Phase != "parked" {
		return landing.DrillAction{}, fmt.Errorf("operation not awaiting CI")
	}
	// Bounds are checked before any external read: an unavailable Gitea or
	// window must not keep the active slot forever.
	_, n := landing.RootBudget(s, o.Task)
	if why := landing.RootBound(s, o.Task, store.DecisionAt(), false); why != "" {
		return settleBound()
	}
	if src.Main == nil || src.CI == nil || src.WindowBlocked == nil {
		return landing.DrillAction{}, fmt.Errorf("drill sources unavailable")
	}
	main, err := src.Main(ctx, repo)
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
		why := ""
		if o.S5.Required {
			why = "S5_BINDING"
		}
		return landing.DrillAction{Kind: "replay", Why: why}, appendRecord("discarded", map[string]any{"why": "main_changed"})
	}
	f, _, err := c.LandingDecision(ctx, src.Facts, o.Task, op, "push", now)
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	if o.S5.Required || landing.S5AutomaticRoute(f) {
		return c.landingS5CI(ctx, src, op)
	}
	if repo == "bifrost-trade-infra" && o.Kicked && landing.PreparationDecision(f, store.DecisionAt()).Allowed {
		if src.EnsureInfraCI == nil {
			return landing.DrillAction{Kind: "wait"}, fmt.Errorf("infra CI creator unavailable")
		}
		if err = src.EnsureInfraCI(ctx, o); err != nil {
			return landing.DrillAction{Kind: "wait"}, err
		}
	}
	ci, err := candidateCI(ctx, src, repo, o)
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	mainCI := landing.CIRun{Status: "Unknown"}
	if ci.Status == "Failed" && repo == "bifrost-platform" {
		mainCI, err = src.CI(ctx, repo, o.M0, time.Time{})
		if err != nil {
			mainCI.Status = "Unknown"
		}
	}
	blocked, windowErr := src.WindowBlocked(ctx, repo)
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
	// An operation that can only reach main through a grant runs CI pinned
	// to sibling commits fixed at its first pinned run, before anything
	// else: whatever other run of R (the push webhook's) says (review 698
	// B-01). Its key comes with the stream read above: no extra read.
	pinnable := c.pinnable(src, f, o, at)
	if keyed && (o.Kicked || o.Rerun) && src.EnsurePinnedCI != nil {
		// A crash between the kick's intent and the run: the pinned run is
		// made sure of before readiness is judged.
		n := 1
		if o.Rerun {
			n = 2
		}
		if err = src.EnsurePinnedCI(ctx, landing.CIName(op, n), key); err != nil {
			return landing.DrillAction{Kind: "wait"}, err
		}
	}
	a := landing.NextDrill(s, op, landing.DrillObservation{Facts: f, Main: main, Candidate: ci, MainCI: mainCI, WindowBlocked: blocked,
		PinCI: pinnable && !keyed}, at)
	if a.Kind == "failed" && (a.Why == "bound" || a.Why == "approval_expired") {
		return settleBound()
	}
	if o.Phase == "branch_pushed" && landing.CIChanged(o.CI, ci) && ci.Run != "" && ci.R == o.R && ci.CreatedAt.After(o.PreparedAt) && !ci.CreatedAt.After(at) {
		if err = appendRecord("ci_seen", landing.CIRecord(op, ci)); err != nil {
			return a, err
		}
	}
	switch a.Kind {
	case "ready":
		if c.Env == "prod" {
			return a, c.landLive(ctx, src, op, o.R, repo)
		}
		return a, c.finishLandingDrill(ctx, src, op, now, repo)
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
			// the cards read (review 670 R-06), ended and started in one
			// transaction (review 672 R-05).
			_, err := store.ReparkLanding(ctx, c.DB, repo, op, o.Why, o.LastAt, a.Why, at.Add(time.Minute), landingText(c.Env))
			return a, err
		}
		return a, appendRecord("parked", map[string]any{"why": a.Why, "retry_at": at.Add(time.Minute).Format(time.RFC3339Nano)})
	case "unparked":
		return a, appendRecord("unparked", map[string]any{})
	case "ci_kick", "ci_rerun":
		if repo == "bifrost-trade-infra" {
			if src.EnsureInfraCI == nil || a.Kind != "ci_kick" {
				return a, fmt.Errorf("infra CI creator unavailable or invalid retry")
			}
			if o.Phase == "parked" {
				return landing.DrillAction{Kind: "unparked"}, appendRecord("unparked", map[string]any{})
			}
			if err = appendRecord(a.Kind, map[string]any{"try": 1}); err != nil {
				return a, err
			}
			return a, src.EnsureInfraCI(ctx, o)
		}
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
		// The grant's evidence is a run pinned to sibling commits fixed at
		// the first pinned run and recorded with it, read strictly (C-5,
		// review 694 B-01); a rerun keeps the key. Every other operation
		// keeps the v1 CI (uiRevision=main).
		if !keyed && pinnable {
			siblings, err := src.SiblingHeads(ctx)
			if err != nil {
				return a, err
			}
			key, keyed = store.GrantCIKey{Repo: f.Repo, Revision: o.R, Base: o.M0, Siblings: siblings}, true
		}
		if !keyed || src.EnsurePinnedCI == nil {
			if err = appendRecord(a.Kind, map[string]any{"try": n}); err != nil {
				return a, err
			}
			return a, src.EnsureCI(ctx, repo, landing.CIName(op, n), o.R)
		}
		if err = store.AppendCIKick(ctx, c.DB, op, a.Kind, n, key, landingText(c.Env)+a.Kind+" (pinned for a grant)"); err != nil {
			return a, err
		}
		return a, src.EnsurePinnedCI(ctx, landing.CIName(op, n), key)
	}
	// Resume creation after a crash between intent and external result. The
	// same name is used on every pass; intentional rerun alone gets try 2.
	if src.EnsureCI != nil && (o.Kicked || o.Rerun) {
		if repo == "bifrost-trade-infra" {
			return a, nil // ensured above, before readiness
		}
		n := 1
		if o.Rerun {
			n = 2
		}
		if keyed {
			if src.EnsurePinnedCI == nil {
				return a, fmt.Errorf("pinned CI creator unavailable")
			}
			return a, nil // made sure of above, before readiness
		}
		return a, src.EnsureCI(ctx, repo, landing.CIName(op, n), o.R)
	}
	return a, nil
}

// pinnable: the operation needs a grant to reach main (its soft failures
// and grants enabled, as the final gate will judge them) and this process
// can pin its CI. Pure: no read.
func (c *Controller) pinnable(src DrillSources, f landing.Facts, o landing.Operation, at time.Time) bool {
	if c.Env != "prod" || o.DryRun || src.EnsurePinnedCI == nil || src.SiblingHeads == nil {
		return false
	}
	reason, soft := landing.PolicyChecks(f, at)
	return reason == "" && len(soft) > 0 && landing.MayAwaitGrant(f, soft)
}

func (c *Controller) finishLandingDrill(ctx context.Context, src DrillSources, op string, now time.Time, repo string) error {
	return store.FinishLandingDrill(ctx, c.DB, src.Facts, op, now, func(ctx context.Context, o landing.Operation) (landing.DrillObservation, error) {
		main, err := src.Main(ctx, repo)
		if err != nil {
			return landing.DrillObservation{}, err
		}
		ci, err := candidateCI(ctx, src, repo, o)
		if err != nil {
			return landing.DrillObservation{}, err
		}
		blocked, err := src.WindowBlocked(ctx, repo)
		if err != nil {
			return landing.DrillObservation{}, err
		}
		return landing.DrillObservation{Main: main, Candidate: ci, WindowBlocked: blocked}, nil
	}, repo)
}

func candidateCI(ctx context.Context, src DrillSources, repo string, o landing.Operation) (landing.CIRun, error) {
	if o.S5.Required && o.Phase != "awaiting_grant" {
		if src.Facts.S5CI == nil || !o.S5.Key.Valid() || o.S5.UID == "" {
			return landing.CIRun{Status: "Missing"}, nil
		}
		ci, err := src.Facts.S5CI(ctx, o.S5.Key, o.S5.UID)
		return s5CIRun(ci), err
	}

	if repo == "bifrost-trade-infra" {
		if src.InfraCI == nil {
			return landing.CIRun{}, fmt.Errorf("infra CI reader unavailable")
		}
		return src.InfraCI(ctx, o)
	}
	return src.CI(ctx, repo, o.R, o.PreparedAt)
}

// landingEnv says whether this controller's environment lands: the STG
// drill, or PROD with Live set by the wiring.
func (c *Controller) landingEnv() bool {
	return c.Env == "stg" || c.Env == "prod" && c.Live
}

// landingRuns is landingEnv with the loop's sources and Git wired.
func (c *Controller) landingRuns() bool {
	return c.landingEnv() && c.Drill != nil && len(c.landingRepoNames()) > 0
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

func (c *Controller) landingRepoEnabled(repo string) bool {
	if !landing.RepoSupported(repo) || !c.landingEnv() {
		return false
	}
	if c.Env != "prod" {
		return true
	}
	if c.LiveRepos == nil {
		return repo == "bifrost-platform"
	}
	for _, r := range c.LiveRepos {
		if r == repo {
			return true
		}
	}
	return false
}
func (c *Controller) landingRepoNames() []string {
	var repos []string
	for _, repo := range landing.LandingRepoNames() {
		if c.DrillGit[repo] != nil && c.landingRepoEnabled(repo) {
			repos = append(repos, repo)
		}
	}
	return repos
}
