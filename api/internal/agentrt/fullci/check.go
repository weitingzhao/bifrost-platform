package fullci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GET /api/v1/units/full-ci/check?repo=…&green=…&red=…[&run=…] answers, and
// writes nothing: if the daily full run of commit red were red and the last
// green one had been of commit green, which landed commits would the
// reconciliation name? It lists main's first-parent commits after green up to
// red, each with its own CI run and which modules that run ran, and marks
// the suspects: those whose run left a module out. With run it also names
// the pipeline tasks of that run that failed. It is the same reading the
// controller does for a real red full run (agentrt/fullci), done on
// request, so that the reconciliation can be seen on real landings without
// waiting for a red run, and it is the run view's "what each landing ran".
// Read-only; viewer and above.

const CheckMax = 30 // as FullCILandingsMax

// CheckLanding is one landed commit of the check.
type CheckLanding struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	// Run is the landing's own CI run and Modules its run-modules as written
	// ("*" every module; names between spaces otherwise). Read says how the
	// reading went: ok, no_run (no run of its own with that result), error.
	Run     string `json:"run,omitempty"`
	Modules string `json:"modules,omitempty"`
	Read    string `json:"read"`
	Suspect bool   `json:"suspect"`
}

// CheckAnswer is the answer of GET /api/v1/units/full-ci/check.
type CheckAnswer struct {
	Repo     string         `json:"repo"`
	Green    string         `json:"green"`
	Red      string         `json:"red"`
	Landings []CheckLanding `json:"landings"`
	Suspects int            `json:"suspects"`
	Unread   int            `json:"unread"`
	// Unreconciled says why the landings could not all be looked at.
	Unreconciled string `json:"unreconciled,omitempty"`
	// Run and FailedTasks: the pipeline tasks of the run asked about that failed.
	Run         string   `json:"run,omitempty"`
	FailedTasks []string `json:"failed_tasks,omitempty"`
}

// CheckSources is what the check reads.
type CheckSources struct {
	// FirstParents lists the first-parent commits after green up to red,
	// newest first, at most max+1 (one more says there are more).
	FirstParents func(ctx context.Context, repo, green, red string, max int) ([][2]string, error)
	RunModules   func(ctx context.Context, repo, revision string) (run, modules string, err error)
	FailedTasks  func(ctx context.Context, run string) ([]string, error)
}

// Check reads the landings of main between two commits and says which of them
// left a module out of their own CI run.
func Check(ctx context.Context, src CheckSources, repo, green, red, run string) (CheckAnswer, error) {
	out := CheckAnswer{Repo: repo, Green: green, Red: red, Landings: []CheckLanding{}, Run: run}
	commits, err := src.FirstParents(ctx, repo, green, red, CheckMax)
	if err != nil {
		return out, err
	}
	if len(commits) > CheckMax {
		commits = commits[:CheckMax]
		out.Unreconciled = fmt.Sprintf("more than %d commits landed after %s; only the newest %d were looked at", CheckMax, green, CheckMax)
	}
	for _, c := range commits {
		l := CheckLanding{Commit: c[0], Subject: c[1]}
		r, modules, err := src.RunModules(ctx, repo, c[0])
		switch {
		case err != nil:
			l.Read = "error"
			out.Unread++
		case r == "":
			l.Read = "no_run"
			out.Unread++
		default:
			l.Read, l.Run, l.Modules = "ok", r, modules
			l.Suspect = modules != "" && strings.TrimSpace(modules) != "*"
		}
		if l.Suspect {
			out.Suspects++
		}
		out.Landings = append(out.Landings, l)
	}
	if run != "" {
		tasks, err := src.FailedTasks(ctx, run)
		if err != nil {
			return out, fmt.Errorf("run %s: %w", run, err)
		}
		out.FailedTasks = tasks
		if out.FailedTasks == nil {
			out.FailedTasks = []string{}
		}
	}
	return out, nil
}

// FirstParentsOfPage reads Gitea's page of main's commits (red first) and
// follows first parents from red until green (see firstParentsOf).
func FirstParentsOfPage(body []byte, repo, green, red string, max int) ([][2]string, error) {
	var page []struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("gitea: the commits of %s: %w", repo, err)
	}
	listed := make([]historyCommit, 0, len(page))
	for _, c := range page {
		h := historyCommit{sha: c.SHA, subject: strings.SplitN(c.Commit.Message, "\n", 2)[0]}
		if len(c.Parents) > 0 {
			h.parent = c.Parents[0].SHA
		}
		listed = append(listed, h)
	}
	return firstParentsOf(listed, green, red, max)
}

type historyCommit struct {
	sha, parent, subject string
}

// firstParentsOf follows first parents from red through the commits listed
// until green: the commits after green up to red, newest first, at most
// max+1. A chain that leaves the listing before reaching green is an error:
// green is not a first-parent ancestor of red within what was read.
func firstParentsOf(listed []historyCommit, green, red string, max int) ([][2]string, error) {
	by := map[string]historyCommit{}
	for _, c := range listed {
		by[c.sha] = c
	}
	var out [][2]string
	for at := red; at != green; {
		c, ok := by[at]
		if !ok {
			return nil, fmt.Errorf("%s is not reached from %s along first parents within the %d commits read", green, red, len(listed))
		}
		out = append(out, [2]string{c.sha, c.subject})
		if len(out) > max {
			return out, nil
		}
		at = c.parent
	}
	return out, nil
}

// CheckRoute is GET /api/v1/units/full-ci/check: the checks of a request's
// names are the caller's, and Sources says, per request, what to read (ok
// false: this platform does not read the cluster and Gitea).
type CheckRoute struct {
	RepoOK, RunOK, CommitOK func(string) bool
	Sources                 func() (CheckSources, bool)
}

func checkJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (c CheckRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo, green, red, run := strings.TrimSpace(q.Get("repo")), strings.TrimSpace(q.Get("green")), strings.TrimSpace(q.Get("red")), strings.TrimSpace(q.Get("run"))
	if !c.RepoOK(repo) || strings.HasSuffix(repo, ".git") || !c.CommitOK(green) || !c.CommitOK(red) || green == red ||
		(run != "" && !c.RunOK(run)) {
		checkJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name; green and red are two different 40-character lowercase SHAs; run, when given, a run's name"})
		return
	}
	src, ok := c.Sources()
	if !ok {
		checkJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read the cluster and Gitea"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	out, err := Check(ctx, src, repo, green, red, run)
	if err != nil {
		checkJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	checkJSON(w, http.StatusOK, out)
}
