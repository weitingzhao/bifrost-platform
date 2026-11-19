package headrelease

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// The shadow of "the platform releases PROD in batches" (the judgement and
// why are in prod_shadow.go; this file is its eyes). It has no hands: ProdReads
// below is every way it touches the world, each one a read, and the one thing
// it writes is an audit record (the server wires it).
//
// PROD_HEAD_RELEASE is off (the default, and what any other value means) or
// shadow. There is no "on".
const (
	prodShadowEnv         = "PROD_HEAD_RELEASE"
	ProdSoakEnv           = "PROD_BATCH_SOAK_SECONDS"
	ProdGapEnv            = "PROD_BATCH_GAP_SECONDS"
	ProdShadowPrincipal   = "prod-batch-shadow"
	ProdShadowPassTimeout = 30 * time.Second
	// The row of deliverables.yaml the shadow judges, and the row whose
	// environment its candidate comes from.
	ProdRowName    = "platform-prod"
	ProdSourceName = "platform-stg"
	// The engine's reason for its window clause starts with this
	// (releasepolicy/engine.go, window_held_by_requester). The shadow holds
	// no window, so that one reason is set aside and the window's state is
	// its own condition. TestProdShadowPolicySetsOnlyTheWindowAside holds the
	// two files together.
	policyWindowReason = "release window: "
)

// ProdShadowMode reads the switch. Unset, it is shadow in the process where
// head-release is on (the one that would release PROD) and off anywhere else;
// "off" turns it off there too. The coordinator asked on 2026-11-16 for a
// night of shadow records before anyone decides to turn releasing on, and the
// PROD manifest is not an agent's to change: the default ships with the code,
// through CI and the release policy, and it can do nothing but read and write
// an audit record.
func ProdShadowMode() string {
	v := strings.TrimSpace(os.Getenv(prodShadowEnv))
	if v == "" && On() {
		return ProdShadow
	}
	return ProdMode(v)
}

func ProdShadowOn() bool { return ProdShadowMode() == ProdShadow }

// ProdReads is what the shadow reads.
type ProdReads struct {
	Now func() time.Time
	// rule is the lane and environment the release records keep a pipeline's
	// runs under; current what that environment runs by the records.
	Rule    func(pipeline string) (releases.Rule, bool)
	Current func(ctx context.Context, lane, env string) (releases.Record, error)
	// ci is the strict ledger verdict (the one the policy's ci_succeeded and
	// the PROD pipeline's source check read).
	CI func(ctx context.Context, repo, sha string) (status, run string, err error)
	// policy is the engine's evaluation of a release request; nil when no
	// engine is wired.
	Policy func(ctx context.Context, params map[string]any) releasepolicy.Decision
	// Cards names the approvals of a run of the pipeline that are not
	// finished (waiting for their decision, or approved and not yet run to
	// their end), each as "#number status".
	Cards  func(pipeline string) ([]string, error)
	Window func(ctx context.Context) (who, what string, open bool, err error)
	Frozen func(ctx context.Context) (bool, string)
}

// Gather reads the facts of this moment for one PROD row and its source row.
func (r ProdReads) Gather(ctx context.Context, prod, source Deliverable, soak, gap time.Duration) ProdFacts {
	f := ProdFacts{Now: r.Now(), Soak: soak, Gap: gap}
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
		if r.CI == nil {
			f.CIErr = "the evidence ledger is not connected"
		} else if status, run, err := r.CI(ctx, repo, candidate); err != nil {
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
		if r.Policy == nil {
			f.PolicyErr = "the release policy is not wired"
		} else {
			d := r.Policy(ctx, map[string]any{"name": prod.Pipeline, "revision": candidate, "who": ProdShadowPrincipal, "params": beside})
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

	if r.Cards == nil {
		f.CardsErr = "the approvals are not wired"
	} else if items, err := r.Cards(prod.Pipeline); err != nil {
		f.CardsErr = "the approvals: " + err.Error()
	} else {
		f.OpenCards = items
	}

	if r.Window == nil {
		f.WindowErr = "no reader of the release window"
	} else if who, what, open, err := r.Window(ctx); err != nil {
		f.WindowErr = "the release window: " + err.Error()
	} else if open {
		f.WindowHeld = strings.TrimSpace(who + " (" + what + ")")
	}

	if r.Frozen != nil {
		f.Frozen, f.FreezeReason = r.Frozen(ctx)
	}
	return f
}

// deployed reads what the pipeline's environment runs of repo from the
// release records, and the record it read it from.
func (r ProdReads) deployed(ctx context.Context, pipeline, repo string) (Deployed, releases.Record) {
	if r.Rule == nil || r.Current == nil {
		return Deployed{Err: "the release records are not wired"}, releases.Record{}
	}
	rule, ok := r.Rule(pipeline)
	if !ok || !rule.Deploys {
		return Deployed{Err: "no release rule records " + pipeline + " as a deployment"}, releases.Record{}
	}
	rec, err := r.Current(ctx, rule.Lane, rule.Env)
	switch {
	case errors.Is(err, releases.ErrUnsettled), errors.Is(err, releases.ErrNoDeployment):
		return Deployed{Unsettled: err.Error()}, releases.Record{}
	case err != nil:
		return Deployed{Err: "the release records of " + rule.Lane + "/" + rule.Env + ": " + err.Error()}, releases.Record{}
	}
	return Deployed{Commit: rec.Repos[repo].SHA, Since: rec.CompletedAt, Run: rec.Run}, rec
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

// ProdBatchFrom counts the commits a candidate brings and reads their Work
// trailers from the same answer head-release's own record uses (Brought).
func ProdBatchFrom(brought []BroughtCommit, total int, note string) ProdBatch {
	b := ProdBatch{Count: total, Known: total >= 0, Note: note}
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
