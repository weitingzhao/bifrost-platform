package delivery

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

func tektonTaskRunState(obj unstructured.Unstructured) string { return cirunview.TaskRunState(obj) }

func pipelineTaskFromTaskRun(obj unstructured.Unstructured, runName string) string {
	return cirunview.PipelineTask(obj, runName)
}

// PipelineRunSteps answers what one run did, read from this service's
// cluster; the read and the projection are cirunview's.
func (s *Service) PipelineRunSteps(ctx context.Context, namespace, runName string) PipelineRunStepsResponse {
	ns := namespace
	if ns == "" {
		ns = s.PipelinesNamespace()
	}
	return cirunview.ReadRunSteps(ctx, s.buildDynamicClient, s.clusterID(), ns, runName)
}
