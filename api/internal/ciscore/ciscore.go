// Package ciscore scores how each repository uses CI, and CI as their
// provider, from runs that were already read. It reads nothing itself, starts
// nothing and decides nothing: a verdict here is a reading for the Ops page,
// not a gate.
//
// ISSUE-20 (2026-11-16): CI does not depend on the business; it sets the standard
// and scores every repository that uses it from measured data, and the
// repositories score CI in turn. Only what is measured is an item: each is
// pass, fail or unmeasured with the raw number, there is no weighted formula,
// and an overall verdict is the worst of its items. The pass lines are CI's
// and are published with the answer (Standards).
package ciscore

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// WindowDays is how far back a completed run counts. Two since 2026-11-17:
// the cluster keeps a run two days (infra k8s/cicd/tekton/pipelinerun-ttl.yaml,
// seven before; the 15,000 TaskRuns without a pod that stalled Tekton for 23
// minutes that night), and a window longer than what is kept would report
// two days of runs as three.
const WindowDays = 2

const (
	Pass       = "pass"
	Fail       = "fail"
	Unmeasured = "unmeasured"
)

// The pass lines. They are CI's to set; Standards publishes them.
const (
	landingSeconds    = 120
	landingSamples    = 5
	followsRatio      = 0.6
	followsSamples    = 3
	sidesSamples      = 5
	redPercent        = 10
	verdictSeconds    = 150
	startLagSeconds   = 15
	cleanupHours      = 2
	frontModule       = "console"
	scopeResult       = "api-scope"
	runModulesResult  = "run-modules"
	testedByResult    = "tested-by"
	landOutcomeResult = "outcome"
	// ScopeTask is the task whose run-modules result says which sides ran.
	ScopeTask = "changes"
)

// Run is one PipelineRun as the scorecard reads it.
type Run struct {
	Name      string
	Created   time.Time
	Started   time.Time
	Completed time.Time // zero while it runs
	Failed    bool      // of a completed run
	// Base is the run's base parameter: empty (or all zeros, git's "no
	// commit") on the run of a ready branch, which tests the change itself.
	Base string
	// Full says the run is a daily full run, which is not a landing.
	Full bool
	// Results are the run's results, without surrounding whitespace.
	Results map[string]string
}

// Task is one TaskRun of a run.
type Task struct {
	Run     string
	Task    string // the pipeline task's name
	Failed  bool
	Started time.Time // when its first step began to run
	Results map[string]string
}

// Declaration is what was read of a repository's bifrost.units.yaml.
type Declaration struct {
	Err      string // the read failed
	Declared bool
	// Invalid is why a declaration that is there does not parse.
	Invalid string
	// Units counts the modules it declares (the units of a commit are a
	// plan, which this does not compute).
	Units int
}

// Repo is the input of one repository.
type Repo struct {
	Repo     string
	Pipeline string
	Runs     []Run
	// Tasks are the first and the scope tasks of the runs (what start_lag
	// and sides_separated read); TasksErr says they could not be read.
	Tasks       []Task
	TasksErr    string
	Declaration Declaration
}

// Input is everything the scorecard is computed from.
type Input struct {
	Now   time.Time
	Repos []Repo
	// Land are the runs of the pipeline that merges.
	Land []Run
	// FailedTasks are all the tasks of the failed runs in the window.
	FailedTasks    []Task
	FailedTasksErr string
	// Cleanup is when the job that deletes old runs last succeeded.
	Cleanup    time.Time
	CleanupErr string
}

type Standard struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	PassWhen string `json:"pass_when"`
}

type Item struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Verdict string `json:"verdict"`
	Value   string `json:"value"`
	Samples int    `json:"samples"`
}

type RepoScore struct {
	Repo     string `json:"repo"`
	Pipeline string `json:"pipeline"`
	// Tier is selective (some runs test only what changed), full (every run
	// tests everything) or none (no run in the window).
	Tier    string `json:"tier"`
	Overall string `json:"overall"`
	Items   []Item `json:"items"`
}

type Provider struct {
	Overall string `json:"overall"`
	Items   []Item `json:"items"`
}

