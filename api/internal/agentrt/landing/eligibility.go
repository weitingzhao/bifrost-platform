package landing

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type History string

const (
	Unknown   History = "unknown"
	Never     History = "never"
	Effective History = "effective"
	Reverted  History = "reverted"
)

type MainHistory struct {
	State  History
	Commit string
	// RevertedBy is the commit whose reversal of Commit is in effect, when
	// State is Reverted (S3 B-02: the reconciler records it).
	RevertedBy string
	// Snapshot is the main commit the answer was read at, when the reader
	// pinned one (plan observations keep it as evidence).
	Snapshot string
}

// Facts is a fresh snapshot, shared by all gates. Unknown is deliberately not
// Never. Versions let the caller bind its audit record to the rows it locked.
type Facts struct {
	Mission, Reviewers                                      string
	Environment, Mode, Tenant, Repo, Task, Attempt, A, Base string
	ExecutorPin                                             string
	// GrantEnabled: the Owner's signed standing envelope in force names the
	// coordinator's external host for Repo (§2.1). Until it does, nothing
	// waits for a grant: core and soft failures are refused as before
	// (review 694 R-03).
	GrantEnabled                                                     bool
	MissionState, StepState, ProducingPlan, LatestUsedPlan           string
	ReservedToday, OtherInFlight                                     int
	MissionVersion, StepVersion                                      int
	GoalTask, NoMerge, SingleCommit, DependenciesEffective, Terminal bool
	AcceptedAt, MissionCreated, EnabledAt                            time.Time
	TaskState                                                        string
	History                                                          MainHistory
	Operation                                                        Operation
	SnapshotHistory                                                  MainHistory
	Rule                                                             *rules.Rule
	Changes, DeployChanges                                           []rules.LandingChange
}
type Decision struct {
	Allowed, Reconcile bool
	Reason             string
	Detail             map[string]any
}

func refuse(s string) Decision { return Decision{Reason: s} }
func modeOK(f Facts) bool {
	return f.Environment == "prod" && f.Mode == "live" || f.Environment == "stg" && f.Mode == "dry_run"
}
func planOK(f Facts) bool {
	if f.StepState != "" && !slices.Contains([]string{"planned", "active", "done"}, f.StepState) {
		return false
	}
	return !f.GoalTask || f.ProducingPlan != "" && f.ProducingPlan == f.LatestUsedPlan
}

// ruleValid deliberately does not check paths, presentation or size. Missing
// or expired rules can never be rescued by a grant or an older signed version.
func ruleValid(f Facts, now time.Time) bool {
	r := f.Rule
	if r == nil || r.Kind != rules.KindLanding || r.Landing == nil || r.Landing.Tenant != f.Tenant || now.Before(r.Effective) || !now.Before(r.Expires) {
		return false
	}
	_, ok := r.Landing.Repo(f.Repo)
	return ok
}

// PolicyChecks is shared by preparation and the final gate. Callers must
// still check global/live and runtime limits; soft failures authorize no push.
func PolicyChecks(f Facts, now time.Time) (reason string, refusals []rules.LandingRefusal) {
	if f.Repo == "bifrost-trade-infra" && !ruleValid(f, now) {
		return "rule", nil
	}
	if _, bad := rules.LandingStructuralViolation(f.Repo, f.Changes); bad {
		return "floor", nil
	}
	if _, bad := rules.LandingAbsoluteViolation(f.Repo, f.Changes); bad {
		return "floor", nil
	}
	if !ruleValid(f, now) {
		return "rule", nil
	}
	if _, bad := rules.LandingChangeKindViolation(f.Repo, f.Changes); bad {
		return "floor", nil
	}
	return "", rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
}

