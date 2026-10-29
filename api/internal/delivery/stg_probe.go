package delivery

import (
	"context"
	"fmt"
	"slices"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// RequiredSTGReleaseAccess is the RBAC contract for workers. It is returned
// afresh so callers cannot mutate the next probe's required permission set.
func RequiredSTGReleaseAccess() []authv1.ResourceAttributes {
	return []authv1.ResourceAttributes{
		{Namespace: STGReleaseNamespace, Group: "tekton.dev", Resource: "pipelines", Verb: "get"},
		{Namespace: STGReleaseNamespace, Group: "tekton.dev", Resource: "pipelineruns", Verb: "get"},
		{Namespace: STGReleaseNamespace, Group: "tekton.dev", Resource: "pipelineruns", Verb: "list"},
		{Namespace: STGReleaseNamespace, Group: "tekton.dev", Resource: "pipelineruns", Verb: "create"},
		{Namespace: STGReleaseNamespace, Group: "tekton.dev", Resource: "pipelineruns", Verb: "patch"},
		{Namespace: STGReleaseNamespace, Group: "tekton.dev", Resource: "taskruns", Verb: "get"},
		{Namespace: STGReleaseNamespace, Resource: "configmaps", Name: releaseWindowConfigMap, Verb: "get"},
		{Namespace: stgNamespace, Group: "apps", Resource: "deployments", Verb: "get"},
		{Namespace: stgNamespace, Group: "apps", Resource: "replicasets", Verb: "get"},
		{Namespace: stgNamespace, Resource: "pods", Verb: "list"},
		{Namespace: STGReleaseNamespace, Group: "argoproj.io", Resource: "applications", Name: stgNamespace, Verb: "get"},
	}
}

// ProbeAccess performs non-persisted SelfSubjectAccessReviews as the supplied
// client's actual identity. Allowed alone cannot prove admission permits the
// finalizer; ProbeFinalizerAdmission checks that separately. No probe is wired
// into startup or run by B5-b against a cluster.
func (a *STGReleaseAdapter) ProbeAccess(ctx context.Context) error {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	core, err := a.core()
	if err != nil {
		return err
	}
	for _, access := range RequiredSTGReleaseAccess() {
		review, err := core.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &access}}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		if !review.Status.Allowed || review.Status.Denied || review.Status.EvaluationError != "" {
			return fmt.Errorf("STG RBAC probe refused %s %s/%s", access.Verb, access.Namespace, access.Resource)
		}
	}
	return nil
}

// ProbeFinalizerAdmission never persists a mutation: CREATE and PATCH both
// carry server-side DryRunAll. Patch needs an existing held sample, since the
// dry-run Create does not leave an object. Run it as workers before enabling
// B5; a missing/denied/changed sample means unproven, not safe to enable.
func (a *STGReleaseAdapter) ProbeFinalizerAdmission(ctx context.Context, newInput, sample NamedDeliverInput, sampleUID types.UID) error {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return err
	}
	pipe, err := dyn.Resource(pipelineGVR).Namespace(STGReleaseNamespace).Get(ctx, STGReleasePipeline, metav1.GetOptions{})
	if err != nil {
		return err
	}
	obj, err := PrepareNamedDeliverRun(newInput, pipe)
	if err != nil {
		return err
	}
	res := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace)
	created, err := res.Create(ctx, obj, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return err
	}
	if err = MatchNamedDeliverRun(created, newInput, true); err != nil {
		return err
	}
	id, err := sample.Identity()
	if err != nil {
		return err
	}
	existing, err := res.Get(ctx, id.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if sampleUID == "" || existing.GetUID() != sampleUID {
		return ErrSTGCollision
	}
	if err = MatchNamedDeliverRun(existing, sample, true); err != nil {
		return err
	}
	patch, err := finalizerPatch(existing)
	if err != nil || patch == nil {
		return ErrSTGUnknown
	}
	patched, err := res.Patch(ctx, id.Name, types.JSONPatchType, patch, metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return err
	}
	// A successful HTTP response does not prove admission allowed removal:
	// a mutating webhook can put the hold back. Inspect the dry-run result;
	// a GET would only return the deliberately unchanged persisted sample.
	if err = MatchNamedDeliverRun(patched, sample, false); err != nil {
		return err
	}
	if patched.GetUID() != sampleUID {
		return ErrSTGCollision
	}
	remaining := patched.GetFinalizers()
	if slices.Contains(remaining, STGReleaseFinalizer) {
		return fmt.Errorf("%w: admission retained STG release hold", ErrSTGUnknown)
	}
	for _, finalizer := range existing.GetFinalizers() {
		if finalizer != STGReleaseFinalizer && !slices.Contains(remaining, finalizer) {
			return fmt.Errorf("%w: admission removed another finalizer", ErrSTGUnknown)
		}
	}
	return nil
}
