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
// (task-result:<task>/<name>[,<name>…]) from the TaskRun of that task in the
// event's run: each name its result, else its param (a branch is a param).
// One list by label, only for a row that asks, only when its event matched.
func (s *Server) busRunDetail() subscribe.Detail {
	if s.delivery == nil {
		return nil
	}
	return func(ctx context.Context, row registry.Subscription, ev subscribe.Event) (map[string]string, error) {
		task, names, ok := registry.TaskResults(row.Params[registry.ParamDetail])
		run := subscribe.RunOf(ev)
		if !ok || run == "" {
			return nil, nil
		}
		trs, err := s.delivery.UnitTaskRuns(kubemetrics.WithSite(ctx, "bus.detail"), "",
			"tekton.dev/pipelineRun="+run+",tekton.dev/pipelineTask="+task)
		if err != nil {
			return nil, err
		}
		out := map[string]string{}
		for _, n := range names {
			if v := taskRunValue(trs, n); v != "" {
				out[n] = v
			}
		}
		return out, nil
	}
}

// taskRunValue is the named result of the first TaskRun that wrote it, else
// the named param of the first that has it.
func taskRunValue(trs []unstructured.Unstructured, name string) string {
	if v := taskRunField(trs, name, "status", "results"); v != "" {
		return v
	}
	return taskRunField(trs, name, "spec", "params")
}

func taskRunField(trs []unstructured.Unstructured, name string, path ...string) string {
	for _, tr := range trs {
		items, _, _ := unstructured.NestedSlice(tr.Object, path...)
		for _, it := range items {
			m, _ := it.(map[string]any)
			if m["name"] != name {
				continue
			}
			switch v := m["value"].(type) {
			case string:
				if v != "" {
					return v
				}
			case nil:
			default:
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}
