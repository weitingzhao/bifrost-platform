package server

import (
	"context"
	"encoding/json"
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

// releasePolicyNotice is owner-written. It has no fallback: a hidden
// character in it is posted as the stock withheld title.
func releasePolicyNotice(title, message string) approvalnotify.Message {
	return approvalnotify.Message{Title: title, Message: message, Kind: "release_policy"}
}

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
				return approvalnotify.Notify(ctx, releasePolicyNotice(title, message))
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
	if t := traceFrom(r.Context()); t != nil {
		t.Decision = map[string]any{"mode": "signed_policy", "decision": d}
	}
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
	if t := traceFrom(r.Context()); t != nil {
		t.Decision = map[string]any{"mode": "direct_tier_b", "decision": d}
	}
	if d.PolicyID != "" && s.audit != nil {
		result := "not covered"
		if d.Auto {
			result = "covered"
		}
		s.audit.Record(r, "release_policy.check", id+" "+name, result, decisionDetail(r, d))
	}
	return false
}

// policyCheck is the answer of GET /delivery/pipelines/{name}/policy-check.
type policyCheck struct {
	Action        string            `json:"action"`
	Pipeline      string            `json:"pipeline"`
	Tier          actions.Tier      `json:"tier"`
	NeedsApproval bool              `json:"needs_approval"`
	Covered       bool              `json:"covered"`
	PolicyID      string            `json:"policy_id,omitempty"`
	Reasons       []string          `json:"reasons"`
	Clauses       []string          `json:"clauses"`
	SHAs          map[string]string `json:"shas"`
	AdditiveDDL   []string          `json:"additive_ddl,omitempty"`
	Outcome       string            `json:"outcome"`
}

// handleReleasePolicyCheck judges one start_pipeline_run request against the
// signed policy and answers what the direct route would decide, with every
// reason. It starts nothing, creates no approval and writes no audit record.
// The query carries what the run request's body would: revision, tag, who,
// and params as a JSON object. The parameters go through the action's own
// Extract and the decision through the calls guard makes (Decide for a tier
// that needs approval, Evaluate for tier B), so the answer is the direct
// route's answer for the same request at this moment; the window, the newest
// records and CI can all change before the real call.
func (s *Server) handleReleasePolicyCheck(w http.ResponseWriter, r *http.Request) {
	const id = "start_pipeline_run"
	if s.releasePolicy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "release policy not wired"})
		return
	}
	act, ok := actions.ByID(id)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": id + " is not in the action catalog"})
		return
	}
	q := r.URL.Query()
	body := map[string]any{}
	for _, key := range []string{"revision", "tag", "who"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			body[key] = v
		}
	}
	if raw := strings.TrimSpace(q.Get("params")); raw != "" {
		var inner map[string]string
		if err := json.Unmarshal([]byte(raw), &inner); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "params must be a JSON object of strings"})
			return
		}
		body["params"] = inner
	}
	raw, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	params, err := act.Extract(r, raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if missing := act.Missing(params); missing != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing " + missing})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tierBPolicyTimeout)
	defer cancel()
	tier := act.TierOf(ctx, params)
	name := strings.TrimSpace(fmt.Sprint(params["name"]))
	var d releasepolicy.Decision
	if tier.NeedsApproval() {
		d = s.releasePolicy.Decide(ctx, id, tier, params)
	} else {
		d = s.releasePolicy.Evaluate(ctx, id, params)
	}
	out := policyCheck{
		Action: id, Pipeline: name, Tier: tier, NeedsApproval: tier.NeedsApproval(), Covered: d.Auto,
		PolicyID: d.PolicyID, Reasons: d.Reasons, Clauses: d.Clauses, SHAs: d.SHAs, AdditiveDDL: d.AdditiveDDL,
	}
	if out.Reasons == nil {
		out.Reasons = []string{}
	}
	if out.Clauses == nil {
		out.Clauses = []string{}
	}
	if out.SHAs == nil {
		out.SHAs = map[string]string{}
	}
	switch {
	case tier.NeedsApproval() && d.Auto:
		out.Outcome = "the direct call would start the run: the signed policy covers it"
	case tier.NeedsApproval():
		out.Outcome = "the direct call would answer 403 approval required with these reasons and create nothing; the run then needs an approval request"
	default:
		out.Outcome = "tier " + string(tier) + " runs without an approval either way; covered says what the policy would have decided"
		if frozen, reason := s.releasePolicy.FreezeSet(ctx); frozen && releasePipeline(name) {
			out.Outcome = "the direct call would be refused: releases are frozen (" + reason + ")"
		}
	}
	writeJSON(w, http.StatusOK, out)
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
