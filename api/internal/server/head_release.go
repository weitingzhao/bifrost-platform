package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The platform releases main's head by itself (internal/headrelease has the
// judgement, the world it reads and the loop's pass); this file hands them
// what they stand on: Gitea, the evidence ledger, the cluster, the bus, the
// approvals, and the places a decision is recorded.

// headWorldOf builds the world of one pass, or nil when this process has no
// delivery service or no ledger.
func (s *Server) headWorldOf() *headrelease.Live {
	if s.delivery == nil || s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
		return nil
	}
	db := func() eventlog.DB { return s.agentRuntime.api.DB }
	tree := giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})
	start := func(ctx context.Context, principal, pipeline, revision string, params map[string]string) (string, string, error) {
		run, heldBy, err := s.startHeadRelease(ctx, principal, pipeline, revision, params)
		return run.Name, heldBy, err
	}
	return headrelease.NewLive(headrelease.LiveDeps{
		Head: s.agentRuntimeMains(),
		CI:   evidenceread.CIBeside(evidenceread.LedgerCI(db, tree), db),
		Runs: func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error) {
			// The bus run cache when it is synced: a pass, and the PROD
			// shadow once a minute, read it instead of listing every run in
			// cicd (2026-11-17: ~470 LISTs in 10 minutes). The start itself
			// reads the cluster (StartDeliveryIfFree), so a stale cache cannot
			// start a second run beside an unfinished one.
			if items, ok := s.delivery.CachedRunsOf(pipeline); ok {
				readerseen.Saw("head-release")
				return items, nil
			}
			return s.delivery.PipelineRunsOf(kubemetrics.WithSite(ctx, "head-release"), pipeline)
		},
		Start: start,
		Frozen: func(ctx context.Context) (bool, string) {
			if s.releasePolicy == nil {
				return false, ""
			}
			return s.releasePolicy.FreezeSet(ctx)
		},
		Approved: func() (string, error) {
			if s.approvals == nil {
				return "", nil
			}
			items, err := s.approvals.Observations()
			if err != nil {
				return "", err
			}
			return approvalreadings.ApprovedReleaseWaiting(items, time.Now()), nil
		},
	})
}

// startHeadRelease starts a head-release run naming, as its cause, the newest
// bus event worth a pass (delivery.LabelCause; 2026-11-17, the communication
// layer: git.main.advanced -> the run -> release.deploy.finished could not be
// followed, no event carried a cause). A run started by the loop's own
// reconciliation, with no wake since the last start, names none.
func (s *Server) startHeadRelease(ctx context.Context, principal, pipeline, revision string, params map[string]string) (delivery.PipelineRunView, string, error) {
	cause := s.bus.head.LastWake()
	run, heldBy, err := s.delivery.StartDeliveryIfFree(delivery.WithCause(ctx, cause, s.bus.head.WakeEnv()), principal, pipeline, revision, params)
	if err == nil && run.Name != "" && cause != "" {
		s.bus.head.ClearWake(cause)
	}
	return run, heldBy, err
}

// headReleaseSource is the loop's pass, or nil when the switch is off.
func (s *Server) headReleaseSource() func(context.Context) error {
	if s.delivery == nil {
		return nil
	}
	return headrelease.Source(s.headHands())
}

// headHands lends the head-release loop what it reaches outside delivery:
// this process's audit log and action trace, Gitea, the delivery start, the
// PROD shadow's reads, the daily full CI and the ledger. The loop's own work
// (records, the shadow, the PROD release) is in headrelease (Source).
func (s *Server) headHands() headrelease.Hands {
	return headrelease.Hands{
		WorldOf: s.headWorldOf,
		Git:     gitAccess(s.delivery.GiteaAccess),
		Audit: func(principal, action, target, result, detail string) {
			s.audit.RecordDirect(principal, actuation.RoleOperator, action, target, result, detail)
		},
		Trace: func(ctx context.Context, action, mode, principal, why string, params map[string]any, decision, result any) {
			s.tracer().RecordSystem(ctx, action, mode, principal, why, params, decision, result)
		},
		Start: func(ctx context.Context, principal, pipeline, revision string, params map[string]string) (string, string, error) {
			run, heldBy, err := s.startHeadRelease(ctx, principal, pipeline, revision, params)
			return run.Name, heldBy, err
		},
		ShadowReads: s.prodShadowReads,
		DB: func() eventlog.DB {
			if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
				return nil
			}
			return s.agentRuntime.api.DB
		},
		// Read when called: wireFullCI sets fullCIRun on PROD's controller,
		// which may come after this loop is built.
		FullCI: func(ctx context.Context, revision string) (string, error) {
			if s.fullCIRun == nil {
				return "", errors.New("the daily full CI is not wired in this process")
			}
			return s.fullCIRun(ctx, revision)
		},
		Zone: prodReleaseZone(),
	}
}

// handleReleased is GET /api/v1/delivery/released (headrelease.ReleasedRoute).
func (s *Server) handleReleased(w http.ResponseWriter, r *http.Request) {
	headrelease.ReleasedRoute{RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString, World: s.headWorldOf,
		ChangedFiles: func(ctx context.Context, repo, from, to string) ([]string, error) {
			return s.delivery.PolicyFacts().ChangedFiles(ctx, repo, from, to)
		}}.ServeHTTP(w, r)
}
