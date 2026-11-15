package delivery

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UnitTaskRuns lists the TaskRuns of unit tasks in the delivery namespace:
// the ones carrying the label, whatever its value (CI design v4, landing
// stage 2 block 2-C). selector narrows it further ("" for all of them).
func (s *Service) UnitTaskRuns(ctx context.Context, label, selector string) ([]unstructured.Unstructured, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	if selector == "" {
		selector = label
	}
	// Paged, and each run without the copy of its Task that Tekton keeps in
	// status.taskSpec and without managedFields: the loop that reads these
	// every 30 s reads labels, conditions, times and results (see
	// PipelineRunObservations for what the copies cost the workers).
	var out []unstructured.Unstructured
	options := metav1.ListOptions{LabelSelector: selector, Limit: 100}
	seen := map[string]bool{}
	for {
		page, err := dyn.Resource(taskRunGVR).Namespace(s.PipelinesNamespace()).List(ctx, options)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			unstructured.RemoveNestedField(page.Items[i].Object, "status", "taskSpec")
			unstructured.RemoveNestedField(page.Items[i].Object, "metadata", "managedFields")
		}
		out = append(out, page.Items...)
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("unit task run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}

// CreateUnitTaskRun creates the TaskRun; one of that name being there
// already is not an error and reports false: a unit's run is named by its
// plan and number, so asking twice starts it once.
func (s *Service) CreateUnitTaskRun(ctx context.Context, obj map[string]any) (bool, error) {
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return false, err
	}
	_, err = dyn.Resource(taskRunGVR).Namespace(s.PipelinesNamespace()).Create(ctx, &unstructured.Unstructured{Object: obj}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return false, nil
	}
	return err == nil, err
}

// UnitTaskRuns and CreateUnitTaskRun, for runtime wiring.
func (h *Handler) UnitTaskRuns(ctx context.Context, label, selector string) ([]unstructured.Unstructured, error) {
	return h.svc.UnitTaskRuns(ctx, label, selector)
}

func (h *Handler) CreateUnitTaskRun(ctx context.Context, obj map[string]any) (bool, error) {
	return h.svc.CreateUnitTaskRun(ctx, obj)
}

// PipelinesNamespace is where the platform's runs live, for runtime wiring.
func (h *Handler) PipelinesNamespace() string { return h.svc.PipelinesNamespace() }
