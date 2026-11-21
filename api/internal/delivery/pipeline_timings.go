package delivery

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// HandlePipelineTimings answers Work > CI's timings of one pipeline's
// retained runs. The read, the answer and its short cache are verification's
// observation (cirunview, through CIRunReadings.NewTimings); this service
// supplies its cluster, its list of a pipeline's runs and the parameters that
// may be shown.
func (h *Handler) HandlePipelineTimings(w http.ResponseWriter, r *http.Request) {
	t := h.timingsReader()
	if t == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errTimingsUnbound})
		return
	}
	t.Serve(w, r, chi.URLParam(r, "name"), h.svc.timingsSource())
}

// timingsReader is the handler's timings reader, made on first use; nil
// while the reading is unbound.
func (h *Handler) timingsReader() TimingsReader {
	h.timingsMu.Lock()
	defer h.timingsMu.Unlock()
	if h.timings == nil {
		if mk := ciRuns().NewTimings; mk != nil {
			h.timings = mk()
		}
	}
	return h.timings
}

func (s *Service) timingsSource() TimingsSource {
	return TimingsSource{Client: s.buildDynamicClient, RunsOf: s.PipelineRunsOf, Params: timingParams,
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
