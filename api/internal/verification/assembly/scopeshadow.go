package assembly

import (
	"context"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The unit scope shadow itself (what it judges and records) lives in
// cirunview.ScopeShadow; this file hands it Gitea's trees, the ledger, the
// audit log and the bus. Moved out of server (units_scope_shadow.go,
// unitScopeShadowSource) on 2026-11-23, unchanged.

// ScopeShadowLent is what the platform lends the shadow.
type ScopeShadowLent struct {
	// Repo is the repository whose CI runs are shadowed (agentrt's deploy
	// repository).
	Repo string
	// DB is the runtime's database as it connects; nil until it does.
	DB func() eventlog.DB
	// Tree reads the git tree of a commit; Trees, when called, reads a
	// commit's files.
	Tree  func(ctx context.Context, repo, commit string) (string, error)
	Trees func() unitexec.TreesFunc
	// FirstParent is the first parent of a commit as Gitea has it.
	FirstParent func(ctx context.Context, repo, commit string) (string, error)
	// Audit records one line of the audit log as the shadow's principal.
	Audit func(principal, action, target, status, detail string)
}

// ScopeShadow is the controller's RunEvidenced, or nil when the switch
// (cirunview.ScopeShadowEnv) is not "on".
func ScopeShadow(l ScopeShadowLent) func(context.Context, seenfacts.PipelineRun) {
	if os.Getenv(cirunview.ScopeShadowEnv) != "on" {
		return nil
	}
	db, tree := l.DB, l.Tree
	baseCI := evidenceread.LedgerCIScoped(db, tree, false)
	sh := &cirunview.ScopeShadow{
		Repo: l.Repo, Pipeline: rules.LandingRepositories[l.Repo].CIPipeline,
		Scope: func(ctx context.Context, repo, commit string) (unitplan.Scope, error) {
			base, err := l.FirstParent(ctx, repo, commit)
			if err != nil {
				return unitplan.Scope{}, err
			}
			return unitplan.ScopeOf(ctx, l.Trees(), unitplan.BaseCIFunc(baseCI), repo, commit, base)
		},
		Known: cirunview.LedgerKnown(db),
		Start: func(fn func()) { safego.Go("unit-scope-shadow", fn) },
		Record: func(run, status, detail string) {
			l.Audit(cirunview.ScopeShadowPrincipal, cirunview.ScopeShadowAction, "run/"+run, status, detail)
		},
		Incremental: cirunview.LedgerIncremental(db, tree),
		Reconcile: func(run, status, detail string) {
			l.Audit(cirunview.ScopeShadowPrincipal, cirunview.CIReconcileAction, "run/"+run, status, detail)
		},
		Drift: cirunview.AnnounceDrift(db),
	}
	return sh.Observe
}
