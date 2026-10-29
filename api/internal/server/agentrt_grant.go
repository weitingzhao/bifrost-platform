package server

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// Landing grants' approval side (W-31 round 4, core block C-5;
// DESIGN-coordinator-landing-v3-2026-10-29.md §5.6, §12.2). Two D-tier
// actions, PROD only: approve_guardrail_landing and approve_core_landing.
// The create check refuses a request whose material is not the server's own
// recomputation; the executor writes the grant only after reading the
// approval record back from the approval service. Nothing here pushes: the
// final gate consumes the grant (store.AuthorizeLandingPush).

// writeLandingGrant is the store half; a variable so the approval tests can
// observe what the executor verified without a landing world.
var writeLandingGrant = store.WriteLandingGrant

// landingGrantSources are the Git and CI reads of a grant's material in this
// process: the lander identity and the CI client. Incomplete (every grant
// read refuses) where the process has no lander identity or no cluster.
func (s *Server) landingGrantSources() store.GrantSources {
	var src store.GrantSources
	rt := s.agentRuntime
	if rt == nil || rt.env != "prod" || s.cluster == nil {
		return src
	}
	g, err := landinggit.LanderFromEnv("prod")
	if err != nil {
		return src
	}
	ci := s.landingCIClient()
	src.Facts = controller.DrillGitFacts(g)
	src.Facts.GrantPatch = landingGrantPatch(g)
	src.Facts.GrantCI = ci.GrantCI // the strict read of the pinned key (review 694 B-01)
	src.Main = func(ctx context.Context) (string, error) { return g.Ref(ctx, "refs/heads/main") }
	return src
}

func landingGrantPatch(g *landinggit.Git) func(context.Context, string, string, string) (store.GrantPatch, error) {
	return func(ctx context.Context, a, m0, r string) (store.GrantPatch, error) {
		p, err := g.CandidatePatch(ctx, a, m0, r)
		return store.GrantPatch{SHA256: p.SHA256, Shown: p.Shown, Truncated: p.Truncated, Size: p.Size, Match: p.Match}, err
	}
}

// landingGrantRequest decodes and binds a grant request to this platform:
// PROD home, PROD runtime, a supported repository (§5.6 item 2).
func (s *Server) landingGrantRequest(action string, params map[string]any) (store.GrantParams, error) {
	if !slices.Contains(store.GrantActions, action) {
		return store.GrantParams{}, fmt.Errorf("%s is not a landing grant action", action)
	}
	p, err := store.DecodeGrantParams(params)
	if err != nil {
		return p, err
	}
	if !approvalhome.IsProd() {
		return p, errors.New(action + " is PROD only: " + approvalhome.Required)
	}
	rt := s.agentRuntime
	if rt == nil || rt.api == nil || rt.api.DB == nil {
		return p, errors.New("this platform runs no agent runtime")
	}
	if rt.env != "prod" {
		return p, fmt.Errorf("this platform runs the %q runtime; landing grants are PROD only", rt.env)
	}
	return p, nil
}

// landingGrantCreateCheck refuses, before the approval row exists, a request
// the executor would refuse (§5.6 item 2): the store half recomputes the
// material. At most one open request per operation is the approval
// service's own atomic rule (landingGrantOpenKey).
func (s *Server) landingGrantCreateCheck(ctx context.Context, action string, params map[string]any) error {
	p, err := s.landingGrantRequest(action, params)
	if err != nil {
		return err
	}
	_, err = store.CheckLandingGrantRequest(ctx, s.agentRuntime.api.DB, s.landingGrantSources(), action, p)
	return err
}

