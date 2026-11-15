package releasepolicy

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A signed policy may cover apply_manifest (its apply section, Owner
// 2026-11-14): a tier C apply whose every object is a kind and namespace the
// section names, that the applier applied before (the cluster's copy of it
// carries the applier's field manager, read when the request is judged),
// from a path the section does not exclude. The way back from such an apply is the same path at an
// earlier commit of main, which the same rule covers. A policy without the
// section covers no apply, as before it existed.

const applyAction = "apply_manifest"

// ownershipApplied is the plan's ownership of an object the applier writes
// without taking fields from another manager (workactions.Service.Apply).
const ownershipApplied = "ssa"

// Plans reads an apply_manifest plan.
type Plans interface {
	ApplyPlan(ctx context.Context, planID string) (ApplyPlan, error)
}

// ApplyPlan is what the policy reads of a plan: whether it is a finished
// plan of this platform whose allow-list check passed, where it rendered
// from, and every object it rendered.
type ApplyPlan struct {
	Ready              bool
	Policy             string
	Repo, Path, Commit string
	Objects            []ApplyObject
}

// ApplyObject is one rendered object. Its ownership does not say whether the
// object exists: a new object plans as "ssa" too.
type ApplyObject struct {
	Namespace, Kind, Name string
	Ownership             string
}

// Live reads the cluster's own copy of an object.
type Live interface {
	// Managers is the entries of the object's metadata.managedFields, read
	// with a GET of the object itself. found is false when it does not exist.
	Managers(ctx context.Context, namespace, kind, name string) (managers []FieldManager, found bool, err error)
}

// FieldManager is one managedFields entry: who, and by which operation.
type FieldManager struct{ Manager, Operation string }

const (
	// maxApplyObjects bounds the live reads one decision makes; a larger
	// plan is the Owner's to look at.
	maxApplyObjects = 50
	// liveReadTimeout bounds each of those reads.
	liveReadTimeout = 5 * time.Second
)

// previousGeneration is "" when the cluster's copy of o exists and carries
// an Apply entry of the applier's field manager; otherwise why not.
func (e *Engine) previousGeneration(ctx context.Context, o ApplyObject, manager string) string {
	ctx, cancel := context.WithTimeout(ctx, liveReadTimeout)
	defer cancel()
	managers, found, err := e.d.Live.Managers(ctx, o.Namespace, o.Kind, o.Name)
	switch {
	case err != nil:
		return "cannot be read from the cluster: " + err.Error()
	case !found:
		return "does not exist in the cluster: a first creation"
	}
	for _, m := range managers {
		if m.Manager == manager && m.Operation == "Apply" {
			return ""
		}
	}
	return "exists, but " + manager + " has not applied it"
}

// evaluateApply judges one apply_manifest request. Without an apply section
// in a signed policy the answer is the one every non-release action gets.
func (e *Engine) evaluateApply(ctx context.Context, params map[string]any) Decision {
	d := Decision{SHAs: map[string]string{}}
	st := e.Status(ctx)
	if st.policy == nil || st.policy.Apply == nil {
		d.Reasons = []string{applyAction + " is not a release; the policy covers " + releaseAction + " only"}
		return d
	}
	pol, rules := st.policy, st.policy.Apply
	// The policy id is set from here on, covered or not: it is how a caller
	// tells a policy that judges applies from one that does not.
	d.PolicyID = pol.PolicyID
	d.Plan = str(params["plan_id"])
	if st.Frozen {
		d.Reasons = []string{"releases are frozen: " + st.FreezeReason + " (lift: " + UnfreezeCommand + ")"}
		return d
	}
	if !st.Valid {
		d.Reasons = append(append([]string{}, st.Reasons...), "sign a policy: "+SignCommand)
		return d
	}
	d.Clauses = append(d.Clauses, "signature", "unexpired", "not_frozen")
	if !rules.RequirePreviousGeneration {
		d.Reasons = []string{"the policy's apply section does not set require_previous_generation, so it covers nothing"}
		return d
	}
	manager := strings.TrimSpace(rules.FieldManager)
	if manager == "" {
		d.Reasons = []string{"the policy's apply section names no field_manager, so it covers nothing"}
		return d
	}
	if d.Plan == "" {
		d.Reasons = []string{"the request names no plan"}
		return d
	}
	if e.d.Plans == nil || e.d.Live == nil {
		d.Reasons = []string{"no reader for plans or for the cluster's objects"}
		return d
	}
	plan, err := e.d.Plans.ApplyPlan(ctx, d.Plan)
	if err != nil {
		d.Reasons = []string{"cannot read plan " + d.Plan + ": " + err.Error()}
		return d
	}
	d.Path = plan.Repo + ":" + plan.Path
	if plan.Repo != "" && plan.Commit != "" {
		d.SHAs[plan.Repo] = plan.Commit
	}
	if !plan.Ready || plan.Policy != "pass" {
		d.Reasons = []string{"plan " + d.Plan + " is not a finished plan whose allow-list check passed"}
		return d
	}
	d.Clauses = append(d.Clauses, "plan_passed")
	if len(plan.Objects) == 0 {
		d.Reasons = []string{"plan " + d.Plan + " lists no objects"}
		return d
	}

	if len(plan.Objects) > maxApplyObjects {
		d.Reasons = []string{fmt.Sprintf("plan %s lists %d objects, too many to check (more than %d)", d.Plan, len(plan.Objects), maxApplyObjects)}
		return d
	}

	var place, excluded, previous []string
	for _, o := range plan.Objects {
		name := o.Namespace + "/" + o.Kind + "/" + o.Name
		if contains(rules.ExcludeObjects, o.Kind+"/"+o.Name) {
			excluded = append(excluded, name)
		}
		if !contains(rules.Namespaces, o.Namespace) || !contains(rules.Kinds, o.Kind) {
			// Not read from the cluster: the policy names no such object.
			place = append(place, name)
			continue
		}
		if o.Ownership != ownershipApplied {
			previous = append(previous, fmt.Sprintf("%s (ownership %q)", name, o.Ownership))
			continue
		}
		if why := e.previousGeneration(ctx, o, manager); why != "" {
			previous = append(previous, name+" ("+why+")")
		}
	}
	var reasons []string
	if len(place) > 0 {
		reasons = append(reasons, "outside the policy's apply namespaces and kinds: "+listed(place))
	} else {
		d.Clauses = append(d.Clauses, "apply_namespaces", "apply_kinds")
	}
	if len(excluded) > 0 {
		reasons = append(reasons, "excluded by the policy whatever renders it: "+listed(excluded))
	} else {
		d.Clauses = append(d.Clauses, "apply_objects")
	}
	if hit := excludedBy(plan.Path, rules.ExcludePaths); hit != "" {
		reasons = append(reasons, "path "+plan.Path+" is excluded by the policy ("+hit+")")
	} else {
		d.Clauses = append(d.Clauses, "apply_paths")
	}
	if len(previous) > 0 {
		reasons = append(reasons, "no previous generation to go back to: "+listed(previous))
	} else {
		d.Clauses = append(d.Clauses, "previous_generation")
	}
	d.Reasons = reasons
	d.Auto = len(reasons) == 0
	return d
}

// excludedBy returns the first pattern that excludes a plan's path, or "".
// A plan's path is a file or a directory that is rendered whole, so a
// pattern excludes the path it matches, anything under what it matches, and
// any directory that holds something it could match. An empty path is
// excluded by every policy: it names nothing to compare.
func excludedBy(path string, patterns []string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return "no path"
	}
	for _, pattern := range patterns {
		pattern = strings.TrimLeft(pattern, "/")
		if PathMatches(path, pattern) {
			return pattern
		}
		// The literal start of the pattern, up to its first wildcard.
		literal := pattern
		if i := strings.IndexAny(pattern, "*?"); i >= 0 {
			literal = pattern[:i]
		}
		wild := literal != pattern
		switch {
		case strings.HasPrefix(literal, path+"/"): // the path holds what the pattern names
			return pattern
		case wild && strings.HasPrefix(path+"/", literal): // the pattern could match under the path
			return pattern
		case !wild && strings.HasPrefix(path, pattern+"/"): // the path is under what the pattern names
			return pattern
		}
	}
	return ""
}

// listed joins names, the first five spelled out.
func listed(names []string) string {
	if len(names) > 5 {
		return strings.Join(names[:5], ", ") + fmt.Sprintf(" (+%d more)", len(names)-5)
	}
	return strings.Join(names, ", ")
}
