package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/ciscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GET /api/v1/ci/scorecard is how each repository uses CI, and how CI serves
// them, scored from the runs the cluster still keeps (Owner 2026-11-16; the
// items and their pass lines are ciscore's). Read-only; viewer and above.
//
// It is computed only when asked and the answer is kept for ciScorecardKeep.
// The runs are read one pipeline at a time by Tekton's pipeline label, slim
// (delivery.PipelineRunsOf), and of the TaskRuns only the first and the scope
// task of each CI run plus the tasks of the failed runs: on 2026-11-14 a loop
// that read every run each round took the workers past their memory limit.

// Ten minutes: one read is about 1,300 runs and 1,800 TaskRuns (some 12 MB of
// JSON), and a two-day window does not move faster than that. Incident #63
// (2026-11-11): a Console page that made the API list the cluster on every
// poll starved PROD's platform.
const ciScorecardKeep = 10 * time.Minute

// ciScoreRepos are the repositories of the thin version, with the pipeline
// that tests each; ciLandPipeline merges for both.
var ciScoreRepos = []struct{ repo, pipeline string }{
	{"bifrost-platform", "bifrost-ci-platform"},
	{"bifrost-trade-infra", "bifrost-ci-infra"},
}

const (
	ciLandPipeline = "bifrost-land"
	ciFirstTask    = "clone"
	ciCleanupJob   = "tekton-pipelinerun-ttl"
)

// ciScoreSource is what the scorecard reads; the server's is ciScoreSourceOf.
type ciScoreSource struct {
	runs        func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error)
	tasks       func(ctx context.Context, selector string) ([]unstructured.Unstructured, error)
	declaration func(ctx context.Context, repo string) ciscore.Declaration
	// cleanup is when the cleanup CronJob last succeeded; zero when it never did.
	cleanup func(ctx context.Context) (time.Time, error)
}

func ciTime(obj unstructured.Unstructured, field string) time.Time {
	t, err := time.Parse(time.RFC3339, cirunview.TimingString(obj, "status", field))
	if err != nil {
		return time.Time{}
	}
	return t
}

func ciResults(obj unstructured.Unstructured) map[string]string {
	out := map[string]string{}
	for _, r := range cirunview.Results(&obj) {
		out[r.Name] = strings.TrimSpace(r.Value)
	}
	return out
}

