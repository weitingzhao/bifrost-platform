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
		Writer:     facts,
		Git:        facts,
		CI:         facts,
		Window:     facts,
		Deployed:   recordDeployed{svc: releasesSvc},
		StatePath:  filepath.Join(dataDir, "release-policy-freeze"),
		Anchor:     releasepolicy.OwnerKeyFingerprint,
	})
	s.approvals.SetAutoApprover(s.releasePolicyApprover())
	if runsWorkers && releasepolicy.RemindersWanted() {
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
			"auto-approved "+decisionDetail(r, d))
	}
	return true, nil
}

func decisionDetail(r *http.Request, d releasepolicy.Decision) string {
	out := fmt.Sprintf("policy_id=%s requester=%s sha=%s clauses=%s", d.PolicyID, policyRequester(r), d.SHAText(), d.ClauseText())
	if len(d.AdditiveDDL) > 0 {
		out += " additive_ddl=" + strings.Join(d.AdditiveDDL, ",")
	}
	if len(d.Reasons) > 0 {
		out += " reasons=" + strings.Join(d.Reasons, "; ")
	}
	return out
}

// releasePipeline is a pipeline that ships something: deliver and image
// builds. CI and smoke pipelines are not releases.
func releasePipeline(name string) bool {
	return strings.HasPrefix(name, "bifrost-deliver-") || strings.HasPrefix(name, "bifrost-build-")
}

const tierBPolicyTimeout = 20 * time.Second

// checkTierBRelease runs before a tier B start_pipeline_run. A freeze that
// someone set refuses it (true: the response is written). Otherwise, while a
// signed policy is in force, the policy is evaluated and audited, and the run
// goes ahead either way: tier B never needed an approval, and the STG release
// is where the policy's facts are first exercised before PROD relies on them.
func (s *Server) checkTierBRelease(w http.ResponseWriter, r *http.Request, id string, params map[string]any) bool {
	name := strings.TrimSpace(fmt.Sprint(params["name"]))
	if s.releasePolicy == nil || id != "start_pipeline_run" || !releasePipeline(name) {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), tierBPolicyTimeout)
	defer cancel()
	if frozen, reason := s.releasePolicy.FreezeSet(ctx); frozen {
		if s.audit != nil {
			s.audit.Record(r, "release_policy.frozen", id+" "+name, "refused", reason)
		}
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":    "releases are frozen",
			"action":   id,
			"freeze":   reason,
			"unfreeze": releasepolicy.UnfreezeCommand,
		})
		return true
	}
	d := s.releasePolicy.Evaluate(ctx, id, params)
	if d.PolicyID != "" && s.audit != nil {
		result := "not covered"
		if d.Auto {
			result = "covered"
		}
		s.audit.Record(r, "release_policy.check", id+" "+name, result, decisionDetail(r, d))
	}
	return false
}

func (s *Server) auditRecorder() releasepolicy.Audit {
	return func(r *http.Request, action, target, result, detail string) {
		if s.audit != nil {
			s.audit.Record(r, action, target, result, detail)
		}
	}
}

func (s *Server) handleReleasePolicyInstall(w http.ResponseWriter, r *http.Request) {
	if s.releasePolicy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "release policy not wired"})
		return
	}
	s.releasePolicy.HandleInstall(s.auditRecorder())(w, r)
}

func (s *Server) handleReleaseFreeze(w http.ResponseWriter, r *http.Request) {
	if s.releasePolicy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "release policy not wired"})
		return
	}
	s.releasePolicy.HandleFreeze(s.auditRecorder())(w, r)
}

func (s *Server) handleReleaseUnfreeze(w http.ResponseWriter, r *http.Request) {
	if s.releasePolicy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "release policy not wired"})
		return
	}
	s.releasePolicy.HandleUnfreeze(s.auditRecorder())(w, r)
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
