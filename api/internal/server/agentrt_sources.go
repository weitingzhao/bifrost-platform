package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/approvalfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deploysource"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planloop"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// What the agent runtime reads of the platform's stores lives in the
// agentrt packages (deploysource, approvalfacts, runfacts, gitread, readslot);
// this file names them for the rest of the server and builds the plan loop's
// sources from them (the controller's are agentrt_external_records.go's).

// agentrtDeployRepo is the repository whose deployments the runtime reads.
const agentrtDeployRepo = deploysource.Repo

var (
	giteaName = gitread.Name
	commitID  = gitread.CommitID
)

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
func (s *Server) agentRuntimeMains() mainHead {
	if s.delivery == nil {
		return nil
	}
	return gitread.MainHeads(gitAccess(s.delivery.GiteaAccess), &http.Client{Timeout: 10 * time.Second})
}

// The readers agentRuntimeSources takes; a nil one reads nothing.
type (
	approvalReader interface {
		Lookup(ref string) (approvals.Approval, bool, error)
		Observations() ([]approvals.Approval, error)
	}
	currentDeployment = deploysource.Deployments
)

// agentRuntimeSources reads the records the runtime does not own from the
// platform's own stores (design v5 §5.2 外部事实), each without side effects:
// approvals and questions by Lookup (no sweep), the CI verdict from ci (the
// evidence ledger's reader, wireEvidenceGate), and the deployment from the
// release records. A record that cannot be read now is
// ok=false: the plan loop records nothing for it, a fact seen before stays
// seen, and gates_passed on the deployment is unknown.
func agentRuntimeSources(ap approvalReader, question func(context.Context, string) (seenfacts.Question, bool), ci delivery.CIReader, rel currentDeployment) planloop.Sources {
	src := planloop.Sources{
		Approval: func(context.Context, int64) (seenfacts.ApprovalFact, bool) { return seenfacts.ApprovalFact{}, false },
		Question: func(context.Context, string) (seenfacts.Question, bool) { return seenfacts.Question{}, false },
		CI:       func(context.Context, string, string) (string, string, bool) { return "", "", false },
		Deployed: func(context.Context, string) (string, map[string]any, bool) { return "", nil, false },
	}
	if ap != nil {
		src.Approval = approvalfacts.ByNumber(ap.Lookup)
		// The executed law_amend cards the store holds, as the plan's law
		// cards, their params read by the approvals' own reader
		// (planstore.ReadLawAmend, legislation's); a card that does not read is skipped, since
		// the store refused it at creation.
		src.Laws = func(context.Context) ([]planstore.LawCard, bool) {
			all, err := ap.Observations()
			if err != nil {
				return nil, false
			}
			var out []planstore.LawCard
			for _, a := range all {
				if a.Action != actions.LawAmendAction || a.Status != approvals.StatusExecuted {
					continue
				}
				la, err := planstore.ReadLawAmend(a.Params)
				if err != nil {
					continue
				}
				c := planstore.LawCard{ApprovalID: a.ID, Number: a.Number, Op: la.Op, Law: la.Law, After: la.After, Before: la.Before}
				if !a.DecidedAt.IsZero() {
					c.DecidedOn = a.DecidedAt.UTC().Format("2006-01-02")
				}
				c.Standards, c.Repeals = la.Standards, la.Repeals
				out = append(out, c)
			}
			return out, true
		}
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
