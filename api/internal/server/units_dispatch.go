package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// Units as the platform runs them (CI design v4, landing stage 2 block 2-C).
//
// advanceUnits takes a commit one step: it computes the plan, and for every
// task the ledger has no verdict on it creates the TaskRuns that are not
// there yet, and once more the one whose first run the cluster lost. A run is
// named by its plan and number, so advancing twice starts nothing twice.
// receiveUnits records every unit TaskRun that finished and writes a row of
// evidence for each task whose parts add up to a verdict.
//
// Who calls advanceUnits is another layer (Owner 2026-11-13: a push event,
// with a slow poll behind it) and is not written yet. Until then an agent
// asks through POST /api/v1/units/evidence, and the receiving pass runs.
//
// UNIT_DISPATCH is off (the default: nothing is created), shadow or on (both
// create; what a gate does with the result is not this file's). A platform
// that may not create TaskRuns (STG) still receives: it sees the same runs.
const (
	unitDispatchEnv     = "UNIT_DISPATCH"
	unitNodeSelectorEnv = "UNIT_NODE_SELECTOR"
	unitMaxRunningEnv   = "UNIT_MAX_RUNNING"
	unitGiteaBaseEnv    = "UNIT_GITEA_BASE"

	unitMaxRunningDefault = 12
	unitGiteaBaseDefault  = "http://gitea.cicd.svc.cluster.local:3000/bifrost"
	// unitContinueWindow: the receiving pass takes a commit a step further
	// while its newest unit run is younger than this, so the runs put off
	// for want of room start as room comes, and the one retry of a lost run
	// starts. A commit that has started nothing for this long is done or
	// stuck, and is left until someone asks again.
	unitContinueWindow = 20 * time.Minute
)

type unitDispatchConfig struct {
	Mode         string
	NodeSelector map[string]string
	MaxRunning   int
	GiteaBase    string
	Namespace    string
}

