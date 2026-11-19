package evidenceread

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// PlanTask is one unit task of the plan with the ledger's word on its key.
type PlanTask struct {
	Unit    string         `json:"unit"`
	Task    string         `json:"task"`
	Module  string         `json:"module"`
	Key     string         `json:"input_key"`
	Shards  int            `json:"shards,omitempty"`
	Verdict string         `json:"verdict"` // the ledger's for this key: green, red, none
	Inputs  map[string]any `json:"inputs,omitempty"`
}

// PlanAnswer is the answer of GET /api/v1/units/plan.
type PlanAnswer struct {
	Repo   string             `json:"repo"`
	Commit string             `json:"commit"`
	Tree   string             `json:"tree"`
	Pins   []unitplan.PlanPin `json:"pins"`
	Units  []PlanTask         `json:"units"`
	// ToRun counts the tasks whose key the ledger is not green for.
	ToRun  int            `json:"to_run"`
	Counts map[string]int `json:"counts"` // by verdict
}

// UnitPlan computes the plan and reads the ledger for each of its keys.
func UnitPlan(ctx context.Context, db eventlog.DB, trees func(ctx context.Context, repo, commit string) (*unitplan.Tree, error), repo, commit string, withInputs bool) (PlanAnswer, error) {
	plan, err := unitplan.BuildFrom(ctx, trees, repo, commit)
	if err != nil {
		return PlanAnswer{}, err
	}
	out := PlanAnswer{Repo: repo, Commit: commit, Tree: plan.Tree, Pins: plan.Pins, Units: make([]PlanTask, 0, len(plan.Units)), Counts: map[string]int{}}
	for _, u := range plan.Units {
		rows, err := evidence.EvidenceByKey(ctx, db, repo, u.Unit, u.Task, u.Key)
		if err != nil {
			return PlanAnswer{}, err
		}
		task := PlanTask{Unit: u.Unit, Task: u.Task, Module: u.Module, Key: u.Key, Shards: u.Shards, Verdict: evidence.EvidenceVerdict(rows)}
		if withInputs {
			task.Inputs = u.Inputs
		}
		out.Counts[task.Verdict]++
		if task.Verdict != evidence.EvidenceGreen {
			out.ToRun++
		}
		out.Units = append(out.Units, task)
	}
	return out, nil
}

// PlanSources is what one request of the plan route reads.
type PlanSources struct {
	DB    eventlog.DB
	Trees func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)
}

// PlanRoute is GET /api/v1/units/plan?repo=…&commit=…: CI's plan of one
// commit (CI design v4 §3.2, landing stage 2 block 2-B): the commit set (the
// commit and the pins of its bifrost.units.yaml), every unit's task with its
// key, what the evidence ledger says of that key, and so what would have to
// run. Read-only: it computes and reads; it starts nothing and writes
// nothing. With inputs=1 each task carries the document its key is the hash
// of. The checks of a request's names are the caller's, and Sources says,
// per request, what to read (ok false: the ledger is not available).
type PlanRoute struct {
	RepoOK, CommitOK func(string) bool
	Sources          func() (PlanSources, bool)
}

func (p PlanRoute) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p PlanRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !p.RepoOK(repo) || strings.HasSuffix(repo, ".git") || !p.CommitOK(commit) {
		p.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	src, ok := p.Sources()
	if !ok {
		p.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	out, err := UnitPlan(ctx, src.DB, src.Trees, repo, commit, r.URL.Query().Get("inputs") == "1")
	if err != nil {
		p.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	p.writeJSON(w, http.StatusOK, out)
}
