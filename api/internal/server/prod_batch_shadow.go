package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// The shadow of "the platform releases PROD in batches" (the judgement and
// why are in headrelease/prod_shadow.go; this file is its eyes). It has no
// hands: prodShadowReads below is every way it touches the world, each one a
// read, and the one thing it writes is an audit record.
//
// PROD_HEAD_RELEASE is off (the default, and what any other value means) or
// shadow. There is no "on".
const (
	prodShadowEnv         = "PROD_HEAD_RELEASE"
	prodShadowSoakEnv     = "PROD_BATCH_SOAK_SECONDS"
	prodShadowGapEnv      = "PROD_BATCH_GAP_SECONDS"
	prodShadowPrincipal   = "prod-batch-shadow"
	prodShadowPassTimeout = 30 * time.Second
	// The row of headrelease/deliverables.yaml the shadow judges, and the row
	// whose environment its candidate comes from.
	prodShadowRow    = "platform-prod"
	prodShadowSource = "platform-stg"
	// The policy is asked about this action and the cards are filtered by it;
	// the shadow never performs it.
	prodShadowAsksAbout = "start_pipeline_run"
	// The engine's reason for its window clause starts with this
	// (releasepolicy/engine.go, window_held_by_requester). The shadow holds
	// no window, so that one reason is set aside and the window's state is
	// its own condition. TestProdShadowPolicySetsOnlyTheWindowAside holds the
	// two files together.
	policyWindowReason = "release window: "
)

// prodShadowMode reads the switch. Unset, it is shadow in the process where
// head-release is on (the one that would release PROD) and off anywhere else;
// "off" turns it off there too. The coordinator asked on 2026-11-16 for a
// night of shadow records before anyone decides to turn releasing on, and the
// PROD manifest is not an agent's to change: the default ships with the code,
// through CI and the release policy, and it can do nothing but read and write
// an audit record.
func prodShadowMode() string {
	v := strings.TrimSpace(os.Getenv(prodShadowEnv))
	if v == "" && headReleaseOn() {
		return headrelease.ProdShadow
	}
	return headrelease.ProdMode(v)
}

func prodShadowOn() bool { return prodShadowMode() == headrelease.ProdShadow }

// prodShadowReads is what the shadow reads.
type prodShadowReads struct {
	now func() time.Time
	// rule is the lane and environment the release records keep a pipeline's
	// runs under; current what that environment runs by the records.
	rule    func(pipeline string) (releases.Rule, bool)
	current func(ctx context.Context, lane, env string) (releases.Record, error)
	// ci is the strict ledger verdict (the one the policy's ci_succeeded and
	// the PROD pipeline's source check read).
	ci delivery.CIReader
	// policy is the engine's evaluation of a release request; nil when no
	// engine is wired.
	policy func(ctx context.Context, params map[string]any) releasepolicy.Decision
	cards  func() ([]approvals.Approval, error)
	window func(ctx context.Context) (who, what string, open bool, err error)
	frozen func(ctx context.Context) (bool, string)
}

// gather reads the facts of this moment for one PROD row and its source row.
func (r prodShadowReads) gather(ctx context.Context, prod, source headrelease.Deliverable, soak, gap time.Duration) headrelease.ProdFacts {
	f := headrelease.ProdFacts{Now: r.now(), Soak: soak, Gap: gap}
	repo, param := "", ""
	for _, c := range prod.Commits {
		if c.TestedWith == "" && repo == "" {
			repo, param = c.Repo, c.Param
		}
	}
	var stgRecord releases.Record
	f.Prod, _ = r.deployed(ctx, prod.Pipeline, repo)
	f.Stg, stgRecord = r.deployed(ctx, source.Pipeline, repo)

	if candidate := f.Stg.Commit; candidate != "" {
		if r.ci == nil {
			f.CIErr = "the evidence ledger is not connected"
		} else if status, run, err := r.ci(ctx, repo, candidate); err != nil {
			f.CIErr = "the ledger on " + repo + ": " + err.Error()
		} else {
			f.CI, f.CIRun = status, run
		}

		// The request a seat would send: the candidate as the revision and,
		// beside it, what the same STG delivery shipped of the other
		// repositories this pipeline takes.
		beside := map[string]string{}
		for _, c := range prod.Commits {
			if c.Param != param && stgRecord.Repos[c.Repo].SHA != "" {
				beside[c.Param] = stgRecord.Repos[c.Repo].SHA
			}
		}
		if r.policy == nil {
			f.PolicyErr = "the release policy is not wired"
		} else {
			d := r.policy(ctx, map[string]any{"name": prod.Pipeline, "revision": candidate, "who": prodShadowPrincipal, "params": beside})
			if err := ctx.Err(); err != nil {
				// Reasons gathered while the time ran out are about the
				// clock, not the release.
				f.PolicyErr = "the policy's evaluation did not finish: " + err.Error()
			} else {
				f.PolicyCovered, f.PolicyReasons = policySansWindow(d)
				f.PolicyID = d.PolicyID
			}
		}
	}

	if r.cards == nil {
		f.CardsErr = "the approvals are not wired"
	} else if items, err := r.cards(); err != nil {
		f.CardsErr = "the approvals: " + err.Error()
	} else {
		f.OpenCards = openReleaseCards(items, prod.Pipeline)
	}

	if r.window == nil {
		f.WindowErr = "no reader of the release window"
	} else if who, what, open, err := r.window(ctx); err != nil {
		f.WindowErr = "the release window: " + err.Error()
	} else if open {
		f.WindowHeld = strings.TrimSpace(who + " (" + what + ")")
	}

	if r.frozen != nil {
		f.Frozen, f.FreezeReason = r.frozen(ctx)
	}
	return f
}

