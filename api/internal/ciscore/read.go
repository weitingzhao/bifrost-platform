package ciscore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

// GET /api/v1/ci/scorecard is how each repository uses CI, and how CI serves
// them, scored from the runs the cluster still keeps (ISSUE-20; the
// items and their pass lines are ciscore's). Read-only; viewer and above.
//
// It is computed only when asked and the answer is kept for scorecardKeep.
// The runs are read one pipeline at a time by Tekton's pipeline label, slim
// (delivery.PipelineRunsOf), and of the TaskRuns only the first and the scope
// task of each CI run plus the tasks of the failed runs: on 2026-11-14 a loop
// that read every run each round took the workers past their memory limit.

// Ten minutes: one read is about 1,300 runs and 1,800 TaskRuns (some 12 MB of
// JSON), and a two-day window does not move faster than that. Incident #63
// (2026-11-11): a Console page that made the API list the cluster on every
// poll starved PROD's platform.
const scorecardKeep = 10 * time.Minute

// Repos are the repositories of the thin version, with the pipeline
// that tests each; landPipeline merges for both.
var Repos = []struct{ Repo, Pipeline string }{
	{"bifrost-platform", "bifrost-ci-platform"},
	{"bifrost-trade-infra", "bifrost-ci-infra"},
}

const (
	landPipeline = "bifrost-land"
	firstTask    = "clone"
	cleanupJob   = "tekton-pipelinerun-ttl"
)

// LastCleanup is Source.Cleanup on a cluster client: when the CronJob that
// deletes finished runs last succeeded; zero when it never has.
func LastCleanup(ctx context.Context, client kubernetes.Interface, namespace string) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	job, err := client.BatchV1().CronJobs(namespace).Get(ctx, cleanupJob, metav1.GetOptions{})
	if err != nil || job.Status.LastSuccessfulTime == nil {
		return time.Time{}, err
	}
	return job.Status.LastSuccessfulTime.Time, nil
}

// Source is what the scorecard reads.
type Source struct {
	Runs        func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error)
	Tasks       func(ctx context.Context, selector string) ([]unstructured.Unstructured, error)
	Declaration func(ctx context.Context, repo string) Declaration
	// Cleanup is when the cleanup CronJob last succeeded; zero when it never did.
	Cleanup func(ctx context.Context) (time.Time, error)
}

func timeOf(obj unstructured.Unstructured, field string) time.Time {
	t, err := time.Parse(time.RFC3339, cirunview.TimingString(obj, "status", field))
	if err != nil {
		return time.Time{}
	}
	return t
}

func resultsOf(obj unstructured.Unstructured) map[string]string {
	out := map[string]string{}
	for _, r := range cirunview.Results(&obj) {
		out[r.Name] = strings.TrimSpace(r.Value)
	}
	return out
}

func runsOf(items []unstructured.Unstructured) []Run {
	out := make([]Run, 0, len(items))
	for _, item := range items {
		run := Run{Name: item.GetName(), Created: item.GetCreationTimestamp().Time, Started: timeOf(item, "startTime"),
			Completed: timeOf(item, "completionTime"), Failed: cirunview.TaskRunState(item) == "failed",
			Full: item.GetLabels()["bifrost.io/trigger"] == landingci.FullCITrigger, Results: resultsOf(item)}
		params, _, _ := unstructured.NestedSlice(item.Object, "spec", "params")
		for _, p := range params {
			if m, _ := p.(map[string]any); m["name"] == "base" {
				run.Base, _ = m["value"].(string)
			}
		}
		out = append(out, run)
	}
	return out
}

func tasksOf(items []unstructured.Unstructured) []Task {
	out := make([]Task, 0, len(items))
	for _, item := range items {
		labels := item.GetLabels()
		out = append(out, Task{Run: labels["tekton.dev/pipelineRun"], Task: labels["tekton.dev/pipelineTask"],
			Failed: cirunview.TaskRunState(item) == "failed", Started: stepStarted(item), Results: resultsOf(item)})
	}
	return out
}

// stepStarted is when the first of a TaskRun's steps began to run, zero when
// none has. status.startTime is not it: the controller sets that the moment it
// takes the TaskRun, before the pod is scheduled and its images are there, so
// from the run's creation it is 0 s every time (2026-11-16, 573 runs) while a
// step starts a median of 9 s later.
func stepStarted(obj unstructured.Unstructured) time.Time {
	steps, _, _ := unstructured.NestedSlice(obj.Object, "status", "steps")
	var first time.Time
	for _, st := range steps {
		m, _ := st.(map[string]any)
		for _, state := range []string{"terminated", "running"} {
			at, _, _ := unstructured.NestedString(m, state, "startedAt")
			if t, err := time.Parse(time.RFC3339, at); err == nil && (first.IsZero() || t.Before(first)) {
				first = t
			}
		}
	}
	return first
}

