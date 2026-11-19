package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	busdrift "github.com/weitingzhao/bifrost-platform/api/internal/bus/drift"
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
	baseCI := evidenceread.LedgerCIScoped(db, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), false)
	sh := &cirunview.ScopeShadow{
		Repo: agentrtDeployRepo, Pipeline: rules.LandingRepositories[agentrtDeployRepo].CIPipeline,
		Scope: func(ctx context.Context, repo, commit string) (unitplan.Scope, error) {
			base, err := s.giteaFirstParent(ctx, repo, commit)
			if err != nil {
				return unitplan.Scope{}, err
			}
			return unitplan.ScopeOf(ctx, s.unitTreesOf(), unitplan.BaseCIFunc(baseCI), repo, commit, base)
		},
		Known: func(ctx context.Context, repo, commit, uid string) (bool, error) {
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
		Start: func(fn func()) { safego.Go("unit-scope-shadow", fn) },
		Record: func(run, status, detail string) {
			s.audit.RecordDirect(cirunview.ScopeShadowPrincipal, actuation.RoleOperator, cirunview.ScopeShadowAction, "run/"+run, status, detail)
		},
		Incremental: func(ctx context.Context, repo, commit string) (string, error) {
			d := db()
			if d == nil {
				return "", fmt.Errorf("the evidence ledger is not connected")
			}
			rows, err := evidence.EvidenceCIByCommit(ctx, d, repo, commit, 50)
			if err != nil {
				return "", err
			}
			if tree, terr := giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})(ctx, repo, commit); terr == nil {
				more, err := evidence.EvidenceByTree(ctx, d, repo, tree, 50)
				if err != nil {
					return "", err
				}
				rows = append(rows, more...)
			}
			for _, r := range rows {
				if evidence.IncrementalBase(r) != "" {
					return r.RunName, nil
				}
			}
			return "", nil
		},
		Reconcile: func(run, status, detail string) {
			s.audit.RecordDirect(cirunview.ScopeShadowPrincipal, actuation.RoleOperator, cirunview.CIReconcileAction, "run/"+run, status, detail)
		},
		Drift: func(ctx context.Context, repo, commit, passed, contradicts string) error {
			d := db()
			if d == nil {
				return fmt.Errorf("the ledger is not connected")
			}
			_, err := busdrift.Announce(ctx, d, busdrift.Found{Repo: repo, Commit: commit, Kind: busdrift.FullVsIncremental, Passed: passed, Contradicts: contradicts})
			return err
		},
	}
	return sh.Observe
}

// giteaFirstParent is the first parent of a commit as Gitea has it
// (gitread.Trees.FirstParent).
func (s *Server) giteaFirstParent(ctx context.Context, repo, commit string) (string, error) {
	s.unitTreesOf()
	return s.unitTrees.FirstParent(ctx, repo, commit)
}
