package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ReadBoundaryPrecheck uses the claim gate's own facts and predicate. It neither
// claims work nor reserves a slot; final push/grant/CI checks remain mandatory.
func ReadBoundaryPrecheck(ctx context.Context, db DB, sources GrantSources, task string) (map[string]any, error) {
	if !deliveryTaskID.MatchString(task) {
		return nil, fmt.Errorf("%w: invalid task id", ErrInvalid)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var repo string
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT COALESCE(repo,''), transaction_timestamp() FROM agentrt.task WHERE task_id=$1`, task).Scan(&repo, &now); err != nil {
		return nil, err
	}
	out := map[string]any{"task_id": task, "repo": repo, "as_of": now, "state": "unknown", "boundary_id": "landing.claim.evidence", "source": "landing.EligibleToClaim", "note": "Read-only PROD claim check. Not push or release authorization; grants, CI, release windows and current facts are rechecked by their execution gates."}
	if !landing.RepoSupported(repo) {
		out["reason"] = "Repository has no registered landing gate"
		return out, nil
	}
	src := sources.forRepo(repo).Facts
	if src.Result == nil || src.History == nil {
		out["reason"] = "Trusted Git readers are unavailable on this seat"
		return out, nil
	}
	s, err := LandingState(ctx, tx, "landing:"+repo)
	if err != nil {
		return nil, err
	}
	f, deps, err := landingFactsDB(ctx, tx, src, "prod", "live", task, s.LatestOperation(task), now)
	if err != nil {
		return nil, err
	}
	if f.Rule != nil {
		out["rule_id"], out["rule_version"] = f.Rule.ID, f.Rule.Version
	}
	if err = landingClaimExternal(context.WithValue(ctx, landingEvidenceTxKey{}, tx), src, &f, deps); err != nil {
		out["reason"] = "Trusted candidate or dependency evidence could not be read; no authorization inferred"
		return out, nil
	}
	d := landing.EligibleToClaim(f, now)
	out["explanation"], out["reason_registered"] = landing.BoundaryReason(d.Reason)
	out["state"], out["reason"], out["boundary_id"] = "blocked", d.Reason, "landing.claim."+d.Reason
	if d.Allowed {
		out["state"], out["reason"], out["boundary_id"] = "claim_eligible", "Claim checks passed at this snapshot; execution rechecks all facts", "landing.claim"
		out["explanation"], out["reason_registered"] = "", true
	}
	out["candidate"], out["base"], out["detail"] = f.A, f.Base, d.Detail
	out["other_in_flight"], out["reserved_today"] = f.OtherInFlight, f.ReservedToday
	return out, nil
}
