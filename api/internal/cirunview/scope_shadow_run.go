package cirunview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The unit scope shadow: for every finished CI run of the platform's own
// repository, what a run by unit would have run (the scope route's run_units
// against the commit's first parent), and whether every unit a failed test of
// the run belongs to was among them. A failing unit the scope would have left
// out is a miss, and is what this exists to find before CI skips by unit
// (main coordinator 2026-11-14). It decides nothing and writes nothing but
// one log line a run, "unit scope shadow".
//
// ScopeShadowEnv switches it: "on" computes, anything else (the default)
// computes nothing. It is read once, in the server's unitScopeShadowSource, when the
// controller's sources are wired.
const ScopeShadowEnv = "UNIT_SCOPE_SHADOW"

const (
	// One plan pair reads two commits' trees through the Gitea mirror; the
	// first after a restart can take about a minute. The bound is the scope
	// route's (evidenceread.ScopeRoute, in another package).
	scopeShadowTimeout = 150 * time.Second
	// The ledger read that says whether a run was shadowed before a restart.
	scopeShadowLedgerTimeout = 2 * time.Second
	// Runs waiting for their plan pair; one more is dropped, and logged.
	scopeShadowPending = 16
)

// The audit record of a shadowed run (GET /api/v1/audit, action
// unit_scope_shadow): status missed when a failing package was among those
// the scope would have skipped, else ok; detail is the log line as JSON.
const (
	ScopeShadowPrincipal = "unit-scope-shadow"
	ScopeShadowAction    = "unit_scope_shadow"
)

// ScopeShadow is the shadow of one repository's runs; its fields are what it
// reads and where it writes, set by whoever wires it.
type ScopeShadow struct {
	Repo, Pipeline string
	// scope is the run's commit against its base, and that base.
	Scope func(ctx context.Context, repo, commit string) (unitplan.Scope, error)
	// known says whether the evidence ledger already has the run: it was
	// shadowed by a process before this one.
	Known func(ctx context.Context, repo, commit, uid string) (bool, error)
	// start runs one computation off the caller's pass.
	Start func(fn func())
	// record keeps the line where it outlives the process: the audit log.
	// The workers are replaced at every release of the environment, and
	// with them their log (2026-11-15: three lines read, then none).
	Record func(run, status, detail string)
	// incremental is the run of a passed incremental row the ledger has for
	// the commit (by commit, then by tree), "" for none; nil: no reconciling.
	Incremental func(ctx context.Context, repo, commit string) (string, error)
	// reconcile keeps one reconciliation where record keeps a line.
	Reconcile func(run, status, detail string)
	// drift tells the bus of a miss (reconcile.drift.found); nil: nobody is told.
	Drift func(ctx context.Context, repo, commit, passed, contradicts string) error
	Log   *slog.Logger

	mu      sync.Mutex
	seen    map[string]bool // run uid: told in this process
	pending int
	one     sync.Mutex // one plan pair at a time
}

