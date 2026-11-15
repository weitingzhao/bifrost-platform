package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
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
// unitScopeShadowEnv switches it: "on" computes, anything else (the default)
// computes nothing. It is read once, in unitScopeShadowSource, when the
// controller's sources are wired.
const unitScopeShadowEnv = "UNIT_SCOPE_SHADOW"

const (
	// One plan pair reads two commits' trees through the Gitea mirror; the
	// first after a restart can take about a minute. The bound is the scope
	// route's (handleUnitScope).
	unitScopeShadowTimeout = 150 * time.Second
	// The ledger read that says whether a run was shadowed before a restart.
	unitScopeShadowLedgerTimeout = 2 * time.Second
	// Runs waiting for their plan pair; one more is dropped, and logged.
	unitScopeShadowPending = 16
)

// The audit record of a shadowed run (GET /api/v1/audit, action
// unit_scope_shadow): status missed when a failing package was among those
// the scope would have skipped, else ok; detail is the log line as JSON.
const (
	unitScopeShadowPrincipal = "unit-scope-shadow"
	unitScopeShadowAction    = "unit_scope_shadow"
)

// shadowRecord turns the log line's key and value pairs into the record.
func shadowRecord(line []any) (status, detail string) {
	doc := map[string]any{}
	for i := 0; i+1 < len(line); i += 2 {
		if key, ok := line[i].(string); ok {
			doc[key] = line[i+1]
		}
	}
	status = "ok"
	if missed, ok := doc["missed"].([]string); ok && len(missed) > 0 {
		status = "missed"
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return status, fmt.Sprint(line...)
	}
	return status, string(raw)
}

type unitScopeShadow struct {
	repo, pipeline string
	// scope is the run's commit against its base, and that base.
	scope func(ctx context.Context, repo, commit string) (unitplan.Scope, error)
	// known says whether the evidence ledger already has the run: it was
	// shadowed by a process before this one.
	known func(ctx context.Context, repo, commit, uid string) (bool, error)
	// start runs one computation off the caller's pass.
	start func(fn func())
	// record keeps the line where it outlives the process: the audit log.
	// The workers are replaced at every release of the environment, and
	// with them their log (2026-11-15: three lines read, then none).
	record func(run, status, detail string)
	log    *slog.Logger

	mu      sync.Mutex
	seen    map[string]bool // run uid: told in this process
	pending int
	one     sync.Mutex // one plan pair at a time
}

// unitScopeShadowSource is the controller's RunEvidenced, or nil when the
// switch is not "on" or there is nothing to read commits from.
func (s *Server) unitScopeShadowSource() func(context.Context, store.PipelineRun) {
	if os.Getenv(unitScopeShadowEnv) != "on" || s.delivery == nil {
		return nil
	}
	db := func() store.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}
	baseCI := ledgerCIScoped(db, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), false)
	sh := &unitScopeShadow{
		repo: agentrtDeployRepo, pipeline: rules.LandingRepositories[agentrtDeployRepo].CIPipeline,
		scope: func(ctx context.Context, repo, commit string) (unitplan.Scope, error) {
			base, err := s.giteaFirstParent(ctx, repo, commit)
			if err != nil {
				return unitplan.Scope{}, err
			}
			return unitScope(ctx, s.unitTreesOf(), baseCIFunc(baseCI), repo, commit, base)
		},
		known: func(ctx context.Context, repo, commit, uid string) (bool, error) {
			d := db()
			if d == nil {
				return false, fmt.Errorf("the evidence ledger is not connected")
			}
			rows, err := evidence.EvidenceCIByCommit(ctx, d, repo, commit, 50)
			for _, r := range rows {
				if r.RunUID == uid {
					return true, err
				}
			}
			return false, err
		},
		start: func(fn func()) { safego.Go("unit-scope-shadow", fn) },
		record: func(run, status, detail string) {
			s.audit.RecordDirect(unitScopeShadowPrincipal, actuation.RoleOperator, unitScopeShadowAction, "run/"+run, status, detail)
		},
	}
	return sh.observe
}

// giteaFirstParent is the first parent of a commit as Gitea has it, "" for a
// commit with none. It is the base a landing's commit is compared with: the
// CI run's own changes task compares with the merge base of main, which for a
// commit prepared on main's head is this parent, and has no base at all for a
// commit already on main.
func (s *Server) giteaFirstParent(ctx context.Context, repo, commit string) (string, error) {
	s.unitTreesOf()
	body, err := s.unitTrees.get(ctx, url.PathEscape(repo)+"/git/commits/"+commit+"?stat=false&verification=false&files=false", 1<<20)
	if err != nil {
		return "", err
	}
	var doc struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err = json.Unmarshal(body, &doc); err != nil || doc.SHA != commit {
		return "", fmt.Errorf("gitea: %s commit %s: not that commit (%v)", repo, commit, err)
	}
	if len(doc.Parents) == 0 {
		return "", nil
	}
	if !commitID.MatchString(doc.Parents[0].SHA) {
		return "", fmt.Errorf("gitea: %s commit %s: its parent is not a commit id", repo, commit)
	}
	return doc.Parents[0].SHA, nil
}

