package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

const releasePolicyCheckInterval = time.Hour

// recordDeployed takes the deployed commits from the newest release record
// of the pipeline (releases.Service.List is newest first).
type recordDeployed struct{ svc *releases.Service }

func (d recordDeployed) Deployed(ctx context.Context, pipeline string) (map[string]string, []string, error) {
	recs, err := d.svc.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range recs {
		if r.Pipeline != pipeline {
			continue
		}
		out := make(map[string]string, len(r.Repos))
		for name, b := range r.Repos {
			out[name] = b.SHA
		}
		return out, append([]string(nil), r.Missing...), nil
	}
	return nil, nil, nil
}

// wireReleasePolicy builds the engine, hooks it into approval creation and,
// in the workers role, starts the hourly expiry reminders.
func (s *Server) wireReleasePolicy(releasesSvc *releases.Service, dataDir string, runsWorkers bool) {
	facts := s.delivery.PolicyFacts()
	s.releasePolicy = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: facts,
		Git:        facts,
		CI:         facts,
		Window:     facts,
		Deployed:   recordDeployed{svc: releasesSvc},
		StatePath:  filepath.Join(dataDir, "release-policy-freeze"),
		Anchor:     releasepolicy.OwnerKeyFingerprint,
	})
	s.approvals.SetAutoApprover(s.releasePolicyApprover())
	if runsWorkers {
		releasepolicy.NewChecker(s.releasePolicy, func() int { return pendingReleases(s.approvals) },
			func(ctx context.Context, title, message string) error {
				return approvalnotify.Notify(ctx, approvalnotify.Message{Title: title, Message: message})
			},
			filepath.Join(dataDir, "release-policy-reminders"),
		).Start(context.Background(), releasePolicyCheckInterval)
	}
}

// releasePolicyApprover answers POST /api/v1/approvals for tier C: a policy
// id sends the caller to the direct route, where guard decides again.
func (s *Server) releasePolicyApprover() approvals.AutoApprover {
	return func(ctx context.Context, action string, tier actions.Tier, params map[string]any) string {
		if s.releasePolicy == nil {
			return ""
		}
		if d := s.releasePolicy.Decide(ctx, action, tier, params); d.Auto {
			return d.PolicyID
		}
		return ""
	}
}

func pendingReleases(svc *approvals.Service) int {
	n := 0
	for _, a := range svc.Pending() {
		if a.Action == "start_pipeline_run" {
			n++
		}
	}
	return n
}

// autoApprove decides a tier C direct call on the signed policy and audits an
// approval. false means the call keeps needing a manual approval; reasons say why.
func (s *Server) autoApprove(r *http.Request, id string, tier actions.Tier, params map[string]any) (bool, []string) {
	if s.releasePolicy == nil || id != "start_pipeline_run" {
		return false, nil
	}
	d := s.releasePolicy.Decide(r.Context(), id, tier, params)
	if !d.Auto {
		return false, d.Reasons
	}
	if s.audit != nil {
		s.audit.Record(r, "release_policy.auto_approve", id+" "+d.Pipeline, "approved",
			fmt.Sprintf("auto-approved policy_id=%s requester=%s sha=%s", d.PolicyID, policyRequester(r), d.SHAText()))
	}
	return true, nil
}

func policyRequester(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(approvals.SessionHeader)); v != "" {
		return v
	}
	if name := actuation.PrincipalFromContext(r.Context()).Name; name != "" {
		return name
	}
	return "unknown"
}

func (s *Server) handleReleasePolicy(w http.ResponseWriter, r *http.Request) {
	if s.releasePolicy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "release policy not wired"})
		return
	}
	s.releasePolicy.HandleStatus(w, r)
}
