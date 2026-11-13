// Package headrelease releases the newest head of main by itself: when a
// deliverable's commits are green in the evidence ledger and are not what its
// environment runs, it starts the delivery once; a head that moves while a
// delivery runs is delivered by the next one.
//
// Why (2026-11-13, ci-feedback 1-3): each line released its own commit and
// held a release window to do it. A commit waited thirteen minutes for a
// window two other lines held; three STG deliveries in fourteen minutes failed
// because main moved under them; a coordinator had to arrange for one release
// to carry two lines' commits.
//
// What a deliverable is, is a table (deliverables.yaml beside this file): the
// platform knows repositories, pipelines and the ledger, not what is in them.
// The step decides from what it reads each time (main's heads, the ledger,
// the cluster's runs) and keeps nothing: a restart loses nothing.
package headrelease

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Commit is one of the commits a deliverable is made of.
type Commit struct {
	Repo  string `json:"repo"`
	Param string `json:"param"`
	// TestedWith names the repository whose CI ran beside this one; empty
	// when the commit is this repository's own main head.
	TestedWith string `json:"tested_with,omitempty"`
}

// Deliverable is one row of the table.
type Deliverable struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Environment string   `json:"environment"`
	Pipeline    string   `json:"pipeline"`
	Commits     []Commit `json:"commits"`
}

// Table is deliverables.yaml.
type Table struct {
	Deliverables []Deliverable `json:"deliverables"`
}

// The table ships in the binary. The Pod's config directory is a ConfigMap
// kept in another repository (infra k8s/components/platform-config), and a
// second copy there is one more thing to keep the same; a row is added here.
//
//go:embed deliverables.yaml
var shipped []byte

// LoadTable reads the table that ships. There is no other: a table read
// from a path would let a file outside review enable any row (Codex review
// 2026-11-13: ParseTable does not know which repositories are Trade's; only
// the test of the shipped table does).
func LoadTable() (Table, string, error) {
	const path = "deliverables.yaml (built in)"
	t, err := ParseTable(shipped)
	if err != nil {
		return Table{}, path, fmt.Errorf("%s: %w", path, err)
	}
	return t, path, nil
}

// ParseTable reads a table and checks every row, enabled or not.
func ParseTable(raw []byte) (Table, error) {
	var t Table
	if err := yaml.UnmarshalStrict(raw, &t); err != nil {
		return Table{}, err
	}
	names := map[string]bool{}
	for _, d := range t.Deliverables {
		if d.Name == "" || d.Pipeline == "" || d.Environment == "" || names[d.Name] {
			return Table{}, fmt.Errorf("deliverable %q: a name of its own, a pipeline and an environment are needed", d.Name)
		}
		names[d.Name] = true
		// The release policy alone releases to PROD. A PROD row may be
		// listed, never enabled: it is there to be read (WhereReleased).
		if d.Enabled && d.prod() {
			return Table{}, fmt.Errorf("deliverable %q: the head of main is not released to PROD from here", d.Name)
		}
		heads, repos, params := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, c := range d.Commits {
			if c.Repo == "" || c.Param == "" || repos[c.Repo] || params[c.Param] {
				return Table{}, fmt.Errorf("deliverable %q: each commit names a repository and a parameter, each once", d.Name)
			}
			repos[c.Repo], params[c.Param] = true, true
			if c.TestedWith == "" {
				heads[c.Repo] = true
			}
		}
		if len(heads) == 0 {
			return Table{}, fmt.Errorf("deliverable %q: at least one commit is a repository's own head", d.Name)
		}
		for _, c := range d.Commits {
			if c.TestedWith != "" && !heads[c.TestedWith] {
				return Table{}, fmt.Errorf("deliverable %q: %s is tested with %s, which is not one of its heads", d.Name, c.Repo, c.TestedWith)
			}
		}
	}
	return t, nil
}

// Run is a delivery run of a pipeline, as the cluster holds it.
type Run struct {
	Name   string
	Params map[string]string
	// Status is running, succeeded or failed.
	Status string
}

const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"

	CISucceeded = "Succeeded"
	CIFailed    = "Failed"
)

