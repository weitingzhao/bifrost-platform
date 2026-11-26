package unitexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// Units as the platform runs them (CI design v4, landing stage 2 block 2-C).
//
// Advance takes a commit one step: it computes the plan, and for every
// task the ledger has no verdict on it creates the TaskRuns that are not
// there yet, and once more the one whose first run the cluster lost. A run is
// named by its plan and number, so advancing twice starts nothing twice.
// ReceiveUnits records every unit TaskRun that finished and writes a row of
// evidence for each task whose parts add up to a verdict.
//
// Who calls Advance is another layer (ISSUE-74: a push event,
// with a slow poll behind it) and is not written yet. Until then an agent
// asks through POST /api/v1/units/evidence, and the receiving pass runs.
//
// UNIT_DISPATCH is off (the default: nothing is created), shadow or on (both
// create; what a gate does with the result is not this file's). A platform
// that may not create TaskRuns (STG) still receives: it sees the same runs.
const (
	DispatchEnv     = "UNIT_DISPATCH"
	NodeSelectorEnv = "UNIT_NODE_SELECTOR"
	MaxRunningEnv   = "UNIT_MAX_RUNNING"
	GiteaBaseEnv    = "UNIT_GITEA_BASE"

	MaxRunningDefault = 12
	GiteaBaseDefault  = "http://gitea.cicd.svc.cluster.local:3000/bifrost"
	// ContinueWindow: the receiving pass takes a commit a step further
	// while its newest unit run is younger than this, so the runs put off
	// for want of room start as room comes, and the one retry of a lost run
	// starts. A commit that has started nothing for this long is done or
	// stuck, and is left until someone asks again.
	ContinueWindow = 20 * time.Minute
)

type Config struct {
	Mode         string
	NodeSelector map[string]string
	MaxRunning   int
	GiteaBase    string
	Namespace    string
}

