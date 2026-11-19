package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/approvalfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deploysource"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// What the agent runtime reads of the platform's stores lives in the
// agentrt packages (deploysource, approvalfacts, runfacts, gitread, readslot);
// this file names them for the rest of the server and builds the controller's
// sources from them.

// agentrtDeployRepo is the repository whose deployments the runtime reads.
const agentrtDeployRepo = deploysource.Repo

var (
	giteaName = gitread.Name
	commitID  = gitread.CommitID
)

// commitTreeEntry is one entry of a Gitea tree answer.
type commitTreeEntry = gitread.TreeEntry

// gitAccess is the delivery service's Gitea access as gitread reads it.
func gitAccess(access func(context.Context) (delivery.GiteaAccess, error)) func(context.Context) (gitread.Access, error) {
	return func(ctx context.Context) (gitread.Access, error) {
		a, err := access(ctx)
		return gitread.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, err
	}
}

// giteaCommitTree reads the git tree of a commit of <org>/<repo> from Gitea
// with the delivery service's read-only credential (gitread.CommitTree).
func giteaCommitTree(access func(context.Context) (delivery.GiteaAccess, error), client *http.Client) func(context.Context, string, string) (string, error) {
	return gitread.CommitTree(gitAccess(access), client)
}

// agentRuntimeMains reads the head of a repository's main where agentd clones
// it from: the organization the releases clone from (the delivery service's
// Gitea organization, what agentd's AGENTD_GIT_BASE names), the base of a
// goal's task (W-31 2.2b). Without the delivery service there is none.
func (s *Server) agentRuntimeMains() agentrtapi.MainHead {
	if s.delivery == nil {
		return nil
	}
	access := s.delivery.GiteaAccess
	client := &http.Client{Timeout: 10 * time.Second}
	return func(ctx context.Context, repo string) (string, error) {
		acc, err := access(ctx)
		if err != nil {
			return "", fmt.Errorf("gitea access: %w", err)
		}
		return gitread.BranchHead(gitAccess(access), acc.Org, client)(ctx, repo, "refs/heads/main")
	}
}

// The readers agentRuntimeSources takes; a nil one reads nothing.
type (
	approvalReader interface {
		Lookup(ref string) (approvals.Approval, bool, error)
	}
	currentDeployment = deploysource.Deployments
)

// agentRuntimeSources reads the records the runtime does not own from the
// platform's own stores (design v5 §5.2 外部事实), each without side effects:
// approvals and questions by Lookup (no sweep), the CI verdict from ci (the
// evidence ledger's reader, wireEvidenceGate), and the deployment from the
// release records. A record that cannot be read now is
// ok=false: the controller records nothing for it, a fact seen before stays
// seen, and gates_passed on the deployment is unknown.
func agentRuntimeSources(ap approvalReader, question func(context.Context, string) (seenfacts.Question, bool), ci delivery.CIReader, rel currentDeployment) controller.Sources {
	src := controller.Sources{
		Approval: func(context.Context, int64) (seenfacts.ApprovalFact, bool) { return seenfacts.ApprovalFact{}, false },
		Question: func(context.Context, string) (seenfacts.Question, bool) { return seenfacts.Question{}, false },
		CI:       func(context.Context, string, string) (string, string, bool) { return "", "", false },
		Deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false },
	}
	if ap != nil {
		src.Approval = approvalfacts.ByNumber(ap.Lookup)
	}
	if question != nil {
		src.Question = question
	}
	if ci != nil {
		src.CI = func(ctx context.Context, repo, commit string) (string, string, bool) {
			status, run, err := ci(ctx, repo, commit)
			if err != nil || status == "" {
				return "", "", false
			}
			return status, run, true
		}
	}
	if rel != nil {
		src.Deployed = deploysource.Deployed(rel)
	}
	return src
}
