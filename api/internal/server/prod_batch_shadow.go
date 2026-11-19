package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// The shadow of "the platform releases PROD in batches": the judgement is in
// headrelease/prod_shadow.go and what it reads is headrelease.ProdReads (each
// one a read; the one thing it writes is the audit record below). The shadow
// has no hands; this file hands ProdReads what it reads from.

// The policy is asked about this action and the cards are filtered by it;
// the shadow never performs it.
const prodShadowAsksAbout = "start_pipeline_run"

// prodShadowOf builds the shadow, or nil: the switch is not shadow, the table
// lacks one of its two rows, or this process has no delivery service.
func (s *Server) prodShadowOf(table headrelease.Table) *headrelease.ProdShadowLoop {
	mode := headrelease.ProdShadowMode()
	if mode != headrelease.ProdShadow || s.delivery == nil {
		return nil
	}
	var prod, source *headrelease.Deliverable
	for i := range table.Deliverables {
		switch table.Deliverables[i].Name {
		case headrelease.ProdRowName:
			prod = &table.Deliverables[i]
		case headrelease.ProdSourceName:
			source = &table.Deliverables[i]
		}
	}
	if prod == nil || source == nil {
		slog.Error("prod batch shadow: the table lacks its rows; nothing is recorded", "prod", headrelease.ProdRowName, "source", headrelease.ProdSourceName)
		return nil
	}
	soak := headrelease.Seconds(os.Getenv(headrelease.ProdSoakEnv), headrelease.DefaultProdSoak)
	gap := headrelease.Seconds(os.Getenv(headrelease.ProdGapEnv), headrelease.DefaultProdGap)
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
	repo := ""
	for _, c := range prod.Commits {
		if c.TestedWith == "" && repo == "" {
			repo = c.Repo
		}
	}
	slog.Info("prod batch shadow: on (records only)", "deliverable", prod.Name, "candidate_from", source.Name, "soak", soak.String(), "gap", gap.String())
	return &headrelease.ProdShadowLoop{
		Mode: mode,
		// Five minutes. Each judgement asks the release records what STG and
		// what PROD run, and each of those answers lists every run of the
		// pipelines' namespace a hundred a page: on 2026-11-17, with 2,058 runs
		// kept, one judgement a minute was 327 LISTs in ten minutes under
		// head-release's name (the communication line's count). The records
		// move to a table next; until then the shadow asks less often, and a
		// decision that is five minutes late is still a shadow's decision.
		Every: 5 * time.Minute,
		Gather: func(ctx context.Context) headrelease.ProdFacts {
			return reads.Gather(kubemetrics.WithSite(ctx, "prod-shadow"), *prod, *source, soak, gap)
		},
		Batch: func(ctx context.Context, from, to string) headrelease.ProdBatch {
			return s.prodShadowBatch(ctx, repo, from, to)
		},
		Record: func(result, detail string) {
			slog.Info("prod batch shadow", "deliverable", prod.Name, "result", result, "record", detail)
			s.audit.RecordDirect(headrelease.ProdShadowPrincipal, actuation.RoleOperator, headrelease.ProdRecordAction, "deliverable/"+prod.Name, result, detail)
		},
	}
}

// prodShadowBatch counts the commits to has and from lacks with the one
// Gitea request head-release's own record uses, and reads their Work
// trailers from the same answer.
func (s *Server) prodShadowBatch(ctx context.Context, repo, from, to string) headrelease.ProdBatch {
	return headrelease.ProdBatchFrom(s.headBrought(ctx, repo, from, to))
}
