package server

import (
	"context"
	"errors"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

// What the release policy's apply section reads (Owner 2026-11-14): the plan
// of an apply_manifest request and the cluster's copy of each object. Each
// fails rather than guessing, and a source that is not connected is an
// error, so the policy covers nothing then.

// policyPlans reads a plan through the service that will apply it, so the
// policy judges the objects Apply would write.
type policyPlans struct {
	summarize func(ctx context.Context, planID string) (workactions.Summary, error)
}

func (p policyPlans) ApplyPlan(ctx context.Context, planID string) (releasepolicy.ApplyPlan, error) {
	if p.summarize == nil {
		return releasepolicy.ApplyPlan{}, errors.New("the manifest actions are not wired")
	}
	sum, err := p.summarize(ctx, planID)
	if err != nil {
		return releasepolicy.ApplyPlan{}, err
	}
	out := releasepolicy.ApplyPlan{Ready: sum.Ready, Policy: sum.Policy, Repo: sum.Repo, Path: sum.Path, Commit: sum.Commit}
	for _, o := range sum.Objects {
		out.Objects = append(out.Objects, releasepolicy.ApplyObject{
			Namespace: o.Namespace, Kind: o.Kind, Name: o.Name, Ownership: o.Ownership,
		})
	}
	return out, nil
}

// policyLive reads an object's field managers through the service that
// knows which kinds the applier writes.
type policyLive struct {
	managers func(ctx context.Context, namespace, kind, name string) ([]workactions.ManagedBy, bool, error)
}

func (p policyLive) Managers(ctx context.Context, namespace, kind, name string) ([]releasepolicy.FieldManager, bool, error) {
	if p.managers == nil {
		return nil, false, errors.New("the manifest actions are not wired")
	}
	list, found, err := p.managers(ctx, namespace, kind, name)
	if err != nil || !found {
		return nil, false, err
	}
	out := make([]releasepolicy.FieldManager, 0, len(list))
	for _, m := range list {
		out = append(out, releasepolicy.FieldManager{Manager: m.Manager, Operation: m.Operation})
	}
	return out, true, nil
}