// observe takes a run the records pass is about to write evidence of. It
// returns at once: the ledger is asked here (before the pass writes the row
// that would answer yes), the plans are computed off the pass. Nothing it
// does fails or holds the pass; what goes wrong is logged and dropped.
func (u *unitScopeShadow) observe(ctx context.Context, p store.PipelineRun) {
	if p.Kind != "ci" || p.Repo != u.repo || p.Pipeline != u.pipeline || p.UID == "" {
		return
	}
	commit := ""
	for _, in := range p.Inputs {
		if in.Repo == p.Repo {
			commit = in.Commit
		}
	}
	if !commitID.MatchString(commit) {
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
	full := u.pending >= unitScopeShadowPending
	u.mu.Unlock()
	forget := func() {
		u.mu.Lock()
		delete(u.seen, p.UID)
		u.mu.Unlock()
	}
	kctx, cancel := context.WithTimeout(ctx, unitScopeShadowLedgerTimeout)
	known, err := u.known(kctx, p.Repo, commit, p.UID)
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
		u.logger().Warn("unit scope shadow", "run", p.Run, "revision", commit, "error", fmt.Sprintf("dropped: %d runs already wait for their plans", unitScopeShadowPending))
		return
	}
	u.mu.Lock()
	u.pending++
	u.mu.Unlock()
	u.start(func() {
		defer func() {
			u.mu.Lock()
			u.pending--
			u.mu.Unlock()
		}()
		u.one.Lock()
		defer u.one.Unlock()
		// Not the pass's context: the pass ends long before a first plan does.
		sctx, cancel := context.WithTimeout(context.Background(), unitScopeShadowTimeout)
		defer cancel()
		scope, err := u.scope(sctx, p.Repo, commit)
		if err != nil {
			u.logger().Warn("unit scope shadow", "run", p.Run, "revision", commit, "error", err.Error())
			return
		}
		line := shadowLine(p, commit, scope)
		u.logger().Info("unit scope shadow", line...)
		if u.record != nil {
			status, detail := shadowRecord(line)
			u.record(p.Run, status, detail)
		}
	})
}

func (u *unitScopeShadow) logger() *slog.Logger {
	if u.log != nil {
		return u.log
	}
	return slog.Default()
}

// shadowLine is the line's fields: the counts over every unit of the plan
// (by_module splits them), the reason when it is not the keys that make
// everything run, and missed: the units that have a failed test of the run
// and whose keys are the base's. unmatched counts the failed tests of no unit
// of the plan, which are neither a miss nor covered.
func shadowLine(p store.PipelineRun, commit string, scope unitplan.Scope) []any {
	same := map[string]bool{} // unit -> its keys are the base's
	run, byModule := 0, []string{}
	for _, m := range scope.Modules {
		for _, unit := range m.RunUnits {
			same[unit] = false
		}
		for _, unit := range m.SameUnits {
			same[unit] = true
		}
		run += len(m.RunUnits)
		byModule = append(byModule, fmt.Sprintf("%s=%d/%d", m.Module, len(m.RunUnits), len(m.RunUnits)+len(m.SameUnits)))
	}
	missed, unmatched := map[string]bool{}, 0
	for _, test := range p.FailedTests {
		pkg := testPackage(test)
		best, fits := 0, false
		var skipped []string
		for unit, isSame := range same {
			n := unitMatch(pkg, unit)
			if n == 0 || n < best {
				continue
			}
			if n > best {
				best, fits, skipped = n, false, nil
			}
			fits = true
			if isSame {
				skipped = append(skipped, unit)
			}
		}
		if !fits {
			unmatched++
		}
		// Two units that fit a package equally: one the scope leaves out is
		// named, since a miss is what must not go unseen.
		for _, unit := range skipped {
			missed[unit] = true
		}
	}
	names := make([]string, 0, len(missed))
	for unit := range missed {
		names = append(names, unit)
	}
	sort.Strings(names)
	return []any{"run", p.Run, "revision", commit, "base", scope.Base, "verdict", p.Result, "run_scope", p.Scope,
		"would_run", run, "total", len(same), "by_module", strings.Join(byModule, " "), "reason", scope.Note,
		"missed", names, "failed_unnamed", p.Result == "Failed" && len(p.FailedTests) == 0, "unmatched", unmatched}
}

// testPackage is the package path of a failed test's name
// ("<package path>.<TestName>[/subtest]"): what stands before the test
// function. A name with no test function in it is no package's.
func testPackage(test string) string {
	at := -1
	for _, kind := range []string{".Test", ".Example", ".Fuzz", ".Benchmark"} {
		if i := strings.Index(test, kind); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if at < 0 {
		return ""
	}
	return test[:at]
}

// unitMatch is how many of a unit's last path elements a package path ends
// with, 0 when not even the last. A unit is named by its directory in the
// repository and a package by its path in its module, whose root the plan
// does not say: the unit that fits with the most elements is the package's.
func unitMatch(pkg, unit string) int {
	if pkg == "" {
		return 0
	}
	parts := strings.Split(unit, "/")
	for n := len(parts); n > 0; n-- {
		tail := strings.Join(parts[len(parts)-n:], "/")
		if pkg == tail || strings.HasSuffix(pkg, "/"+tail) {
			return n
		}
	}
	return 0
}
