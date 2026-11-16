package delivery

// Observations for Work > CI, never evidence used by a gate. Times and results
// come from the retained Tekton objects, not the current pipeline definition.
import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

type timingStep = cirunview.TimingStep
type timingTask = cirunview.TimingTask
type timingRun struct {
	PipelineRunView
	Created string              `json:"created_at"`
	Trigger string              `json:"trigger,omitempty"`
	Params  map[string]string   `json:"params"`
	Results []PipelineRunResult `json:"results"`
	Tasks   []timingTask        `json:"tasks"`
	Skipped []string            `json:"skipped"`
}
type pipelineTimings struct {
	AsOf     time.Time   `json:"as_of"`
	Retained int         `json:"retained"`
	Limit    int         `json:"limit"`
	Runs     []timingRun `json:"runs"`
}
type timingsKept struct {
	at    time.Time
	value pipelineTimings
}
type pipelineTimingsCache struct {
	mu     sync.Mutex
	values map[string]timingsKept
}

// A short cache coalesces simultaneous readers. No background work, persisted
// history, log fan-out, or unfiltered TaskRun list (incident #63, 2026-11-11).
func (h *Handler) HandlePipelineTimings(w http.ResponseWriter, r *http.Request) {
	pipeline, repo, run := chi.URLParam(r, "name"), r.URL.Query().Get("repo"), r.URL.Query().Get("run")
	for _, name := range []string{pipeline, repo, run} {
		if name != "" && len(validation.IsDNS1123Subdomain(name)) != 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline, repo and run must be DNS names"})
			return
		}
	}
	limit := 60
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			writeJSON(w, 400, map[string]string{"error": "limit is 1 to 100"})
			return
		}
		limit = n
	}
	key := pipeline + "/" + repo + "/" + run + "/" + strconv.Itoa(limit)
	h.timings.mu.Lock()
	defer h.timings.mu.Unlock()
	if kept, ok := h.timings.values[key]; ok && time.Since(kept.at) < 30*time.Second {
		writeJSON(w, 200, kept.value)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	out, err := h.svc.readPipelineTimings(ctx, pipeline, repo, run, limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if len(h.timings.values) >= 32 || h.timings.values == nil {
		h.timings.values = map[string]timingsKept{}
	}
	h.timings.values[key] = timingsKept{time.Now(), out}
	writeJSON(w, 200, out)
}

func timingString(obj unstructured.Unstructured, path ...string) string {
	return cirunview.TimingString(obj, path...)
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
func taskTiming(obj unstructured.Unstructured, run string) timingTask {
	return cirunview.TaskTiming(obj, run)
}

func (s *Service) readPipelineTimings(ctx context.Context, pipeline, repo, run string, limit int) (pipelineTimings, error) {
	out := pipelineTimings{AsOf: time.Now().UTC(), Limit: limit, Runs: []timingRun{}}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return out, err
	}
	var items []unstructured.Unstructured
	if run != "" {
		item, getErr := dyn.Resource(pipelineRunGVR).Namespace("cicd").Get(ctx, run, metav1.GetOptions{})
		if getErr != nil {
			return out, fmt.Errorf("read run %s: %w", run, getErr)
		}
		items = []unstructured.Unstructured{*item}
	} else {
		items, err = s.PipelineRunsOf(ctx, pipeline)
		if err != nil {
			return out, err
		}
	}
	for _, item := range items {
		params := timingParams(item)
		actual := timingString(item, "spec", "pipelineRef", "name")
		if actual == "" {
			actual = item.GetLabels()["tekton.dev/pipeline"]
		}
		if actual != pipeline || repo != "" && item.GetLabels()["bifrost.io/repo"] != repo && params["repo"] != repo {
			continue
		}
		skipped := pipelineRunSkippedTasks(&item)
		if skipped == nil {
			skipped = []string{}
		}
		out.Runs = append(out.Runs, timingRun{PipelineRunView: pipelineRunFromUnstructured(item, pipeline),
			Created: item.GetCreationTimestamp().UTC().Format(time.RFC3339), Trigger: item.GetLabels()["bifrost.io/trigger"],
			Params: params, Results: pipelineRunResults(&item), Tasks: []timingTask{}, Skipped: skipped})
	}
	sort.Slice(out.Runs, func(i, j int) bool { return out.Runs[i].Created > out.Runs[j].Created })
	out.Retained = len(out.Runs)
	if len(out.Runs) > limit {
		out.Runs = out.Runs[:limit]
	}
	index := map[string]*timingRun{}
	for i := range out.Runs {
		index[out.Runs[i].Name] = &out.Runs[i]
	}
	for start := 0; start < len(out.Runs); start += 20 {
		end := min(start+20, len(out.Runs))
		names := []string{}
		for _, item := range out.Runs[start:end] {
			names = append(names, item.Name)
		}
		options := metav1.ListOptions{Limit: 100, LabelSelector: "tekton.dev/pipelineRun in (" + strings.Join(names, ",") + ")"}
		seen := map[string]bool{}
		for {
			page, listErr := dyn.Resource(taskRunGVR).Namespace("cicd").List(ctx, options)
			if listErr != nil {
				return out, fmt.Errorf("read task timings: %w", listErr)
			}
			for _, task := range page.Items {
				if parent := index[task.GetLabels()["tekton.dev/pipelineRun"]]; parent != nil {
					parent.Tasks = append(parent.Tasks, taskTiming(task, parent.Name))
				}
			}
			options.Continue = page.GetContinue()
			if options.Continue == "" {
				break
			}
			if seen[options.Continue] {
				return out, fmt.Errorf("task timing pagination repeated a cursor")
			}
			seen[options.Continue] = true
		}
	}
	for i := range out.Runs {
		sort.Slice(out.Runs[i].Tasks, func(a, b int) bool { return out.Runs[i].Tasks[a].Created < out.Runs[i].Tasks[b].Created })
	}
	return out, nil
}
