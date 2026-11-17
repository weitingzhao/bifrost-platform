package server

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/subscribe"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// busRunDetail answers a subscription row's params.detail
// (task-result:<task>/<result>) from the TaskRun of that task in the event's
// run: one list by label, only for a row that asks, only when its event
// matched (a refused landing, a few a day).
func (s *Server) busRunDetail() subscribe.Detail {
	if s.delivery == nil {
		return nil
	}
	return func(ctx context.Context, row registry.Subscription, ev subscribe.Event) (string, error) {
		task, result, ok := registry.TaskResult(row.Params[registry.ParamDetail])
		run := subscribe.RunOf(ev)
		if !ok || run == "" {
			return "", nil
		}
		trs, err := s.delivery.UnitTaskRuns(kubemetrics.WithSite(ctx, "bus.detail"), "",
			"tekton.dev/pipelineRun="+run+",tekton.dev/pipelineTask="+task)
		if err != nil {
			return "", err
		}
		return taskRunResult(trs, result), nil
	}
}

// taskRunResult is the named result of the first TaskRun that wrote it.
func taskRunResult(trs []unstructured.Unstructured, name string) string {
	for _, tr := range trs {
		results, _, _ := unstructured.NestedSlice(tr.Object, "status", "results")
		for _, r := range results {
			m, _ := r.(map[string]any)
			if m["name"] != name {
				continue
			}
			switch v := m["value"].(type) {
			case string:
				return v
			case nil:
			default:
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}