type Scorecard struct {
	GeneratedAt string      `json:"generated_at"`
	WindowDays  int         `json:"window_days"`
	Standards   []Standard  `json:"standards"`
	Repos       []RepoScore `json:"repos"`
	Provider    Provider    `json:"provider"`
}

var standards = []Standard{
	{"landing_time", "Landing time", fmt.Sprintf("median of the runs that tested a ready branch themselves is at most %d s (at least %d runs)", landingSeconds, landingSamples)},
	{"follows_change", "Time follows the change", fmt.Sprintf("median of incremental runs is at most %.1f of the median of full runs (at least %d of each)", followsRatio, followsSamples)},
	{"declares_units", "Declares its units", "main has a bifrost.units.yaml that parses and declares at least one module"},
	{"sides_separated", "Sides tested apart", fmt.Sprintf("at least one of at least %d runs left %s out", sidesSamples, frontModule)},
	{"red_ratio", "Red runs", fmt.Sprintf("at most %d%% of completed runs failed", redPercent)},
	{"push_to_verdict", "Push to verdict", fmt.Sprintf("median of the merges that landed is at most %d s", verdictSeconds)},
	{"start_lag", "Start lag", fmt.Sprintf("median from a run's creation to the first step of its first task running is at most %d s", startLagSeconds)},
	{"own_failures", "CI's own failures", "no failed run failed in one of CI's own tasks (clone, reuse, changes, resolve, freeze-check)"},
	{"cleanup_keeps_up", "Cleanup keeps up", fmt.Sprintf("the job that deletes old runs last succeeded at most %d h ago", cleanupHours)},
}

// Standards are the pass lines of every item, repository items first.
func Standards() []Standard { return append([]Standard{}, standards...) }

func label(id string) string {
	for _, s := range standards {
		if s.ID == id {
			return s.Label
		}
	}
	return id
}

func item(id, verdict, value string, samples int) Item {
	return Item{ID: id, Label: label(id), Verdict: verdict, Value: value, Samples: samples}
}

func passIf(ok bool) string {
	if ok {
		return Pass
	}
	return Fail
}

// Worst is the overall verdict: fail when any item fails, otherwise
// unmeasured when any is unmeasured, otherwise pass.
func Worst(items []Item) string {
	out := Pass
	for _, it := range items {
		switch it.Verdict {
		case Fail:
			return Fail
		case Pass:
		default:
			out = Unmeasured
		}
	}
	return out
}

// InWindow keeps the runs that completed in the last WindowDays.
func InWindow(runs []Run, now time.Time) []Run {
	since := now.Add(-WindowDays * 24 * time.Hour)
	var out []Run
	for _, r := range runs {
		if !r.Completed.IsZero() && !r.Completed.Before(since) {
			out = append(out, r)
		}
	}
	return out
}

// FailedInWindow names the failed runs InWindow keeps: the runs whose tasks
// own_failures needs.
func FailedInWindow(runs []Run, now time.Time) []string {
	var out []string
	for _, r := range InWindow(runs, now) {
		if r.Failed {
			out = append(out, r.Name)
		}
	}
	return out
}

func noBase(base string) bool { return strings.Trim(base, "0") == "" }

// tested says the run tested a ready branch itself: it has no base, it
// reused no other run's verdict, and it is not the daily full run.
func tested(r Run) bool { return noBase(r.Base) && r.Results[testedByResult] == "" && !r.Full }

func seconds(r Run) (float64, bool) {
	if r.Started.IsZero() || r.Completed.IsZero() {
		return 0, false
	}
	return r.Completed.Sub(r.Started).Seconds(), true
}

