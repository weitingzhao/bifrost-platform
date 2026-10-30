package delivery

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ListSucceededSTG walks every page and checks the actual pipelineRef. A
// label is only a server-side index, never proof of execution identity.
func (a *STGReleaseAdapter) ListSucceededSTG(ctx context.Context) ([]*unstructured.Unstructured, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return nil, err
	}
	var out []*unstructured.Unstructured
	seen := map[string]bool{}
	options := metav1.ListOptions{Limit: 100}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			r := &page.Items[i]
			ref, _, _ := unstructured.NestedString(r.Object, "spec", "pipelineRef", "name")
			if ref == STGReleasePipeline && r.GetNamespace() == STGReleaseNamespace && r.GetName() != "" && r.GetUID() != "" && stgRunStatus(r) == "Succeeded" {
				out = append(out, r.DeepCopy())
			}
		}
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("STG run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}

// ExternalContent uses actual clone results, including when a manual run
// requested a branch name. Content then repeats the complete strict checks,
// including this run's own successful pin-sync and deliverRun parameter.
func (o STGRunObservation) ExternalContent() (STGContent, error) {
	var in STGContent
	for task, dst := range map[string]*string{"clone-platform": &in.P, "clone-ui": &in.U, "clone-infra": &in.Infra} {
		value, err := o.archivedResult(task, "bifrost-git-clone-gitea", "commit")
		if err != nil {
			return STGContent{}, err
		}
		*dst = value
	}
	return o.Content(in)
}
