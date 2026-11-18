package delivery

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// HandlePipelineTimings answers Work > CI's timings of one pipeline's
// retained runs. The read, the answer and its short cache are cirunview's
// observation; this service supplies its cluster, its list of a pipeline's
// runs and the parameters that may be shown.
func (h *Handler) HandlePipelineTimings(w http.ResponseWriter, r *http.Request) {
	h.timings.Serve(w, r, chi.URLParam(r, "name"), h.svc.timingsSource())
}

func (s *Service) timingsSource() cirunview.TimingsSource {
	return cirunview.TimingsSource{Client: s.buildDynamicClient, RunsOf: s.PipelineRunsOf, Params: timingParams,
		Tasks: func(runs []string, list func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error) {
			return s.tasksOf(routeTimings, s.PipelinesNamespace(), runs, list)
		}}
}

func timingParams(obj unstructured.Unstructured) map[string]string {
	out := map[string]string{}
	items, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
	for _, item := range items {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		// Only references used by this view, never arbitrary parameters.
		switch name {
		case "revision", "repo", "base", "branch", "uiRevision":
			if value, ok := m["value"].(string); ok {
				out[name] = value
			}
		}
	}
	return out
}