func ciRuns(items []unstructured.Unstructured) []ciscore.Run {
	out := make([]ciscore.Run, 0, len(items))
	for _, item := range items {
		run := ciscore.Run{Name: item.GetName(), Created: item.GetCreationTimestamp().Time, Started: ciTime(item, "startTime"),
			Completed: ciTime(item, "completionTime"), Failed: cirunview.TaskRunState(item) == "failed",
			Full: item.GetLabels()["bifrost.io/trigger"] == landingci.FullCITrigger, Results: ciResults(item)}
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

func ciTasks(items []unstructured.Unstructured) []ciscore.Task {
	out := make([]ciscore.Task, 0, len(items))
	for _, item := range items {
		labels := item.GetLabels()
		out = append(out, ciscore.Task{Run: labels["tekton.dev/pipelineRun"], Task: labels["tekton.dev/pipelineTask"],
			Failed: cirunview.TaskRunState(item) == "failed", Started: ciStepStarted(item), Results: ciResults(item)})
	}
	return out
}

// ciStepStarted is when the first of a TaskRun's steps began to run, zero when
// none has. status.startTime is not it: the controller sets that the moment it
// takes the TaskRun, before the pod is scheduled and its images are there, so
// from the run's creation it is 0 s every time (2026-11-16, 573 runs) while a
// step starts a median of 9 s later.
func ciStepStarted(obj unstructured.Unstructured) time.Time {
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

// readCIScorecard reads what the scorecard needs and scores it. A failed
// read of a pipeline's runs fails the whole answer; a failed read of
// anything else leaves the items that need it unmeasured, with the reason.
func readCIScorecard(ctx context.Context, src ciScoreSource, now time.Time) (ciscore.Scorecard, error) {
	in := ciscore.Input{Now: now}
	var failed []string
	for _, r := range ciScoreRepos {
		items, err := src.runs(ctx, r.pipeline)
		if err != nil {
			return ciscore.Scorecard{}, fmt.Errorf("runs of %s: %w", r.pipeline, err)
		}
		repo := ciscore.Repo{Repo: r.repo, Pipeline: r.pipeline, Runs: ciRuns(items)}
		tasks, err := src.tasks(ctx, "tekton.dev/pipeline="+r.pipeline+",tekton.dev/pipelineTask in ("+ciFirstTask+","+ciscore.ScopeTask+")")
		if err != nil {
			repo.TasksErr = err.Error()
		}
		repo.Tasks = ciTasks(tasks)
		repo.Declaration = src.declaration(ctx, r.repo)
		failed = append(failed, ciscore.FailedInWindow(repo.Runs, now)...)
		in.Repos = append(in.Repos, repo)
	}
	land, err := src.runs(ctx, ciLandPipeline)
	if err != nil {
		return ciscore.Scorecard{}, fmt.Errorf("runs of %s: %w", ciLandPipeline, err)
	}
	in.Land = ciRuns(land)
	failed = append(failed, ciscore.FailedInWindow(in.Land, now)...)
	// The tasks of the failed runs, twenty runs to a selector.
	for start := 0; start < len(failed) && in.FailedTasksErr == ""; start += 20 {
		tasks, err := src.tasks(ctx, "tekton.dev/pipelineRun in ("+strings.Join(failed[start:min(start+20, len(failed))], ",")+")")
		if err != nil {
			in.FailedTasksErr = err.Error()
		}
		in.FailedTasks = append(in.FailedTasks, ciTasks(tasks)...)
	}
	if in.Cleanup, err = src.cleanup(ctx); err != nil {
		in.CleanupErr = err.Error()
	}
	return ciscore.Score(in), nil
}

// ciScorecardCache keeps the last answer, and each repository's declaration
// for as long as its main stays at the commit it was read at. The lock is
// held across a read, so requests that arrive together read once.
type ciScorecardCache struct {
	mu       sync.Mutex
	at       time.Time
	answer   ciscore.Scorecard
	declared map[string]ciDeclared
}

type ciDeclared struct {
	commit string
	answer ciscore.Declaration
}

func (c *ciScorecardCache) read(ctx context.Context, src ciScoreSource, now time.Time) (ciscore.Scorecard, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && now.Sub(c.at) < ciScorecardKeep {
		return c.answer, nil
	}
	out, err := readCIScorecard(ctx, src, now)
	if err != nil {
		return ciscore.Scorecard{}, err
	}
	c.at, c.answer = now, out
	return out, nil
}

func (s *Server) ciScoreSourceOf() ciScoreSource {
	return ciScoreSource{
		runs: s.delivery.PipelineRunsOf,
		tasks: func(ctx context.Context, selector string) ([]unstructured.Unstructured, error) {
			return s.delivery.UnitTaskRuns(ctx, "", selector)
		},
		declaration: s.ciDeclaration,
		cleanup: func(ctx context.Context) (time.Time, error) {
			client, _, err := s.cluster.Service().KubernetesClient()
			if err != nil {
				return time.Time{}, err
			}
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			job, err := client.BatchV1().CronJobs(s.delivery.PipelinesNamespace()).Get(ctx, ciCleanupJob, metav1.GetOptions{})
			if err != nil || job.Status.LastSuccessfulTime == nil {
				return time.Time{}, err
			}
			return job.Status.LastSuccessfulTime.Time, nil
		},
	}
}

// ciDeclaration reads main's declaration of repo the way /units/config does.
// The caller holds the cache's lock.
func (s *Server) ciDeclaration(ctx context.Context, repo string) ciscore.Declaration {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	head, err := s.mainHead(ctx, repo)
	if err != nil {
		return ciscore.Declaration{Err: err.Error()}
	}
	if kept, ok := s.ciScorecardKept.declared[repo]; ok && kept.commit == head {
		return kept.answer
	}
	// The file and its modules, not the plan /units/config computes: on
	// 2026-11-16 the plan did not build within this limit on STG, the answer
	// came back declared with no module, and bifrost-platform was scored
	// "declares 0 units".
	tree, err := s.unitTreesOf()(ctx, repo, head)
	if err != nil {
		return ciscore.Declaration{Err: err.Error()}
	}
	out := ciscore.Declaration{}
	if doc, err := tree.ReadFile(unitplan.ManifestPath); err == nil {
		out.Declared = true
		if manifest, err := unitplan.ParseManifest(doc); err != nil {
			out.Invalid = err.Error()
		} else {
			out.Units = len(manifest.Modules)
		}
	}
	if s.ciScorecardKept.declared == nil {
		s.ciScorecardKept.declared = map[string]ciDeclared{}
	}
	s.ciScorecardKept.declared[repo] = ciDeclared{commit: head, answer: out}
	return out
}

func (s *Server) handleCIScorecard(w http.ResponseWriter, r *http.Request) {
	if s.cluster == nil || s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read the cluster"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	out, err := s.ciScorecardKept.read(ctx, s.ciScoreSourceOf(), time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
