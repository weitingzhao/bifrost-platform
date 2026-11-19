package server

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The unit scope shadow itself (what it judges and records) lives in
// cirunview.ScopeShadow; this file hands it Gitea's trees, the ledger, the
// audit log and the bus.

// unitScopeShadowSource is the controller's RunEvidenced, or nil when the
// switch is not "on" or there is nothing to read commits from.
func (s *Server) unitScopeShadowSource() func(context.Context, seenfacts.PipelineRun) {
	if os.Getenv(cirunview.ScopeShadowEnv) != "on" || s.delivery == nil {
		return nil
	}
	db := func() eventlog.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}
	tree := giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})
	baseCI := evidenceread.LedgerCIScoped(db, tree, false)
	sh := &cirunview.ScopeShadow{
		Repo: agentrtDeployRepo, Pipeline: rules.LandingRepositories[agentrtDeployRepo].CIPipeline,
		Scope: func(ctx context.Context, repo, commit string) (unitplan.Scope, error) {
			base, err := s.giteaFirstParent(ctx, repo, commit)
			if err != nil {
				return unitplan.Scope{}, err
			}
			return unitplan.ScopeOf(ctx, s.unitTreesOf(), unitplan.BaseCIFunc(baseCI), repo, commit, base)
		},
		Known: cirunview.LedgerKnown(db),
		Start: func(fn func()) { safego.Go("unit-scope-shadow", fn) },
		Record: func(run, status, detail string) {
			s.audit.RecordDirect(cirunview.ScopeShadowPrincipal, actuation.RoleOperator, cirunview.ScopeShadowAction, "run/"+run, status, detail)
		},
		Incremental: cirunview.LedgerIncremental(db, tree),
		Reconcile: func(run, status, detail string) {
			s.audit.RecordDirect(cirunview.ScopeShadowPrincipal, actuation.RoleOperator, cirunview.CIReconcileAction, "run/"+run, status, detail)
		},
		Drift: cirunview.AnnounceDrift(db),
	}
	return sh.Observe
}

// giteaFirstParent is the first parent of a commit as Gitea has it
// (gitread.Trees.FirstParent).
func (s *Server) giteaFirstParent(ctx context.Context, repo, commit string) (string, error) {
	s.unitTreesOf()
	return s.unitTrees.FirstParent(ctx, repo, commit)
}
