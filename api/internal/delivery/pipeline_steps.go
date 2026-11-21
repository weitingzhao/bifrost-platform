package delivery

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// tektonTaskRunState is verification's reading of a TaskRun's state;
// "unknown" when it is unbound (never "failed").
func tektonTaskRunState(obj unstructured.Unstructured) string {
	if f := ciRuns().TaskRunState; f != nil {
		return f(obj)
	}
	return "unknown"
}

// pipelineTaskFromTaskRun is the pipeline task a TaskRun ran for; "" when
// the reading is unbound.
func pipelineTaskFromTaskRun(obj unstructured.Unstructured, runName string) string {
	if f := ciRuns().PipelineTask; f != nil {
		return f(obj, runName)
	}
	return ""
}

// PipelineRunSteps answers what one run did, read from this service's
// cluster; the read and the projection are verification's (cirunview).
// Nil when that reading is unbound (the route answers 503).
func (s *Service) PipelineRunSteps(ctx context.Context, namespace, runName string) PipelineRunStepsResponse {
	read := ciRuns().ReadRunSteps
	if read == nil {
		return nil
	}
	ns := namespace
	if ns == "" {
		ns = s.PipelinesNamespace()
	}
	tasks := func(list func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error) {
		return s.tasksOf(routeRunSteps, ns, []string{runName}, list)
	}
	return read(ctx, s.buildDynamicClient, s.clusterID(), ns, runName, tasks)
}