// grantApproval reads the approval this execution is for back from the
// approval service and checks it is what may write a grant (§5.6 item 4,
// §12.2 item 5): the context alone proves nothing, since any code in this
// process can build it (actions.WithCreateAttempt is exported).
func (s *Server) grantApproval(ctx context.Context, action string, params map[string]any) (store.GrantApproval, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return store.GrantApproval{}, errors.New(action + " runs only as an approval's execution")
	}
	if s.approvals == nil {
		return store.GrantApproval{}, errors.New("no approval service to read the approval back from")
	}
	rec, found, err := s.approvals.Lookup(att.ApprovalID)
	if err != nil {
		return store.GrantApproval{}, err
	}
	hash, err := actions.ParamsHash(params)
	if err != nil {
		return store.GrantApproval{}, err
	}
	ctxHash, _ := actions.ParamsHashFrom(ctx)
	if !found {
		return store.GrantApproval{}, fmt.Errorf("approval %s does not exist", att.ApprovalID)
	}
	if err := checkGrantRecord(rec, action, hash, ctxHash, approvalhome.IsProd()); err != nil {
		return store.GrantApproval{}, err
	}
	return store.GrantApproval{ID: rec.ID, Number: int64(rec.Number), Action: rec.Action, ParamsHash: rec.ParamsHash, ApprovedAt: rec.DecidedAt}, nil
}

// checkGrantRecord is the read-back record's check (§5.6 item 4, §12.2
// item 5): the action executed, tier D, running now, the parameters it
// was approved with (stored hash, the execution's hash and the executed
// parameters' hash all equal), not a drill, approved on the Console with
// its number confirmed, decided, and PROD's.
func checkGrantRecord(rec approvals.Approval, action, hash, ctxHash string, prod bool) error {
	switch {
	case rec.Action != action:
		return fmt.Errorf("approval %s is for %s, not %s", rec.ID, rec.Action, action)
	case rec.Tier != string(actions.TierD):
		return fmt.Errorf("approval %s is tier %s, not D", rec.ID, rec.Tier)
	case rec.Status != approvals.StatusRunning:
		return fmt.Errorf("approval %s is %s, not running", rec.ID, rec.Status)
	case hash == "" || rec.ParamsHash != hash || ctxHash != hash:
		return fmt.Errorf("approval %s's params_hash does not match the parameters executed", rec.ID)
	case rec.Drill:
		return fmt.Errorf("approval %s is a drill", rec.ID)
	case rec.Channel != "console":
		return fmt.Errorf("approval %s was approved on %q; a landing grant needs the Console", rec.ID, rec.Channel)
	case rec.Number < 1 || rec.ConfirmedNumber != rec.Number:
		return fmt.Errorf("approval %s was approved without its number confirmed", rec.ID)
	case rec.DecidedAt.IsZero():
		return fmt.Errorf("approval %s has no decision time", rec.ID)
	case !prod || rec.Params["env"] != "prod":
		return errors.New("a landing grant's approval is PROD's: " + approvalhome.Required)
	}
	return nil
}

// landingGrantExecutor is the catalog executor of one grant action.
func (s *Server) landingGrantExecutor(action string) actions.Executor {
	return func(ctx context.Context, params map[string]any) (any, error) {
		p, err := s.landingGrantRequest(action, params)
		if err != nil {
			return nil, err
		}
		a, err := s.grantApproval(ctx, action, params)
		if err != nil {
			return nil, err
		}
		g, err := writeLandingGrant(ctx, s.agentRuntime.api.DB, s.landingGrantSources(), a, p)
		if err != nil {
			if errors.Is(err, store.ErrGrantUncertain) {
				return nil, actions.Uncertain(err.Error() + "; the Owner checks the operation's grants on its material page (the stream decides)")
			}
			return nil, err
		}
		return map[string]any{"grant_id": g.GrantID, "op": g.Op, "R": g.R, "waives": g.Waives, "expires_at": g.ExpiresAt, "stg_release": g.STGRelease}, nil
	}
}

// landingGrantOpenKey: a landing grant request is for its operation, and an
// operation has at most one open request at a time (§5.4, review 694 R-02),
// whichever of the two actions it is.
func landingGrantOpenKey(action string, params map[string]any) string {
	if !slices.Contains(store.GrantActions, action) {
		return ""
	}
	op, _ := params["op"].(string)
	if op == "" {
		return ""
	}
	return "landing-grant:" + op
}
