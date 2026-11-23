package releasepolicy

import (
	"context"
	"strings"
)

// CheckAnswer is the answer of GET /delivery/pipelines/{name}/policy-check:
// what the direct start_pipeline_run route would decide for one request at
// this moment, with every reason. Until 2026-11-22 the server judged this
// itself (server/release_policy_wire.go); the judgment is the policy's, so it
// lives here and the server only reads the request.
type CheckAnswer struct {
	Action        string            `json:"action"`
	Pipeline      string            `json:"pipeline"`
	Tier          Tier              `json:"tier"`
	NeedsApproval bool              `json:"needs_approval"`
	Covered       bool              `json:"covered"`
	PolicyID      string            `json:"policy_id,omitempty"`
	Reasons       []string          `json:"reasons"`
	Clauses       []string          `json:"clauses"`
	SHAs          map[string]string `json:"shas"`
	AdditiveDDL   []string          `json:"additive_ddl,omitempty"`
	ReversibleDDL []string          `json:"ddl_reversible,omitempty"`
	Reversal      *Reversal         `json:"reversal,omitempty"`
	Outcome       string            `json:"outcome"`
}

// ReleasePipeline is a pipeline that ships something: deliver and image
// builds. CI and smoke pipelines are not releases (they ship nothing).
func ReleasePipeline(name string) bool {
	return strings.HasPrefix(name, "bifrost-deliver-") || strings.HasPrefix(name, "bifrost-build-")
}

// Check judges a request the way the direct route does: Decide for a tier
// that needs approval, Evaluate for one that does not (a tier B release runs
// either way unless releases are frozen). It starts nothing and records
// nothing. needsApproval is the action catalog's answer for tier.
func (e *Engine) Check(ctx context.Context, actionID string, tier Tier, needsApproval bool, pipeline string, params map[string]any) CheckAnswer {
	var d Decision
	if needsApproval {
		d = e.Decide(ctx, actionID, tier, params)
	} else {
		d = e.Evaluate(ctx, actionID, params)
	}
	out := CheckAnswer{
		Action: actionID, Pipeline: pipeline, Tier: tier, NeedsApproval: needsApproval, Covered: d.Auto,
		PolicyID: d.PolicyID, Reasons: d.Reasons, Clauses: d.Clauses, SHAs: d.SHAs, AdditiveDDL: d.AdditiveDDL, ReversibleDDL: d.ReversibleDDL, Reversal: d.Reversal,
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
	case needsApproval && d.Auto:
		out.Outcome = "the direct call would start the run: the signed policy covers it"
	case needsApproval:
		out.Outcome = "the direct call would answer 403 approval required with these reasons and create nothing; the run then needs an approval request"
	default:
		out.Outcome = "tier " + string(tier) + " runs without an approval either way; covered says what the policy would have decided"
		if frozen, reason := e.FreezeSet(ctx); frozen && ReleasePipeline(pipeline) {
			out.Outcome = "the direct call would be refused: releases are frozen (" + reason + ")"
		}
	}
	return out
}