// World is what a step reads and the one thing it does.
type World interface {
	// Head is the commit refs/heads/main of the repository points at.
	Head(ctx context.Context, repo string) (string, error)
	// CI is the ledger's word on the commit's CI: Succeeded, Failed or ""
	// (no verdict yet); and, when it succeeded, the commit of each
	// repository that CI run used beside it.
	// run is the CI run the verdict rests on.
	CI(ctx context.Context, repo, commit string) (status, run string, beside map[string]string, err error)
	// Runs lists the pipeline's delivery runs, newest first.
	Runs(ctx context.Context, pipeline string) ([]Run, error)
	// Start starts the delivery with these parameters and returns the run.
	// wait is why it was not started and may be later: someone holds the
	// release window (it names the holder), or releases are frozen.
	Start(ctx context.Context, pipeline string, params map[string]string) (run string, wait string, err error)
}

// Outcome is what one step of one deliverable did, and why.
type Outcome struct {
	Deliverable string `json:"deliverable"`
	// Action is started, delivered (nothing to do), waiting, or refused
	// (this set of commits is not released: red, or it failed once).
	Action string `json:"action"`
	Why    string `json:"why"`
	// Commits is the set the step judged, by pipeline parameter.
	Commits map[string]string `json:"commits,omitempty"`
	Run     string            `json:"run,omitempty"`
	// Evidence is the CI run each head's verdict rests on, by repository.
	Evidence map[string]string `json:"evidence,omitempty"`
	// Previous is the newest successful delivery, what the environment ran
	// when the step judged: its run and its commits by parameter.
	PreviousRun string            `json:"previous_run,omitempty"`
	Previous    map[string]string `json:"previous,omitempty"`
}

const (
	Started   = "started"
	Delivered = "delivered"
	Waiting   = "waiting"
	Refused   = "refused"
)

// Step takes one deliverable one step: it starts at most one delivery.
//
// What it starts, it has judged twice. Between the first reading of main and
// the runs and the start, a person's delivery can begin and end (Codex review
// 2026-11-13, A2: reproduced with a paused step; the loop then released the
// older head over the newer one). So a step that would start reads everything
// again, and starts only when the second reading says the same.
func Step(ctx context.Context, w World, d Deliverable) (Outcome, error) {
	out, start, err := judge(ctx, w, d)
	if err != nil || !start {
		return out, err
	}
	again, still, err := judge(ctx, w, d)
	if err != nil || !still {
		return again, err
	}
	for _, c := range d.Commits {
		if c.TestedWith == "" && again.Commits[c.Param] != out.Commits[c.Param] {
			again.Action, again.Why = Waiting, "main moved while this step judged it"
			return again, nil
		}
	}
	out = again
	run, wait, err := w.Start(ctx, d.Pipeline, out.Commits)
	if err != nil {
		return out, fmt.Errorf("%s: starting %s: %w", d.Name, d.Pipeline, err)
	}
	if wait != "" {
		out.Action, out.Why = Waiting, wait
		return out, nil
	}
	out.Action, out.Run, out.Why = Started, run, "main's head is green and is not what the environment runs"
	return out, nil
}

// judge reads the world once and says what a step would do; start is true
// when that is to start a delivery of out.Commits.
func judge(ctx context.Context, w World, d Deliverable) (out Outcome, start bool, err error) {
	out, err = judged(ctx, w, d)
	return out, err == nil && out.Action == "", err
}