// Observe takes a run the records pass is about to write evidence of. It
// returns at once: the ledger is asked here (before the pass writes the row
// that would answer yes), the plans are computed off the pass. Nothing it
// does fails or holds the pass; what goes wrong is logged and dropped.
func (u *ScopeShadow) Observe(ctx context.Context, p seenfacts.PipelineRun) {
	if p.Kind != "ci" || p.Repo != u.Repo || p.Pipeline != u.Pipeline || p.UID == "" {
		return
	}
	commit := ""
	for _, in := range p.Inputs {
		if in.Repo == p.Repo {
			commit = in.Commit
		}
	}
	if !gitread.CommitID.MatchString(commit) {
		return
	}
	u.mu.Lock()
	if u.seen[p.UID] {
		u.mu.Unlock()
		return
	}
	if u.seen == nil {
		u.seen = map[string]bool{}
	}
	u.seen[p.UID] = true
	full := u.pending >= scopeShadowPending
	u.mu.Unlock()
	forget := func() {
		u.mu.Lock()
		delete(u.seen, p.UID)
		u.mu.Unlock()
	}
	kctx, cancel := context.WithTimeout(ctx, scopeShadowLedgerTimeout)
	known, err := u.Known(kctx, p.Repo, commit, p.UID)
	cancel()
	if err != nil {
		// Not known either way: the pass tells the run again only if its own
		// write fails, so this run may go without a line.
		forget()
		u.logger().Warn("unit scope shadow", "run", p.Run, "revision", commit, "error", "the ledger could not say whether the run is new: "+err.Error())
		return
	}
	if known {
		return
	}
	if full {
		forget()
		u.logger().Warn("unit scope shadow", "run", p.Run, "revision", commit, "error", fmt.Sprintf("dropped: %d runs already wait for their plans", scopeShadowPending))
		return
	}
	u.mu.Lock()
	u.pending++
	u.mu.Unlock()
	u.Start(func() {
		defer func() {
			u.mu.Lock()
			u.pending--
			u.mu.Unlock()
		}()
		u.one.Lock()
		defer u.one.Unlock()
		// Not the pass's context: the pass ends long before a first plan does.
		sctx, cancel := context.WithTimeout(context.Background(), scopeShadowTimeout)
		defer cancel()
		scope, err := u.Scope(sctx, p.Repo, commit)
		if err != nil {
			u.logger().Warn("unit scope shadow", "run", p.Run, "revision", commit, "error", err.Error())
			return
		}
		line := ShadowLine(ShadowRun{Run: p.Run, Result: p.Result, Scope: p.Scope, FailedTests: p.FailedTests}, commit, scope)
		u.logger().Info("unit scope shadow", line...)
		if u.Record != nil {
			status, detail := ShadowRecord(line)
			u.Record(p.Run, status, detail)
		}
		u.reconcileFull(sctx, p, commit, line)
	})
}

// CIReconcileAction is the audit action of one reconciliation: a full CI run
// of a commit that the ledger already has a passed incremental run for.
//
// Owner 2026-11-16: the landing lets a commit in on an incremental run and the
// full run of the push to main, which nobody waits for, is the check on it.
// status "ok": the full run passed too. "missed": it failed, so the
// incremental run passed something a full run catches; the row of the full
// run makes the commit failed in the ledger, which holds PROD (ledgerCIStrict)
// and leaves the commits after it without a verdict until one passes in full.
// The detail is the material of the "does CI time follow the change" reading:
// which runs, how long the full one took, how much of the plan the change
// touches (by_module), and what failed.
const CIReconcileAction = "ci_reconcile"

func (u *ScopeShadow) reconcileFull(ctx context.Context, p seenfacts.PipelineRun, commit string, line []any) {
	if u.Incremental == nil || u.Reconcile == nil || p.Scope != evidence.EvidenceScopeFull {
		return
	}
	inc, err := u.Incremental(ctx, p.Repo, commit)
	if err != nil {
		u.logger().Warn("ci reconcile", "run", p.Run, "revision", commit, "error", err.Error())
		return
	}
	if inc == "" || inc == p.Run {
		return
	}
	status := "ok"
	if p.Result != "Succeeded" {
		status = "missed"
	}
	fields := map[string]any{"repo": p.Repo, "revision": commit, "full_run": p.Run, "full_result": p.Result,
		"incremental_run": inc, "failed_tests": p.FailedTests, "failed_tasks": p.FailedTasks}
	if !p.StartedAt.IsZero() && !p.CompletedAt.IsZero() {
		fields["full_seconds"] = int(p.CompletedAt.Sub(p.StartedAt).Seconds())
	}
	for i := 0; i+1 < len(line); i += 2 {
		if k, _ := line[i].(string); k == "by_module" || k == "would_run" || k == "total" {
			fields[k] = line[i+1]
		}
	}
	detail, _ := json.Marshal(fields)
	if status == "missed" {
		u.logger().Error("ci reconcile: a full run failed on a commit an incremental run passed", "run", p.Run, "revision", commit, "incremental_run", inc, "failed_tests", p.FailedTests)
	} else {
		u.logger().Info("ci reconcile", "run", p.Run, "revision", commit, "incremental_run", inc, "status", status)
	}
	u.Reconcile(p.Run, status, string(detail))
	if status == "missed" && u.Drift != nil {
		// The record above stands whether or not the bus could be told.
		if err := u.Drift(ctx, p.Repo, commit, inc, p.Run); err != nil {
			u.logger().Warn("ci reconcile", "run", p.Run, "revision", commit, "error", "the miss was not announced: "+err.Error())
		}
	}
}

func (u *ScopeShadow) logger() *slog.Logger {
	if u.Log != nil {
		return u.Log
	}
	return slog.Default()
}