// ReadScorecard reads what the scorecard needs and scores it. A failed
// read of a pipeline's runs fails the whole answer; a failed read of
// anything else leaves the items that need it unmeasured, with the reason.
func ReadScorecard(ctx context.Context, src Source, now time.Time) (Scorecard, error) {
	in := Input{Now: now}
	var failed []string
	for _, r := range Repos {
		items, err := src.Runs(ctx, r.Pipeline)
		if err != nil {
			return Scorecard{}, fmt.Errorf("runs of %s: %w", r.Pipeline, err)
		}
		repo := Repo{Repo: r.Repo, Pipeline: r.Pipeline, Runs: runsOf(items)}
		tasks, err := src.Tasks(ctx, "tekton.dev/pipeline="+r.Pipeline+",tekton.dev/pipelineTask in ("+firstTask+","+ScopeTask+")")
		if err != nil {
			repo.TasksErr = err.Error()
		}
		repo.Tasks = tasksOf(tasks)
		repo.Declaration = src.Declaration(ctx, r.Repo)
		failed = append(failed, FailedInWindow(repo.Runs, now)...)
		in.Repos = append(in.Repos, repo)
	}
	land, err := src.Runs(ctx, landPipeline)
	if err != nil {
		return Scorecard{}, fmt.Errorf("runs of %s: %w", landPipeline, err)
	}
	in.Land = runsOf(land)
	failed = append(failed, FailedInWindow(in.Land, now)...)
	// The tasks of the failed runs, twenty runs to a selector.
	for start := 0; start < len(failed) && in.FailedTasksErr == ""; start += 20 {
		tasks, err := src.Tasks(ctx, "tekton.dev/pipelineRun in ("+strings.Join(failed[start:min(start+20, len(failed))], ",")+")")
		if err != nil {
			in.FailedTasksErr = err.Error()
		}
		in.FailedTasks = append(in.FailedTasks, tasksOf(tasks)...)
	}
	if in.Cleanup, err = src.Cleanup(ctx); err != nil {
		in.CleanupErr = err.Error()
	}
	return Score(in), nil
}

// Cache keeps the last answer, and each repository's declaration
// for as long as its main stays at the commit it was read at. The lock is
// held across a read, so requests that arrive together read once.
type Cache struct {
	mu       sync.Mutex
	at       time.Time
	answer   Scorecard
	declared map[string]declared
}

type declared struct {
	commit string
	answer Declaration
}

// Read answers from the kept answer, else reads and keeps one.
func (c *Cache) Read(ctx context.Context, src Source, now time.Time) (Scorecard, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && now.Sub(c.at) < scorecardKeep {
		return c.answer, nil
	}
	out, err := ReadScorecard(ctx, src, now)
	if err != nil {
		return Scorecard{}, err
	}
	c.at, c.answer = now, out
	return out, nil
}

// Declaration reads main's declaration of repo the way /units/config does.
// head names main's commit and tree reads a commit's tree.
func (c *Cache) Declaration(ctx context.Context, repo string, head func(context.Context, string) (string, error), tree func(context.Context, string, string) (*unitplan.Tree, error)) Declaration {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	h, err := head(ctx, repo)
	if err != nil {
		return Declaration{Err: err.Error()}
	}
	if kept, ok := c.declared[repo]; ok && kept.commit == h {
		return kept.answer
	}
	// The file and its modules, not the plan /units/config computes: on
	// 2026-11-16 the plan did not build within this limit on STG, the answer
	// came back declared with no module, and bifrost-platform was scored
	// "declares 0 units".
	t, err := tree(ctx, repo, h)
	if err != nil {
		return Declaration{Err: err.Error()}
	}
	out := Declaration{}
	if doc, err := t.ReadFile(unitplan.ManifestPath); err == nil {
		out.Declared = true
		if manifest, err := unitplan.ParseManifest(doc); err != nil {
			out.Invalid = err.Error()
		} else {
			out.Units = len(manifest.Modules)
		}
	}
	if c.declared == nil {
		c.declared = map[string]declared{}
	}
	c.declared[repo] = declared{commit: h, answer: out}
	return out
}

// Route is GET /api/v1/ci/scorecard. Source says, per request, what to read
// (ok false: this platform does not read the cluster).
type Route struct {
	Cache  *Cache
	Source func() (Source, bool)
}

func routeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (rt Route) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	src, ok := rt.Source()
	if !ok {
		routeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read the cluster"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	out, err := rt.Cache.Read(ctx, src, time.Now())
	if err != nil {
		routeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	routeJSON(w, http.StatusOK, out)
}
