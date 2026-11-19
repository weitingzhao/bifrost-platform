package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
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
	if s.delivery == nil || (!headrelease.On() && !headrelease.ProdShadowOn()) {
		return nil
	}
	table, path, err := headrelease.LoadTable()
	if err != nil {
		slog.Error("head release: the table does not load; nothing is released", "path", path, "error", err)
		return nil
	}
	releasing := headrelease.On()
	// The shadow of PROD by batch (prod_batch_shadow.go): nil unless
	// PROD_HEAD_RELEASE=shadow. It records and does nothing else.
	shadow := s.prodShadowOf(table)
	slog.Info("head release: on", "path", path, "deliverables", len(table.Deliverables), "releasing", releasing, "prod_shadow", shadow != nil)
	return headrelease.Pass(headrelease.PassDeps{
		Table: table, Releasing: releasing, WorldOf: s.headWorldOf, Shadow: shadow,
		Record: s.recordHeadDecision,
	})
}

func (s *Server) recordHeadDecision(ctx context.Context, d headrelease.Deliverable, out headrelease.Outcome) {
	rec := headrelease.RecordOf(ctx, d, out, s.headBrought)
	detail, err := json.Marshal(rec)
	if err != nil {
		detail = []byte(out.Action + ": " + out.Why)
	}
	slog.Info("head release", "deliverable", out.Deliverable, "action", out.Action, "why", out.Why, "run", out.Run, "record", string(detail))
	s.audit.RecordDirect(headrelease.Principal, actuation.RoleOperator, "head_release", "deliverable/"+out.Deliverable, out.Action, string(detail))
	t := newActionTrace("start_pipeline_run", "system_head", map[string]any{"deliverable": out.Deliverable, "commits": out.Commits})
	t.Initiator = map[string]any{"internal_principal": headrelease.Principal, "external_authenticated_requester": traceUnavailable}
	t.Reason, t.Unchecked, t.Rollback = out.Why, traceUnavailable, traceUnavailable
	t.Decision = rec
	t.Result = map[string]any{"action": out.Action, "run": out.Run}
	s.recordActionTrace(ctx, t)
}

// headBrought lists the commits to has and from lacks, newest first, each
// with its lineage trailers, from Gitea (headrelease.Brought).
func (s *Server) headBrought(ctx context.Context, repo, from, to string) ([]headrelease.BroughtCommit, int, string) {
	return headrelease.Brought(ctx, gitAccess(s.delivery.GiteaAccess), repo, from, to)
}

// GET /api/v1/delivery/released?repo=…&commit=… says, for every deliverable
// of headrelease/deliverables.yaml that holds the repository, what its environment
// runs and whether that has the commit: so a line asks "is my commit
// released" instead of guessing a run's name (ci-feedback 2026-11-13).
// Read-only; viewer and above.
func (s *Server) handleReleased(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name, commit a 40-character lowercase SHA"})
		return
	}
	world := s.headWorldOf()
	if world == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the delivery service or the evidence ledger is not connected"})
		return
	}
	table, _, err := headrelease.LoadTable()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	facts := s.delivery.PolicyFacts()
	within := func(ctx context.Context, repo, ancestor, commit string) (bool, error) {
		if ancestor == commit {
			return true, nil
		}
		// ChangedFiles fails when ancestor is not in commit's history, and
		// when it cannot tell (the mirror is down, the walk is too long):
		// neither is "released".
		_, diffErr := facts.ChangedFiles(ctx, repo, ancestor, commit)
		return diffErr == nil, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	released, err := headrelease.WhereReleased(ctx, world, table, repo, commit, within)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if released == nil {
		released = []headrelease.Released{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": repo, "commit": commit, "head_release": headrelease.On(), "released": released})
}
