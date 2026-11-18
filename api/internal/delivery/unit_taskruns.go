package delivery

import (
	"context"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// UnitTaskRuns lists the TaskRuns of unit tasks in the delivery namespace:
// the ones carrying the label, whatever its value (CI design v4, landing
// stage 2 block 2-C). selector narrows it further ("" for all of them).
//
// The unit plan's reads go through the unit cache once it has earned it
// (readThrough, 2026-11-18, agreed with the CI line): side by side with the
// list until the comparisons are clean, a sample after that.
func (s *Service) UnitTaskRuns(ctx context.Context, label, selector string) ([]unstructured.Unstructured, error) {
	list := func() ([]unstructured.Unstructured, error) {
		lctx, cancel := stgContext(ctx)
		defer cancel()
		dyn, err := s.buildDynamicClient()
		if err != nil {
			return nil, err
		}
		return cirunview.UnitTaskRuns(lctx, dyn, s.PipelinesNamespace(), label, selector)
	}
	plan, ok := unitexec.PlanOfSelector(label, selector)
	r := s.syncedCache()
	if !ok || r == nil {
		return list()
	}
	uc, ok := r.cache.(UnitTaskCache)
	if !ok || !uc.UnitTasksSynced() {
		return list()
	}
	return readThrough(s, routeUnitTasks, func(RunCache) []unstructured.Unstructured {
		return uc.UnitTaskObjects(plan)
	}, list, unitTaskKeys)
}

// UnitTaskCache is the part of the gate's cache that holds the unit TaskRuns.
type UnitTaskCache interface {
	UnitTasksSynced() bool
	// UnitTaskObjects is the unit TaskRuns of one plan, or of all ("").
	UnitTaskObjects(plan string) []unstructured.Unstructured
}

// unitTaskKeys is what the side-by-side compares, sorted by name.
func unitTaskKeys(items []unstructured.Unstructured) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, unitexec.CompareKey(items[i].GetName(), items[i].Object))
	}
	sort.Strings(out)
	return out
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