func ConfigFromEnv(namespace string) Config {
	cfg := Config{Mode: "off", MaxRunning: MaxRunningDefault, GiteaBase: GiteaBaseDefault, Namespace: namespace}
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(DispatchEnv))); v {
	case "", "off":
	case "shadow", "on":
		cfg.Mode = v
	default:
		slog.Error("units: "+DispatchEnv+" is not off, shadow or on; off holds", "value", v)
	}
	if v := strings.TrimSpace(os.Getenv(NodeSelectorEnv)); v != "" {
		if key, value, ok := strings.Cut(v, "="); ok && key != "" && value != "" && !strings.ContainsAny(v, ", ") {
			cfg.NodeSelector = map[string]string{key: value}
		} else {
			slog.Error("units: "+NodeSelectorEnv+" is not one key=value; no selector is set", "value", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv(MaxRunningEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			cfg.MaxRunning = n
		} else {
			slog.Error("units: "+MaxRunningEnv+" is not a number from 1 to 200; the default holds", "value", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv(GiteaBaseEnv)); v != "" {
		cfg.GiteaBase = v
	}
	return cfg
}

// Cluster is the cluster as units need it.
type Cluster interface {
	// List returns the unit TaskRuns; plan "" is all of them.
	List(ctx context.Context, plan string) ([]TaskRun, error)
	// Create creates one; false when a run of that name is there already.
	Create(ctx context.Context, obj map[string]any) (bool, error)
}

type TreesFunc func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)

// Advanced is what one step did.
type Advanced struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Plan   string `json:"plan"`
	Mode   string `json:"mode"`
	// Tasks of the plan; ToRun of them have no verdict in the ledger.
	Tasks int `json:"tasks"`
	ToRun int `json:"to_run"`
	// Runs of ToRun's tasks: created now, there already, put off because
	// MaxRunning runs are going, retried now after the cluster lost them.
	Created  []string `json:"created"`
	Present  int      `json:"present"`
	Deferred int      `json:"deferred"`
	Retried  []string `json:"retried"`
	// Refused: tasks the plan cannot dispatch, each with why. Lost: runs the
	// cluster lost twice.
	Refused []string `json:"refused"`
	Lost    []string `json:"lost"`
}

func Advance(ctx context.Context, db eventlog.DB, trees TreesFunc, cl Cluster, cfg Config, repo, commit, tier string) (Advanced, error) {
	out := Advanced{Repo: repo, Commit: commit, Mode: cfg.Mode, Created: []string{}, Retried: []string{}, Refused: []string{}, Lost: []string{}}
	if cfg.Mode == "off" {
		return out, fmt.Errorf("units: dispatch is off on this platform (%s)", DispatchEnv)
	}
	plan, err := unitplan.BuildFrom(ctx, trees, repo, commit)
	if err != nil {
		return out, err
	}
	out.Plan, out.Tasks = PlanID(plan), len(plan.Units)
	all, err := cl.List(ctx, "")
	if err != nil {
		return out, err
	}
	byName := map[string]TaskRun{}
	running := 0
	for _, tr := range all {
		if tr.Metadata.Labels[LabelPlan] == out.Plan {
			byName[tr.Metadata.Name] = tr
		}
		if !Finished(tr) {
			running++
		}
	}
	seq := 0
	for _, u := range plan.Units {
		parts := u.Shards
		if parts < 1 {
			parts = 1
		}
		first := seq
		seq += parts
		rows, err := evidence.EvidenceByKey(ctx, db, repo, u.Unit, u.Task, u.Key)
		if err != nil {
			return out, err
		}
		if evidence.EvidenceVerdict(rows) != evidence.EvidenceNone {
			continue
		}
		out.ToRun++
		for i := 0; i < parts; i++ {
			job := Job{Plan: plan, Task: u, ShardIndex: i, ShardTotal: parts, Attempt: 1, Seq: first + i, Tier: tier,
				GiteaBase: cfg.GiteaBase, NodeSelector: cfg.NodeSelector, Namespace: cfg.Namespace}
			retry := false
			if tr, ok := byName[RunName(out.Plan, job.Seq, 1)]; ok {
				row, done, rerr := Receive(tr)
				if rerr != nil || !done || row.Class != evidence.UnitRunInfra {
					out.Present++
					continue
				}
				second := RunName(out.Plan, job.Seq, 2)
				if tr2, ok := byName[second]; ok {
					if row2, done2, rerr2 := Receive(tr2); rerr2 == nil && done2 && row2.Class == evidence.UnitRunInfra {
						out.Lost = append(out.Lost, second)
					} else {
						out.Present++
					}
					continue
				}
				job.Attempt, retry = 2, true
			}
			obj, err := TaskRunFor(job)
			if err != nil {
				if i == 0 {
					out.Refused = append(out.Refused, err.Error())
				}
				continue
			}
			if running >= cfg.MaxRunning {
				out.Deferred++
				continue
			}
			created, err := cl.Create(ctx, obj)
			if err != nil {
				return out, fmt.Errorf("units: creating %s: %w", RunName(out.Plan, job.Seq, job.Attempt), err)
			}
			if !created {
				out.Present++
				continue
			}
			running++
			name := RunName(out.Plan, job.Seq, job.Attempt)
			if retry {
				out.Retried = append(out.Retried, name)
			} else {
				out.Created = append(out.Created, name)
			}
		}
	}
	return out, nil
}

// Receiver keeps what the receiving pass has settled, so a pass does not
// compute a plan again for a task it already wrote.
type Receiver struct {
	mu      sync.Mutex
	settled map[string]bool
}

// ReceiveUnits records the finished unit TaskRuns and writes the evidence of
// the tasks they settle. advance, when set, is called for each commit whose
// newest unit run is younger than ContinueWindow: advancing is harmless
// when nothing is left to start, and it is how runs put off for want of room
// and the retry of a lost run get started without anyone asking again. (A
// commit all of whose runs were put off has no run to be seen by, and waits
// for the next request: the route's answer says how many were put off.)
func (u *Receiver) ReceiveUnits(ctx context.Context, db eventlog.DB, trees TreesFunc, cl Cluster, advance func(ctx context.Context, repo, commit, tier string) error, now time.Time) (recorded, written int, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.settled == nil {
		u.settled = map[string]bool{}
	}
	all, err := cl.List(ctx, "")
	if err != nil {
		return 0, 0, err
	}
	type group struct {
		rows   []evidence.UnitRun
		latest time.Time
		tier   string
	}
	groups := map[string]*group{}
	var errs []string
	for _, tr := range all {
		row, done, rerr := Receive(tr)
		if rerr != nil {
			errs = append(errs, rerr.Error())
			continue
		}
		key := strings.Join([]string{row.Repo, row.Unit, row.Task, row.InputKey, row.CommitSHA}, "\x00")
		g := groups[key]
		if g == nil {
			g = &group{tier: tr.Metadata.Labels[LabelTier]}
			groups[key] = g
		}
		if tr.Metadata.CreationTimestamp != nil && tr.Metadata.CreationTimestamp.After(g.latest) {
			g.latest = *tr.Metadata.CreationTimestamp
		}
		if !done {
			g.rows = append(g.rows, evidence.UnitRun{ShardIndex: row.ShardIndex, ShardTotal: row.ShardTotal, Attempt: row.Attempt, Class: "running", RunName: row.RunName})
			continue
		}
		wrote, werr := evidence.RecordUnitRun(ctx, db, row)
		if werr != nil {
			errs = append(errs, fmt.Sprintf("unit run %s: %v", row.RunName, werr))
			continue
		}
		if wrote {
			recorded++
		}
		g.rows = append(g.rows, row)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	type commitSeen struct {
		latest time.Time
		tier   string
	}
	commits := map[string]commitSeen{}
	// plans: a commit's plan is computed once a pass, whatever the number of
	// its tasks that settle in it (nil: it could not be computed this pass).
	plans := map[string]*unitplan.Plan{}
	for _, key := range keys {
		g := groups[key]
		if parts := strings.Split(key, "\x00"); len(parts) == 5 {
			ck := parts[0] + "\x00" + parts[4]
			if seen := commits[ck]; g.latest.After(seen.latest) {
				commits[ck] = commitSeen{latest: g.latest, tier: g.tier}
			}
		}
		if u.settled[key] {
			continue
		}
		var finished []evidence.UnitRun
		pending := false
		for _, r := range g.rows {
			if r.Class == "running" {
				pending = true
			} else {
				finished = append(finished, r)
			}
		}
		s := Settle(finished)
		head := s.Head
		if s.Verdict == "" || s.Verdict == evidence.UnitRunPassed && pending {
			continue
		}
		// A task the ledger already has a verdict on needs nothing more: the
		// row was written by an earlier pass or another process. Asked first,
		// because the plan of a commit is the dear part (after a restart
		// every retained run is unsettled; PROD 2026-11-13: one plan a task
		// made a pass take minutes, and nothing was started meanwhile).
		if before, lerr := evidence.EvidenceByKey(ctx, db, head.Repo, head.Unit, head.Task, head.InputKey); lerr != nil {
			errs = append(errs, lerr.Error())
			continue
		} else if evidence.EvidenceVerdict(before) != evidence.EvidenceNone {
			u.settled[key] = true
			continue
		}
		planKey := head.Repo + "\x00" + head.CommitSHA
		plan, seen := plans[planKey]
		if !seen {
			built, perr := unitplan.BuildFrom(ctx, trees, head.Repo, head.CommitSHA)
			if perr != nil {
				errs = append(errs, fmt.Sprintf("plan of %s %s: %v", head.Repo, head.CommitSHA, perr))
				built = nil
			}
			plans[planKey], plan = built, built
		}
		if plan == nil {
			continue
		}
		var task *unitplan.PlannedTask
		for i := range plan.Units {
			if p := &plan.Units[i]; p.Unit == head.Unit && p.Task == head.Task && p.Key == head.InputKey {
				task = p
			}
		}
		if task == nil {
			// The run names a key the commit's plan does not have: its
			// result is of other content than the ledger would say.
			errs = append(errs, fmt.Sprintf("unit run %s: its key is not in the plan of %s; no evidence written", head.RunName, head.CommitSHA))
			u.settled[key] = true
			continue
		}
		inputs, merr := json.Marshal(task.Inputs) // the bytes unitplan.Hash hashed
		if merr != nil {
			errs = append(errs, merr.Error())
			continue
		}
		completed := now
		if head.CompletedAt != nil {
			completed = *head.CompletedAt
		}
		row, eerr := evidence.UnitEvidenceRow(evidence.UnitEvidence{Repo: head.Repo, Unit: head.Unit, Task: head.Task, InputKey: head.InputKey, Inputs: inputs,
			Passed: s.Verdict == evidence.UnitRunPassed, Commit: head.CommitSHA, Tree: plan.Tree, RunName: head.RunName, RunUID: head.RunUID,
			CompletedAt: completed, FailedTests: s.FailedTests, FailClass: s.FailClass, Runs: s.Runs})
		if eerr != nil {
			errs = append(errs, eerr.Error())
			continue
		}
		n, werr := evidence.RecordEvidence(ctx, db, []evidence.EvidenceRow{row})
		if werr != nil {
			errs = append(errs, werr.Error())
			continue
		}
		written += n
		u.settled[key] = true
	}
	if advance != nil {
		for key, seen := range commits {
			if now.Sub(seen.latest) >= ContinueWindow {
				continue
			}
			repo, commit, _ := strings.Cut(key, "\x00")
			if aerr := advance(ctx, repo, commit, seen.tier); aerr != nil {
				errs = append(errs, aerr.Error())
			}
		}
	}
	if len(errs) > 0 {
		err = fmt.Errorf("units: %s", strings.Join(errs, "; "))
	}
	return recorded, written, err
}

// FuncCluster is Cluster over the two calls the caller makes of the cluster (the delivery service, for the platform).
type FuncCluster struct {
	ListFn   func(ctx context.Context, label, selector string) ([]map[string]any, error)
	CreateFn func(ctx context.Context, obj map[string]any) (bool, error)
}

func (d FuncCluster) List(ctx context.Context, plan string) ([]TaskRun, error) {
	selector := ""
	if plan != "" {
		selector = LabelPlan + "=" + plan
	}
	items, err := d.ListFn(ctx, LabelPlan, selector)
	if err != nil {
		return nil, err
	}
	out := make([]TaskRun, 0, len(items))
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var tr TaskRun
		if err = json.Unmarshal(raw, &tr); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, nil
}

func (d FuncCluster) Create(ctx context.Context, obj map[string]any) (bool, error) {
	return d.CreateFn(ctx, obj)
}

// ErrNoNode says no node can take the units now. It is a wait, not a failure: unit
// runs wait for a node, nothing depends on them, and since 2026-11-19 the database
// nodes stop taking them after writing too much (the controller alerted on four
// such passes in a row).
var ErrNoNode = errors.New("no node for the units")

// PlacedConfig is the dispatch configuration with the node the units go to
// now, read from the node profiles; a slow disk is refused, not fallen back to
// (two tasks timed out on ubt-k3s-05 on 2026-11-18). A platform without node
// metrics keeps what its environment says. place says where the units go
// (node, why when there is none, whether the profiles measured it); hostLabel
// is the node label the selector names.
func PlacedConfig(ctx context.Context, namespace, hostLabel string, place func(ctx context.Context) (node, why string, measured bool)) (Config, error) {
	cfg := ConfigFromEnv(namespace)
	node, why, measured := place(ctx)
	if !measured {
		return cfg, nil
	}
	if node == "" {
		return cfg, fmt.Errorf("%w: %s", ErrNoNode, why)
	}
	cfg.NodeSelector = map[string]string{hostLabel: node}
	return cfg, nil
}

// RunsDeps is what the receiving pass stands on: each is read when the pass
// runs, as the database and the delivery service are set after the routes
// mount. DB returns nil when the ledger is not connected.
type RunsDeps struct {
	DB        func() eventlog.DB
	Trees     func() TreesFunc
	Cluster   func() Cluster
	Namespace func() string
	// Placed is the dispatch configuration with the node the units go to now.
	Placed func(ctx context.Context) (Config, error)
}

// RunsSource has the controller record finished unit tasks at its pipeline
// interval. With dispatch on, a commit whose part the cluster lost once is
// advanced again from there.
func RunsSource(d RunsDeps) func(context.Context) error {
	recv := &Receiver{}
	return func(ctx context.Context) error {
		db := d.DB()
		if db == nil {
			return nil
		}
		trees, cl := d.Trees(), d.Cluster()
		cfg := ConfigFromEnv(d.Namespace())
		var advance func(context.Context, string, string, string) error
		if cfg.Mode != "off" {
			advance = func(ctx context.Context, repo, commit, tier string) error {
				placed, err := d.Placed(ctx)
				if errors.Is(err, ErrNoNode) {
					slog.Info("units: waiting for a node", "repo", repo, "commit", commit, "why", err)
					return nil
				}
				if err != nil {
					return err
				}
				_, err = Advance(ctx, db, trees, cl, placed, repo, commit, tier)
				return err
			}
		}
		recorded, written, err := recv.ReceiveUnits(ctx, db, trees, cl, advance, time.Now())
		if recorded > 0 || written > 0 {
			slog.Info("units: recorded finished unit tasks", "runs", recorded, "evidence", written)
		}
		return err
	}
}

// EvidenceSources is what one request of the evidence-request route stands on.
type EvidenceSources struct {
	DB        eventlog.DB
	Trees     TreesFunc
	Cluster   Cluster
	Namespace string
}

// EvidenceRoute is POST /api/v1/units/evidence {repo, commit}: have the
// platform run what the ledger has no verdict on for this commit (operator,
// tier B). The answer is what this call started; the results arrive in the
// ledger as the runs finish (GET /api/v1/units/plan shows them). The checks of
// a request's names are the caller's; Sources says, per request, what to use
// (ok false: this platform has no ledger); Placed is the dispatch
// configuration with the node the units go to now.
type EvidenceRoute struct {
	RepoOK, CommitOK func(string) bool
	Sources          func() (EvidenceSources, bool)
	Placed           func(ctx context.Context) (Config, error)
}

func (e EvidenceRoute) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (e EvidenceRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo   string `json:"repo"`
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		e.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is {repo, commit}"})
		return
	}
	repo, commit := strings.TrimSpace(body.Repo), strings.TrimSpace(body.Commit)
	if !e.RepoOK(repo) || strings.HasSuffix(repo, ".git") || !e.CommitOK(commit) {
		e.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	src, ok := e.Sources()
	if !ok {
		e.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	cfg := ConfigFromEnv(src.Namespace)
	if cfg.Mode == "off" {
		e.writeJSON(w, http.StatusConflict, map[string]string{"error": "unit dispatch is off on this platform (" + DispatchEnv + ")"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	cfg, err := e.Placed(ctx)
	if err != nil {
		e.writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	out, err := Advance(ctx, src.DB, src.Trees, src.Cluster, cfg, repo, commit, "branch")
	if err != nil {
		e.writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "partial": out})
		return
	}
	e.writeJSON(w, http.StatusOK, out)
}