func ruleOK(f Facts, now time.Time, deploy bool) bool {
	if deploy {
		if !ruleValid(f, now) {
			return false
		}
		repo, _ := f.Rule.Landing.Repo(f.Repo)
		if repo.STGRelease == "none" {
			return false
		}
		f.Changes = f.DeployChanges
	}
	reason, refusals := PolicyChecks(f, now)
	return reason == "" && len(refusals) == 0
}
func EligibleToClaim(f Facts, now time.Time) Decision {
	if !RepoSupported(f.Repo) {
		return refuse("repo")
	}
	if !modeOK(f) {
		return refuse("mode")
	}
	if f.EnabledAt.IsZero() || f.AcceptedAt.IsZero() || f.MissionCreated.IsZero() || f.AcceptedAt.Before(f.EnabledAt) || f.MissionCreated.Before(f.EnabledAt) {
		return refuse("before_enable")
	}
	if f.MissionState != "active" || f.NoMerge || !planOK(f) {
		return refuse("stale")
	}
	if f.TaskState != "accepted" || f.Attempt == "" || !sha.MatchString(f.A) || !sha.MatchString(f.Base) || !f.SingleCommit {
		return refuse("result")
	}
	if f.History.State != Never {
		return refuse("history")
	}
	if f.Terminal {
		return refuse("terminal")
	}
	// Absolute and structural failures remain unwaivable. Only supported live
	// roots may defer policy failures until the final gate (core includes
	// ordinary runtime roots under §12.3; soft-only roots require a pin).
	reason, soft := PolicyChecks(f, now)
	if reason != "" {
		if reason == "floor" {
			return Decision{Reason: reason, Detail: floorDetail(f)}
		}
		return refuse(reason)
	}
	// Infra's L4/L5 failures are final even while a dependency is pending.
	// Platform retains its existing dependency/grant ordering.
	if f.Repo == "bifrost-trade-infra" && len(soft) > 0 {
		return Decision{Reason: "floor", Detail: floorDetail(f)}
	}
	if !f.DependenciesEffective {
		return refuse("dependencies")
	}
	if len(soft) > 0 && !MayAwaitGrant(f, soft) {
		if _, floor := rules.FloorViolation(f.Repo, f.Changes); floor {
			return refuse("floor")
		}
		return refuse("rule")
	}
	if f.ReservedToday >= f.Rule.Landing.Limits.MaxLandingsPerDay || f.OtherInFlight >= f.Rule.Landing.EffectiveLimits(f.Repo).MaxInFlightPerRepo {
		return refuse("limit")
	}
	return Decision{Allowed: true}
}
func EligibleToPush(f Facts, now time.Time) Decision {
	if f.History.State == Effective && f.History.Commit != "" && f.History.Commit == f.Operation.R {
		// Already happened: the caller reconciles even after revocation.
		return Decision{Reconcile: true, Reason: "already_on_main"}
	}
	d := EligibleToClaim(f, now)
	if !d.Allowed {
		return d
	}
	_, soft := PolicyChecks(f, now)
	if len(soft) > 0 {
		return refuse("awaiting_grant")
	}
	return d
}

// MayAwaitGrant is eligibility to prepare, never permission to push. The
// grant executor separately enforces the action, identity and waiver checks.
func MayAwaitGrant(f Facts, soft []rules.LandingRefusal) bool {
	return f.Environment == "prod" && f.Mode == "live" && f.GrantEnabled && rules.GrantSupported(f.Repo) &&
		(f.ExecutorPin != "" || slices.Contains(soft, rules.LandingCore))
}
func EligibleToDeploy(f Facts, now time.Time) Decision {
	if !modeOK(f) || f.Mode != "live" || f.Operation.DryRun {
		return refuse("mode")
	}
	if !f.Operation.EverLanded || f.Operation.Reverted || f.Operation.R == "" || f.SnapshotHistory.State != Effective || f.SnapshotHistory.Commit != f.Operation.R {
		return refuse("history")
	}
	if !slices.Contains([]string{"active", "done"}, f.MissionState) || f.NoMerge || !planOK(f) {
		return refuse("stale")
	}
	if !ruleOK(f, now, true) {
		return refuse("rule")
	}
	return Decision{Allowed: true}
}

// Reconcile never asks for authorization again: it only observes actions
// already intended. No rule or task status can make their records disappear.
func Reconcile(Facts) Decision { return Decision{Allowed: true, Reconcile: true} }

type FactsReader func(context.Context, string) (Facts, error)

// Evaluate always reads anew, fails closed on any read error, and keeps
// reconciliation independent of eligibility (the caller still handles I/O errors).
func Evaluate(ctx context.Context, read FactsReader, task, stage string, now time.Time) (Decision, error) {
	if stage == "reconcile" {
		return Reconcile(Facts{}), nil
	}
	f, err := read(ctx, task)
	if err != nil {
		return refuse("facts_unreadable"), err
	}
	switch stage {
	case "claim":
		return EligibleToClaim(f, now), nil
	case "push":
		return EligibleToPush(f, now), nil
	case "deploy":
		return EligibleToDeploy(f, now), nil
	}
	return refuse("stage"), fmt.Errorf("unknown landing stage %q", stage)
}

func floorDetail(f Facts) map[string]any {
	if f.Repo != "bifrost-trade-infra" {
		return nil // Preserve platform's existing event body and grant evidence.
	}
	var l *rules.Landing
	if f.Rule != nil {
		l = f.Rule.Landing
	}
	return rules.PolicyFloorDetail(l, f.Repo, f.Changes)
}