func unitDispatchConfigFromEnv(namespace string) unitDispatchConfig {
	cfg := unitDispatchConfig{Mode: "off", MaxRunning: unitMaxRunningDefault, GiteaBase: unitGiteaBaseDefault, Namespace: namespace}
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(unitDispatchEnv))); v {
	case "", "off":
	case "shadow", "on":
		cfg.Mode = v
	default:
		slog.Error("units: "+unitDispatchEnv+" is not off, shadow or on; off holds", "value", v)
	}
	if v := strings.TrimSpace(os.Getenv(unitNodeSelectorEnv)); v != "" {
		if key, value, ok := strings.Cut(v, "="); ok && key != "" && value != "" && !strings.ContainsAny(v, ", ") {
			cfg.NodeSelector = map[string]string{key: value}
		} else {
			slog.Error("units: "+unitNodeSelectorEnv+" is not one key=value; no selector is set", "value", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv(unitMaxRunningEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			cfg.MaxRunning = n
		} else {
			slog.Error("units: "+unitMaxRunningEnv+" is not a number from 1 to 200; the default holds", "value", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv(unitGiteaBaseEnv)); v != "" {
		cfg.GiteaBase = v
	}
	return cfg
}

// unitCluster is the cluster as units need it.
type unitCluster interface {
	// List returns the unit TaskRuns; plan "" is all of them.
	List(ctx context.Context, plan string) ([]unitexec.TaskRun, error)
	// Create creates one; false when a run of that name is there already.
	Create(ctx context.Context, obj map[string]any) (bool, error)
}

type unitTreesFunc func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)

// unitAdvance is what one step did.
type unitAdvance struct {
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

func advanceUnits(ctx context.Context, db store.DB, trees unitTreesFunc, cl unitCluster, cfg unitDispatchConfig, repo, commit, tier string) (unitAdvance, error) {
	out := unitAdvance{Repo: repo, Commit: commit, Mode: cfg.Mode, Created: []string{}, Retried: []string{}, Refused: []string{}, Lost: []string{}}
	if cfg.Mode == "off" {
		return out, fmt.Errorf("units: dispatch is off on this platform (%s)", unitDispatchEnv)
	}
	plan, err := buildUnitPlan(ctx, trees, repo, commit)
	if err != nil {
		return out, err
	}
	out.Plan, out.Tasks = unitexec.PlanID(plan), len(plan.Units)
	all, err := cl.List(ctx, "")
	if err != nil {
		return out, err
	}
	byName := map[string]unitexec.TaskRun{}
	running := 0
	for _, tr := range all {
		if tr.Metadata.Labels[unitexec.LabelPlan] == out.Plan {
			byName[tr.Metadata.Name] = tr
		}
		if !unitexec.Finished(tr) {
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
		rows, err := store.EvidenceByKey(ctx, db, repo, u.Unit, u.Task, u.Key)
		if err != nil {
			return out, err
		}
		if store.EvidenceVerdict(rows) != store.EvidenceNone {
			continue
		}
		out.ToRun++
		for i := 0; i < parts; i++ {
			job := unitexec.Job{Plan: plan, Task: u, ShardIndex: i, ShardTotal: parts, Attempt: 1, Seq: first + i, Tier: tier,
				GiteaBase: cfg.GiteaBase, NodeSelector: cfg.NodeSelector, Namespace: cfg.Namespace}
			retry := false
			if tr, ok := byName[unitexec.RunName(out.Plan, job.Seq, 1)]; ok {
				row, done, rerr := unitexec.Receive(tr)
				if rerr != nil || !done || row.Class != store.UnitRunInfra {
					out.Present++
					continue
				}
				second := unitexec.RunName(out.Plan, job.Seq, 2)
				if tr2, ok := byName[second]; ok {
					if row2, done2, rerr2 := unitexec.Receive(tr2); rerr2 == nil && done2 && row2.Class == store.UnitRunInfra {
						out.Lost = append(out.Lost, second)
					} else {
						out.Present++
					}
					continue
				}
				job.Attempt, retry = 2, true
			}
			obj, err := unitexec.TaskRunFor(job)
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
				return out, fmt.Errorf("units: creating %s: %w", unitexec.RunName(out.Plan, job.Seq, job.Attempt), err)
			}
			if !created {
				out.Present++
				continue
			}
			running++
			name := unitexec.RunName(out.Plan, job.Seq, job.Attempt)
			if retry {
				out.Retried = append(out.Retried, name)
			} else {
				out.Created = append(out.Created, name)
			}
		}
	}
	return out, nil
}

// unitReceiver keeps what the receiving pass has settled, so a pass does not
// compute a plan again for a task it already wrote.
type unitReceiver struct {
	mu      sync.Mutex
	settled map[string]bool
}

// receiveUnits records the finished unit TaskRuns and writes the evidence of
// the tasks they settle. advance, when set, is called for each commit whose
// newest unit run is younger than unitContinueWindow: advancing is harmless
// when nothing is left to start, and it is how runs put off for want of room
// and the retry of a lost run get started without anyone asking again. (A
// commit all of whose runs were put off has no run to be seen by, and waits
// for the next request: the route's answer says how many were put off.)
func (u *unitReceiver) receiveUnits(ctx context.Context, db store.DB, trees unitTreesFunc, cl unitCluster, advance func(ctx context.Context, repo, commit, tier string) error, now time.Time) (recorded, evidence int, err error) {
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
		rows   []store.UnitRun
		latest time.Time
		tier   string
	}
	groups := map[string]*group{}
	var errs []string
	for _, tr := range all {
		row, done, rerr := unitexec.Receive(tr)
		if rerr != nil {
			errs = append(errs, rerr.Error())
			continue
		}
		key := strings.Join([]string{row.Repo, row.Unit, row.Task, row.InputKey, row.CommitSHA}, "\x00")
		g := groups[key]
		if g == nil {
			g = &group{tier: tr.Metadata.Labels[unitexec.LabelTier]}
			groups[key] = g
		}
		if tr.Metadata.CreationTimestamp != nil && tr.Metadata.CreationTimestamp.After(g.latest) {
			g.latest = *tr.Metadata.CreationTimestamp
		}
		if !done {
			g.rows = append(g.rows, store.UnitRun{ShardIndex: row.ShardIndex, ShardTotal: row.ShardTotal, Attempt: row.Attempt, Class: "running", RunName: row.RunName})
			continue
		}
		wrote, werr := store.RecordUnitRun(ctx, db, row)
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
		var finished []store.UnitRun
		pending := false
		for _, r := range g.rows {
			if r.Class == "running" {
				pending = true
			} else {
				finished = append(finished, r)
			}
		}
		s := unitexec.Settle(finished)
		head := s.Head
		if s.Verdict == "" || s.Verdict == store.UnitRunPassed && pending {
			continue
		}
		// A task the ledger already has a verdict on needs nothing more: the
		// row was written by an earlier pass or another process. Asked first,
		// because the plan of a commit is the dear part (after a restart
		// every retained run is unsettled; PROD 2026-11-13: one plan a task
		// made a pass take minutes, and nothing was started meanwhile).
		if before, lerr := store.EvidenceByKey(ctx, db, head.Repo, head.Unit, head.Task, head.InputKey); lerr != nil {
			errs = append(errs, lerr.Error())
			continue
		} else if store.EvidenceVerdict(before) != store.EvidenceNone {
			u.settled[key] = true
			continue
		}
		planKey := head.Repo + "\x00" + head.CommitSHA
		plan, seen := plans[planKey]
		if !seen {
			built, perr := buildUnitPlan(ctx, trees, head.Repo, head.CommitSHA)
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
		row, eerr := store.UnitEvidenceRow(store.UnitEvidence{Repo: head.Repo, Unit: head.Unit, Task: head.Task, InputKey: head.InputKey, Inputs: inputs,
			Passed: s.Verdict == store.UnitRunPassed, Commit: head.CommitSHA, Tree: plan.Tree, RunName: head.RunName, RunUID: head.RunUID,
			CompletedAt: completed, FailedTests: s.FailedTests, FailClass: s.FailClass, Runs: s.Runs})
		if eerr != nil {
			errs = append(errs, eerr.Error())
			continue
		}
		n, werr := store.RecordEvidence(ctx, db, []store.EvidenceRow{row})
		if werr != nil {
			errs = append(errs, werr.Error())
			continue
		}
		evidence += n
		u.settled[key] = true
	}
	if advance != nil {
		for key, seen := range commits {
			if now.Sub(seen.latest) >= unitContinueWindow {
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
	return recorded, evidence, err
}

// deliveryUnitCluster is unitCluster over the delivery service.
type deliveryUnitCluster struct {
	list   func(ctx context.Context, label, selector string) ([]map[string]any, error)
	create func(ctx context.Context, obj map[string]any) (bool, error)
}

func (d deliveryUnitCluster) List(ctx context.Context, plan string) ([]unitexec.TaskRun, error) {
	selector := ""
	if plan != "" {
		selector = unitexec.LabelPlan + "=" + plan
	}
	items, err := d.list(ctx, unitexec.LabelPlan, selector)
	if err != nil {
		return nil, err
	}
	out := make([]unitexec.TaskRun, 0, len(items))
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var tr unitexec.TaskRun
		if err = json.Unmarshal(raw, &tr); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, nil
}

func (d deliveryUnitCluster) Create(ctx context.Context, obj map[string]any) (bool, error) {
	return d.create(ctx, obj)
}

func (s *Server) unitClusterOf() unitCluster {
	return deliveryUnitCluster{
		list: func(ctx context.Context, label, selector string) ([]map[string]any, error) {
			items, err := s.delivery.UnitTaskRuns(ctx, label, selector)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(items))
			for _, item := range items {
				out = append(out, item.Object)
			}
			return out, nil
		},
		create: s.delivery.CreateUnitTaskRun,
	}
}

func (s *Server) unitTreesOf() unitTreesFunc {
	s.unitTreesOnce.Do(func() {
		s.unitTrees = &giteaTrees{access: s.delivery.GiteaAccess, client: &http.Client{Timeout: 20 * time.Second}}
	})
	return s.unitTrees.tree
}

// wireUnitRuns has the controller record finished unit tasks at its pipeline
// interval. With dispatch on, a commit whose part the cluster lost once is
// advanced again from there.
func (s *Server) unitRunsSource() func(context.Context) error {
	if s.delivery == nil {
		return nil
	}
	recv := &unitReceiver{}
	return func(ctx context.Context) error {
		if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
			return nil
		}
		db, trees, cl := s.agentRuntime.api.DB, s.unitTreesOf(), s.unitClusterOf()
		cfg := unitDispatchConfigFromEnv(s.delivery.PipelinesNamespace())
		var advance func(context.Context, string, string, string) error
		if cfg.Mode != "off" {
			advance = func(ctx context.Context, repo, commit, tier string) error {
				_, err := advanceUnits(ctx, db, trees, cl, cfg, repo, commit, tier)
				return err
			}
		}
		recorded, evidence, err := recv.receiveUnits(ctx, db, trees, cl, advance, time.Now())
		if recorded > 0 || evidence > 0 {
			slog.Info("units: recorded finished unit tasks", "runs", recorded, "evidence", evidence)
		}
		return err
	}
}

// POST /api/v1/units/evidence {repo, commit}: have the platform run what the
// ledger has no verdict on for this commit (operator, tier B). The answer is
// what this call started; the results arrive in the ledger as the runs
// finish (GET /api/v1/units/plan shows them).
func (s *Server) handleUnitEvidenceRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo   string `json:"repo"`
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is {repo, commit}"})
		return
	}
	repo, commit := strings.TrimSpace(body.Repo), strings.TrimSpace(body.Commit)
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	cfg := unitDispatchConfigFromEnv(s.delivery.PipelinesNamespace())
	if cfg.Mode == "off" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "unit dispatch is off on this platform (" + unitDispatchEnv + ")"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	out, err := advanceUnits(ctx, s.agentRuntime.api.DB, s.unitTreesOf(), s.unitClusterOf(), cfg, repo, commit, "branch")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "partial": out})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
