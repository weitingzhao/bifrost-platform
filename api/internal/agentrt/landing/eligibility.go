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
}

// Facts is a fresh snapshot, shared by all gates. Unknown is deliberately not
// Never. Versions let the caller bind its audit record to the rows it locked.
type Facts struct {
	Mission, Reviewers                                               string
	Environment, Mode, Tenant, Repo, Task, Attempt, A, Base          string
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
func ruleOK(f Facts, now time.Time, deploy bool) bool {
	r := f.Rule
	if r == nil || r.Kind != rules.KindLanding || r.Landing == nil || r.Landing.Tenant != f.Tenant || now.Before(r.Effective) || !now.Before(r.Expires) {
		return false
	}
	repo, ok := r.Landing.Repo(f.Repo)
	if !ok {
		return false
	}
	changes := f.Changes
	if deploy {
		if repo.STGRelease == "none" {
			return false
		}
		changes = f.DeployChanges
	}
	return r.Landing.CheckChanges(f.Repo, changes) == nil
}
func EligibleToClaim(f Facts, now time.Time) Decision {
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
	if !f.DependenciesEffective {
		return refuse("dependencies")
	}
	if !ruleOK(f, now, false) {
		return refuse("rule")
	}
	if f.ReservedToday >= f.Rule.Landing.Limits.MaxLandingsPerDay || f.OtherInFlight >= f.Rule.Landing.Limits.MaxInFlightPerRepo {
		return refuse("limit")
	}
	return Decision{Allowed: true}
}
func EligibleToPush(f Facts, now time.Time) Decision {
	if f.History.State == Effective && f.History.Commit != "" && f.History.Commit == f.Operation.R {
		// Already happened: the caller reconciles even after revocation.
		return Decision{Reconcile: true, Reason: "already_on_main"}
	}
	return EligibleToClaim(f, now)
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
