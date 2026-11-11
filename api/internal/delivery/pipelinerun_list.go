package delivery

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PipelineRunObservations lists every retained cicd run, without a label filter.
func (s *Service) PipelineRunObservations(ctx context.Context) ([]unstructured.Unstructured, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	var out []unstructured.Unstructured
	options := metav1.ListOptions{Limit: 100}
	seen := map[string]bool{}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").List(ctx, options)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("pipeline run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}

// PipelineRunObservations exposes the service's retained runs to runtime wiring.
func (h *Handler) PipelineRunObservations(ctx context.Context) ([]unstructured.Unstructured, error) {
	return h.svc.PipelineRunObservations(ctx)
}