func median(values []float64) float64 {
	sorted := append([]float64{}, values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func durations(runs []Run, keep func(Run) bool) []float64 {
	var out []float64
	for _, r := range runs {
		if !keep(r) {
			continue
		}
		if s, ok := seconds(r); ok {
			out = append(out, s)
		}
	}
	return out
}

// OwnTask says a pipeline task is CI's own plumbing, not a test or a check of
// the repository.
func OwnTask(task string) bool {
	switch task {
	case "reuse", "changes", "resolve", "freeze-check":
		return true
	}
	return strings.HasPrefix(task, "clone")
}

func scoreRepo(in Repo, now time.Time) RepoScore {
	done := InWindow(in.Runs, now)
	out := RepoScore{Repo: in.Repo, Pipeline: in.Pipeline, Tier: "none"}
	real := map[string]bool{}
	for _, r := range done {
		if tested(r) {
			real[r.Name] = true
		}
		if out.Tier == "none" {
			out.Tier = "full"
		}
		if r.Results[scopeResult] == "incremental" {
			out.Tier = "selective"
		}
	}

	// landing_time
	all := durations(done, tested)
	if len(all) < landingSamples {
		out.Items = append(out.Items, item("landing_time", Unmeasured, fmt.Sprintf("%d runs, needs %d", len(all), landingSamples), len(all)))
	} else {
		m := median(all)
		out.Items = append(out.Items, item("landing_time", passIf(m <= landingSeconds), fmt.Sprintf("median %.0f s", m), len(all)))
	}

	// follows_change
	reports := false
	for _, r := range done {
		if _, ok := r.Results[scopeResult]; ok && tested(r) {
			reports = true
		}
	}
	inc := durations(done, func(r Run) bool { return tested(r) && r.Results[scopeResult] == "incremental" })
	full := durations(done, func(r Run) bool { return tested(r) && r.Results[scopeResult] == "full" })
	switch {
	case !reports:
		out.Items = append(out.Items, item("follows_change", Unmeasured, "the pipeline reports no "+scopeResult, 0))
	case len(inc) < followsSamples || len(full) < followsSamples || median(full) <= 0:
		out.Items = append(out.Items, item("follows_change", Unmeasured,
			fmt.Sprintf("%d incremental and %d full runs, needs %d of each", len(inc), len(full), followsSamples), len(inc)+len(full)))
	default:
		ratio := median(inc) / median(full)
		out.Items = append(out.Items, item("follows_change", passIf(ratio <= followsRatio),
			fmt.Sprintf("%.2f (incremental %.0f s / full %.0f s)", ratio, median(inc), median(full)), len(inc)+len(full)))
	}

	// declares_units
	switch d := in.Declaration; {
	case d.Err != "":
		out.Items = append(out.Items, item("declares_units", Unmeasured, "not read: "+d.Err, 0))
	case !d.Declared:
		out.Items = append(out.Items, item("declares_units", Fail, "no bifrost.units.yaml", 0))
	case d.Invalid != "":
		out.Items = append(out.Items, item("declares_units", Fail, "bifrost.units.yaml does not parse: "+d.Invalid, 0))
	case d.Units == 0:
		out.Items = append(out.Items, item("declares_units", Fail, "bifrost.units.yaml declares 0 modules", 0))
	default:
		out.Items = append(out.Items, item("declares_units", Pass, fmt.Sprintf("%d modules", d.Units), d.Units))
	}

	// sides_separated
	asked, skipped := 0, 0
	for _, t := range in.Tasks {
		value, ok := t.Results[runModulesResult]
		if t.Task != ScopeTask || !real[t.Run] || !ok {
			continue
		}
		asked++
		if !contains(strings.Fields(value), "*") && !contains(strings.Fields(value), frontModule) {
			skipped++
		}
	}
	sides := fmt.Sprintf("%s skipped in %d of %d runs", frontModule, skipped, asked)
	switch {
	case in.TasksErr != "":
		out.Items = append(out.Items, item("sides_separated", Unmeasured, "tasks not read: "+in.TasksErr, 0))
	case asked == 0:
		out.Items = append(out.Items, item("sides_separated", Unmeasured, "no "+ScopeTask+" task reports "+runModulesResult, 0))
	case asked < sidesSamples:
		out.Items = append(out.Items, item("sides_separated", Unmeasured, fmt.Sprintf("%s, needs %d runs", sides, sidesSamples), asked))
	default:
		out.Items = append(out.Items, item("sides_separated", passIf(skipped >= 1), sides, asked))
	}

	// red_ratio
	failed := 0
	for _, r := range done {
		if r.Failed {
			failed++
		}
	}
	if len(done) == 0 {
		out.Items = append(out.Items, item("red_ratio", Unmeasured, "no completed run", 0))
	} else {
		out.Items = append(out.Items, item("red_ratio", passIf(failed*100 <= redPercent*len(done)), fmt.Sprintf("%d failed of %d", failed, len(done)), len(done)))
	}
	out.Overall = Worst(out.Items)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func scoreProvider(in Input) Provider {
	var out Provider

	// push_to_verdict
	landed := durations(InWindow(in.Land, in.Now), func(r Run) bool { return r.Results[landOutcomeResult] == "landed" })
	if len(landed) == 0 {
		out.Items = append(out.Items, item("push_to_verdict", Unmeasured, "no merge landed", 0))
	} else {
		m := median(landed)
		out.Items = append(out.Items, item("push_to_verdict", passIf(m <= verdictSeconds), fmt.Sprintf("median %.0f s", m), len(landed)))
	}

	// start_lag: creation of the run to the earliest start among its tasks read.
	var lags []float64
	tasksErr := ""
	for _, repo := range in.Repos {
		if repo.TasksErr != "" {
			tasksErr = repo.TasksErr
		}
		first := map[string]time.Time{}
		for _, t := range repo.Tasks {
			if !t.Started.IsZero() && (first[t.Run].IsZero() || t.Started.Before(first[t.Run])) {
				first[t.Run] = t.Started
			}
		}
		for _, r := range InWindow(repo.Runs, in.Now) {
			if at := first[r.Name]; !at.IsZero() && !r.Created.IsZero() {
				lags = append(lags, at.Sub(r.Created).Seconds())
			}
		}
	}
	switch {
	case tasksErr != "":
		out.Items = append(out.Items, item("start_lag", Unmeasured, "tasks not read: "+tasksErr, 0))
	case len(lags) == 0:
		out.Items = append(out.Items, item("start_lag", Unmeasured, "no run with a started first task", 0))
	default:
		m := median(lags)
		out.Items = append(out.Items, item("start_lag", passIf(m <= startLagSeconds), fmt.Sprintf("median %.0f s", m), len(lags)))
	}

	// own_failures
	var failed []string
	for _, repo := range in.Repos {
		failed = append(failed, FailedInWindow(repo.Runs, in.Now)...)
	}
	failed = append(failed, FailedInWindow(in.Land, in.Now)...)
	if in.FailedTasksErr != "" {
		out.Items = append(out.Items, item("own_failures", Unmeasured, "tasks of the failed runs not read: "+in.FailedTasksErr, len(failed)))
	} else {
		anyFailed, own := map[string]bool{}, map[string]bool{}
		for _, t := range in.FailedTasks {
			if t.Failed {
				anyFailed[t.Run] = true
				if OwnTask(t.Task) {
					own[t.Run] = true
				}
			}
		}
		k := 0
		for _, name := range failed {
			// A run that failed with no failed task did not get to a test: that is CI's too.
			if own[name] || !anyFailed[name] {
				k++
			}
		}
		out.Items = append(out.Items, item("own_failures", passIf(k == 0), fmt.Sprintf("%d of %d failed runs failed in CI's own tasks", k, len(failed)), len(failed)))
	}

	// cleanup_keeps_up
	switch {
	case in.CleanupErr != "":
		out.Items = append(out.Items, item("cleanup_keeps_up", Unmeasured, "not read: "+in.CleanupErr, 0))
	case in.Cleanup.IsZero():
		out.Items = append(out.Items, item("cleanup_keeps_up", Unmeasured, "the cleanup job reports no successful run", 0))
	default:
		hours := in.Now.Sub(in.Cleanup).Hours()
		out.Items = append(out.Items, item("cleanup_keeps_up", passIf(hours <= cleanupHours), fmt.Sprintf("%.1f h since it last succeeded", hours), 1))
	}
	out.Overall = Worst(out.Items)
	return out
}

// Score computes the scorecard of the input.
func Score(in Input) Scorecard {
	out := Scorecard{GeneratedAt: in.Now.UTC().Format(time.RFC3339), WindowDays: WindowDays, Standards: Standards(), Repos: []RepoScore{}}
	for _, repo := range in.Repos {
		out.Repos = append(out.Repos, scoreRepo(repo, in.Now))
	}
	out.Provider = scoreProvider(in)
	return out
}