// deployed reads what the pipeline's environment runs of repo from the
// release records, and the record it read it from.
func (r prodShadowReads) deployed(ctx context.Context, pipeline, repo string) (headrelease.Deployed, releases.Record) {
	if r.rule == nil || r.current == nil {
		return headrelease.Deployed{Err: "the release records are not wired"}, releases.Record{}
	}
	rule, ok := r.rule(pipeline)
	if !ok || !rule.Deploys {
		return headrelease.Deployed{Err: "no release rule records " + pipeline + " as a deployment"}, releases.Record{}
	}
	rec, err := r.current(ctx, rule.Lane, rule.Env)
	switch {
	case errors.Is(err, releases.ErrUnsettled), errors.Is(err, releases.ErrNoDeployment):
		return headrelease.Deployed{Unsettled: err.Error()}, releases.Record{}
	case err != nil:
		return headrelease.Deployed{Err: "the release records of " + rule.Lane + "/" + rule.Env + ": " + err.Error()}, releases.Record{}
	}
	return headrelease.Deployed{Commit: rec.Repos[repo].SHA, Since: rec.CompletedAt, Run: rec.Run}, rec
}

// policySansWindow is the engine's decision with its window clause set aside:
// covered when the window is the only thing it names, and its other reasons
// when not.
func policySansWindow(d releasepolicy.Decision) (covered bool, reasons []string) {
	for _, reason := range d.Reasons {
		if !strings.HasPrefix(reason, policyWindowReason) {
			reasons = append(reasons, reason)
		}
	}
	if len(reasons) > 0 {
		return false, reasons
	}
	if !d.Auto && len(d.Reasons) == 0 {
		return false, []string{"the policy does not cover it and gives no reason"}
	}
	return true, nil
}

// openReleaseCards names each approval of a run of the pipeline that is not
// finished: waiting for its decision, or approved and not yet run to its end.
func openReleaseCards(items []approvals.Approval, pipeline string) []string {
	var out []string
	for _, a := range items {
		if a.Action != prodShadowAsksAbout {
			continue
		}
		if name, _ := a.Params["name"].(string); name != pipeline {
			continue
		}
		switch a.Status {
		case approvals.StatusPending, approvals.StatusApproved, approvals.StatusRunning:
			out = append(out, fmt.Sprintf("#%d %s", a.Number, a.Status))
		}
	}
	sort.Strings(out)
	return out
}

// prodShadowOf builds the shadow, or nil: the switch is not shadow, the table
// lacks one of its two rows, or this process has no delivery service.
func (s *Server) prodShadowOf(table headrelease.Table) *headrelease.ProdShadowLoop {
	mode := prodShadowMode()
	if mode != headrelease.ProdShadow || s.delivery == nil {
		return nil
	}
	var prod, source *headrelease.Deliverable
	for i := range table.Deliverables {
		switch table.Deliverables[i].Name {
		case prodShadowRow:
			prod = &table.Deliverables[i]
		case prodShadowSource:
			source = &table.Deliverables[i]
		}
	}
	if prod == nil || source == nil {
		slog.Error("prod batch shadow: the table lacks its rows; nothing is recorded", "prod", prodShadowRow, "source", prodShadowSource)
		return nil
	}
	soak := headrelease.Seconds(os.Getenv(prodShadowSoakEnv), headrelease.DefaultProdSoak)
	gap := headrelease.Seconds(os.Getenv(prodShadowGapEnv), headrelease.DefaultProdGap)
	facts := s.delivery.PolicyFacts()
	db := func() eventlog.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}
	reads := prodShadowReads{
		now:    time.Now,
		ci:     ledgerCIStrict(db, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})),
		window: facts.WindowHolder,
		frozen: func(ctx context.Context) (bool, string) {
			if s.releasePolicy == nil {
				return false, ""
			}
			return s.releasePolicy.FreezeSet(ctx)
		},
	}
	if s.releases != nil && s.releases.Service() != nil {
		reads.rule, reads.current = s.releases.Service().RuleOf, s.releases.Service().Current
	}
	if s.releasePolicy != nil {
		reads.policy = func(ctx context.Context, params map[string]any) releasepolicy.Decision {
			return s.releasePolicy.Evaluate(ctx, prodShadowAsksAbout, params)
		}
	}
	if s.approvals != nil {
		reads.cards = s.approvals.Observations
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
			return reads.gather(kubemetrics.WithSite(ctx, "prod-shadow"), *prod, *source, soak, gap)
		},
		Batch: func(ctx context.Context, from, to string) headrelease.ProdBatch {
			return s.prodShadowBatch(ctx, repo, from, to)
		},
		Record: func(result, detail string) {
			slog.Info("prod batch shadow", "deliverable", prod.Name, "result", result, "record", detail)
			s.audit.RecordDirect(prodShadowPrincipal, actuation.RoleOperator, headrelease.ProdRecordAction, "deliverable/"+prod.Name, result, detail)
		},
	}
}

// prodShadowBatch counts the commits to has and from lacks with the one
// Gitea request head-release's own record uses, and reads their Work
// trailers from the same answer.
func (s *Server) prodShadowBatch(ctx context.Context, repo, from, to string) headrelease.ProdBatch {
	brought, total, note := s.headBrought(ctx, repo, from, to)
	b := headrelease.ProdBatch{Count: total, Known: total >= 0, Note: note}
	seen := map[string]bool{}
	for _, c := range brought {
		id := c.Commit
		if len(id) > 8 {
			id = id[:8]
		}
		b.IDs = append(b.IDs, id)
		for _, work := range strings.Split(c.Work, ",") {
			if work = strings.TrimSpace(work); work != "" && !seen[work] {
				seen[work] = true
				b.Work = append(b.Work, work)
			}
		}
	}
	sort.Strings(b.Work)
	return b
}
