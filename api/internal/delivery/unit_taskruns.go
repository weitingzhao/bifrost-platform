package delivery

import (
	"context"

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
	list, err := dyn.Resource(taskRunGVR).Namespace(s.PipelinesNamespace()).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
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