func judged(ctx context.Context, w World, d Deliverable) (Outcome, error) {
	out := Outcome{Deliverable: d.Name, Commits: map[string]string{}, Evidence: map[string]string{}}
	if !d.Enabled || d.prod() {
		out.Action, out.Why = Waiting, "the deliverable is not enabled"
		return out, nil
	}
	// The heads, each green; then what was tested beside them.
	beside := map[string]map[string]string{}
	for _, c := range d.Commits {
		if c.TestedWith != "" {
			continue
		}
		head, err := w.Head(ctx, c.Repo)
		if err != nil {
			return out, fmt.Errorf("%s: main of %s: %w", d.Name, c.Repo, err)
		}
		out.Commits[c.Param] = head
		status, ciRun, with, err := w.CI(ctx, c.Repo, head)
		if err != nil {
			return out, fmt.Errorf("%s: the ledger on %s %s: %w", d.Name, c.Repo, head, err)
		}
		if ciRun != "" {
			out.Evidence[c.Repo] = ciRun
		}
		switch status {
		case CISucceeded:
			beside[c.Repo] = with
		case CIFailed:
			out.Action, out.Why = Refused, fmt.Sprintf("the CI of %s %s failed", c.Repo, short(head))
			return out, nil
		default:
			out.Action, out.Why = Waiting, fmt.Sprintf("the CI of %s %s has no verdict yet", c.Repo, short(head))
			return out, nil
		}
	}
	for _, c := range d.Commits {
		if c.TestedWith == "" {
			continue
		}
		commit := beside[c.TestedWith][c.Repo]
		if commit == "" {
			out.Action, out.Why = Refused, fmt.Sprintf("the CI of %s does not say which %s it ran with", c.TestedWith, c.Repo)
			return out, nil
		}
		out.Commits[c.Param] = commit
	}

	runs, err := w.Runs(ctx, d.Pipeline)
	if err != nil {
		return out, fmt.Errorf("%s: the runs of %s: %w", d.Name, d.Pipeline, err)
	}
	// A delivery is of this set when it has the same heads. What was tested
	// beside a head does not make a new set: the ledger can hold several
	// passing CI rows of one commit, each beside another commit of a
	// sibling (PROD ledger 2026-11-13, bifrost-platform c53ea00d:
	// ci-platform-5d6bb and ci-platform-pgdsv ran with two infra commits),
	// and a head that did not move is not delivered again for that.
	same := func(r Run) bool {
		for _, c := range d.Commits {
			if c.TestedWith == "" && r.Params[c.Param] != out.Commits[c.Param] {
				return false
			}
		}
		return true
	}
	// One delivery at a time: a head that moved meanwhile is delivered by
	// the step after this run ends, so several moves make one delivery.
	for _, r := range runs {
		if r.Status == RunRunning {
			out.Action, out.Run = Waiting, r.Name
			out.Why = "a delivery is running"
			return out, nil
		}
	}
	// What the environment runs is the newest delivery that succeeded.
	for _, r := range runs {
		if r.Status == RunSucceeded {
			out.PreviousRun, out.Previous = r.Name, r.Params
			if same(r) {
				out.Action, out.Run, out.Why = Delivered, r.Name, "the newest successful delivery is of these commits"
				return out, nil
			}
			break
		}
	}
	// A set that failed is not tried again by itself: a broken head must not
	// fill the cluster with deliveries. A new head, or a person, moves it.
	//
	// A set that was delivered and then replaced is not delivered again
	// either: the newest successful delivery is of other commits although
	// these succeeded before it, so a person released something older over
	// them. That rollback stands until main moves; it needs no switch
	// (Codex review 2026-11-13, A3: the loop released main's head over a
	// rollback on its next pass).
	for _, r := range runs {
		if same(r) {
			switch r.Status {
			case RunFailed:
				out.Action, out.Run, out.Why = Refused, r.Name, "the last delivery of these commits failed; it is not tried again by itself"
				return out, nil
			case RunSucceeded:
				out.Action, out.Run = Refused, r.Name
				out.Why = fmt.Sprintf("these commits were delivered (%s) and then replaced by %s: a rollback by hand stands until main moves", r.Name, out.PreviousRun)
				return out, nil
			}
			break
		}
	}
	return out, nil // Action "": start
}

// Released says whether a commit of a repository is in what a deliverable's
// newest successful delivery shipped: contains reports whether the delivered
// commit of that repository has the asked commit in its history.
type Released struct {
	Deliverable string `json:"deliverable"`
	Environment string `json:"environment"`
	// Commit is what the environment runs of the asked repository; Run the
	// delivery that put it there. Both empty: nothing was delivered yet, or
	// the deliverable has no commit of that repository.
	Commit   string `json:"commit"`
	Run      string `json:"run"`
	Contains bool   `json:"contains"`
}

// WhereReleased answers, for every deliverable that holds the repository,
// whether its environment has the commit. within(repo, ancestor, commit) is
// git's merge-base --is-ancestor.
func WhereReleased(ctx context.Context, w World, t Table, repo, commit string, within func(ctx context.Context, repo, ancestor, commit string) (bool, error)) ([]Released, error) {
	var out []Released
	for _, d := range t.Deliverables {
		param := ""
		for _, c := range d.Commits {
			if c.Repo == repo {
				param = c.Param
			}
		}
		if param == "" {
			continue
		}
		r := Released{Deliverable: d.Name, Environment: d.Environment}
		runs, err := w.Runs(ctx, d.Pipeline)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.Status == RunSucceeded {
				r.Commit, r.Run = run.Params[param], run.Name
				break
			}
		}
		if r.Commit != "" {
			if r.Contains, err = within(ctx, repo, commit, r.Commit); err != nil {
				return nil, err
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Deliverable < out[j].Deliverable })
	return out, nil
}

func (d Deliverable) prod() bool {
	return strings.Contains(strings.ToLower(d.Environment), "prod") || strings.Contains(strings.ToLower(d.Pipeline), "prod")
}

func short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
