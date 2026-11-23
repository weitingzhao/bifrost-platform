package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	verificationassembly "github.com/weitingzhao/bifrost-platform/api/internal/verification/assembly"
)

// The unit scope shadow is verification's (verification/assembly.ScopeShadow,
// over cirunview.ScopeShadow); this file lends it Gitea's trees, the ledger
// and the audit log.

// unitScopeShadowSource is the controller's RunEvidenced, or nil when the
// switch is not "on" or there is nothing to read commits from.
func (s *Server) unitScopeShadowSource() func(context.Context, seenfacts.PipelineRun) {
	if s.delivery == nil {
		return nil
	}
	return verificationassembly.ScopeShadow(verificationassembly.ScopeShadowLent{
		Repo: agentrtDeployRepo,
		DB: func() eventlog.DB {
			if s.agentRuntime == nil || s.agentRuntime.api == nil {
				return nil
			}
			return s.agentRuntime.api.DB
		},
		Tree:  giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}),
		Trees: s.unitTreesOf,
		FirstParent: s.giteaFirstParent,
		Audit: func(principal, action, target, status, detail string) {
			s.audit.RecordDirect(principal, actuation.RoleOperator, action, target, status, detail)
		},
	})
}

// giteaFirstParent is the first parent of a commit as Gitea has it
// (gitread.Trees.FirstParent).
func (s *Server) giteaFirstParent(ctx context.Context, repo, commit string) (string, error) {
	s.unitTreesOf()
	return s.unitTrees.FirstParent(ctx, repo, commit)
}
