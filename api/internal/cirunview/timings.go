package cirunview

// Observations for Work > CI, never evidence used by a gate. Times and results
// come from the retained Tekton objects, not the current pipeline definition.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

type TimingRun struct {
	PipelineObservation
	Created string            `json:"created_at"`
	Trigger string            `json:"trigger,omitempty"`
	Params  map[string]string `json:"params"`
	Results []Result          `json:"results"`
	Tasks   []TimingTask      `json:"tasks"`
	Skipped []string          `json:"skipped"`
}
type PipelineTimings struct {
	AsOf     time.Time   `json:"as_of"`
	Retained int         `json:"retained"`
	Limit    int         `json:"limit"`
	Runs     []TimingRun `json:"runs"`
}

// TimingsSource is what the caller that owns the cluster gives the reader.
type TimingsSource struct {
	// Client is the caller's way to the cluster.
	Client func() (dynamic.Interface, error)
	// RunsOf lists the retained runs of one pipeline.
	RunsOf func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error)
	// Params picks which of a run's parameters are shown. Which ones is the
	// caller's exposure list, not this package's: nothing else of spec.params
	// reaches the answer.
	Params func(run unstructured.Unstructured) map[string]string
}

type timingsKept struct {
	at    time.Time
	value PipelineTimings
}

// TimingsCache is the short memory of the timings route. Its zero value is
// ready to use.
type TimingsCache struct {
	mu     sync.Mutex
	values map[string]timingsKept
}

func writeTimingsJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Serve answers the timings of one pipeline's retained runs; repo, run and
// limit come from the query.
//
// A short cache coalesces simultaneous readers. No background work, persisted
// history, log fan-out, or unfiltered TaskRun list (incident #63, 2026-11-11).
func (c *TimingsCache) Serve(w http.ResponseWriter, r *http.Request, pipeline string, src TimingsSource) {
	repo, run := r.URL.Query().Get("repo"), r.URL.Query().Get("run")
	for _, name := range []string{pipeline, repo, run} {
		if name != "" && len(validation.IsDNS1123Subdomain(name)) != 0 {
			writeTimingsJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline, repo and run must be DNS names"})
			return
		}
	}
	limit := 60
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			writeTimingsJSON(w, 400, map[string]string{"error": "limit is 1 to 100"})
			return
		}
		limit = n
	}
	key := pipeline + "/" + repo + "/" + run + "/" + strconv.Itoa(limit)
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.values[key]; ok && time.Since(kept.at) < 30*time.Second {
		writeTimingsJSON(w, 200, kept.value)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	out, err := ReadPipelineTimings(ctx, src, pipeline, repo, run, limit)
	if err != nil {
		writeTimingsJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if len(c.values) >= 32 || c.values == nil {
		c.values = map[string]timingsKept{}
	}
	c.values[key] = timingsKept{time.Now(), out}
	writeTimingsJSON(w, 200, out)
}

// ReadPipelineTimings reads the retained runs of one pipeline (or the one run
// named) and, for the newest limit of them, their TaskRuns' times and results.
func ReadPipelineTimings(ctx context.Context, src TimingsSource, pipeline, repo, run string, limit int) (PipelineTimings, error) {
	out := PipelineTimings{AsOf: time.Now().UTC(), Limit: limit, Runs: []TimingRun{}}
	dyn, err := src.Client()
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
		items, err = src.RunsOf(ctx, pipeline)
		if err != nil {
			return out, err
		}
	}
	for _, item := range items {
		params := src.Params(item)
		actual := TimingString(item, "spec", "pipelineRef", "name")
		if actual == "" {
			actual = item.GetLabels()["tekton.dev/pipeline"]
		}
		if actual != pipeline || repo != "" && item.GetLabels()["bifrost.io/repo"] != repo && params["repo"] != repo {
			continue
		}
		skipped := PipelineRunSkippedTasks(&item)
		if skipped == nil {
			skipped = []string{}
		}
		out.Runs = append(out.Runs, TimingRun{PipelineObservation: PipelineRunObservation(item, pipeline),
			Created: item.GetCreationTimestamp().UTC().Format(time.RFC3339), Trigger: item.GetLabels()["bifrost.io/trigger"],
			Params: params, Results: Results(&item), Tasks: []TimingTask{}, Skipped: skipped})
	}
	sort.Slice(out.Runs, func(i, j int) bool { return out.Runs[i].Created > out.Runs[j].Created })
	out.Retained = len(out.Runs)
	if len(out.Runs) > limit {
		out.Runs = out.Runs[:limit]
	}
	index := map[string]*TimingRun{}
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
					parent.Tasks = append(parent.Tasks, TaskTiming(task, parent.Name))
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
