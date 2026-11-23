package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// The shadow of "the platform releases PROD in batches": the judgement is in
// headrelease/prod_shadow.go, when it runs in headrelease/prod_shadow_build.go
// and what it reads is headrelease.ProdReads (each one a read; the one thing
// it writes is the audit record). The shadow has no hands; this file hands it
// what it reads from.

// The policy is asked about this action and the cards are filtered by it;
// the shadow never performs it.
const prodShadowAsksAbout = "start_pipeline_run"

// prodShadowOf builds the shadow, or nil: this process has no delivery
// service, or headrelease.NewProdShadow answers nil.
func (s *Server) prodShadowOf(table headrelease.Table) *headrelease.ProdShadowLoop {
	if s.delivery == nil {
		return nil
	}
	return headrelease.NewProdShadow(table, headrelease.ProdShadowHands{
		Reads: s.prodShadowReads,
		Batch: s.prodShadowBatch,
		Audit: func(target, result, detail string) {
			s.audit.RecordDirect(headrelease.ProdShadowPrincipal, actuation.RoleOperator, headrelease.ProdRecordAction, target, result, detail)
			// The ledger keeps the reasons the audit event does not (migration 0043).
			if s.agentRuntime != nil && s.agentRuntime.api != nil && s.agentRuntime.api.DB != nil {
				headrelease.RecordProdJudged(s.agentRuntime.api.DB, result, detail)
			}
		},
		// Mode on's one act and its hours (prod_batch_release.go).
		Release: s.prodRelease,
		// A candidate that waits only on its full CI gets one, from the daily
		// full CI's own runner (wireFullCI sets fullCIRun on PROD's controller).
		FullCI: func(ctx context.Context, revision string) (string, error) {
			if s.fullCIRun == nil {
				return "", errors.New("the daily full CI is not wired in this process")
			}
			return s.fullCIRun(ctx, revision)
		},
		Zone: prodReleaseZone(),
	})
}

// prodShadowReads joins the shadow's reads to this process's services.
func (s *Server) prodShadowReads() headrelease.ProdReads {
	facts := s.delivery.PolicyFacts()
	db := func() eventlog.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}
	reads := headrelease.ProdReads{
		Now:    time.Now,
		CI:     evidenceread.LedgerCIStrict(db, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})),
		Window: facts.WindowHolder,
		Frozen: func(ctx context.Context) (bool, string) {
			if s.releasePolicy == nil {
				return false, ""
			}
			return s.releasePolicy.FreezeSet(ctx)
		},
	}
	if s.releases != nil && s.releases.Service() != nil {
		reads.Rule, reads.Current = s.releases.Service().RuleOf, s.releases.Service().Current
	}
	if s.releasePolicy != nil {
		reads.Policy = func(ctx context.Context, params map[string]any) releasepolicy.Decision {
			return s.releasePolicy.Evaluate(ctx, prodShadowAsksAbout, params)
		}
	}
	if s.approvals != nil {
		reads.Cards = func(pipeline string) ([]string, error) {
			items, err := s.approvals.Observations()
			if err != nil {
				return nil, err
			}
			return approvalreadings.OpenReleaseCards(items, prodShadowAsksAbout, pipeline), nil
		}
	}
	return reads
}

// prodShadowBatch counts the commits to has and from lacks with the one
// Gitea request head-release's own record uses, and reads their Work
// trailers from the same answer.
func (s *Server) prodShadowBatch(ctx context.Context, repo, from, to string) headrelease.ProdBatch {
	return headrelease.ProdBatchFrom(s.headBrought(ctx, repo, from, to))
}
