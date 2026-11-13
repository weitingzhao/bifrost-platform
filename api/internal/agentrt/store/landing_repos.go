package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ExistingLandingRepos is a startup-only discovery. Platform keeps its legacy
// recovery loop; another repository participates only after it has records.
func ExistingLandingRepos(ctx context.Context, db DB) ([]string, error) {
	seen := map[string]bool{"bifrost-platform": true}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		for _, repo := range landing.LandingRepoNames() {
			if repo == "bifrost-platform" {
				continue
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id=$1)`, "landing:"+repo).Scan(&exists); err != nil {
				return err
			}
			seen[repo] = exists
		}
		return nil
	})
	var repos []string
	for _, repo := range landing.LandingRepoNames() {
		if seen[repo] {
			repos = append(repos, repo)
		}
	}
	return repos, err
}

// landingEvidenceTxKey scopes a transaction to LandingFacts' synchronous
// evidence reads only. Claims read evidence outside their transaction and
// keep the independent, bounded rule read below. No transaction is cached
// in the shared source or controller.
type landingEvidenceTxKey struct{}

func LandingRuleIncludes(ctx context.Context, db DB, tenant, repo string) (bool, error) {
	if !landing.RepoSupported(repo) {
		return false, nil
	}
	if tx, ok := ctx.Value(landingEvidenceTxKey{}).(pgx.Tx); ok {
		return landingRuleIncludes(ctx, tx, tenant, repo)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var includes bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		includes, err = landingRuleIncludes(ctx, tx, tenant, repo)
		return err
	})
	return includes, err
}

func landingRuleIncludes(ctx context.Context, tx pgx.Tx, tenant, repo string) (bool, error) {
	r, err := CurrentRule(ctx, tx, "landing:"+tenant, DecisionAt(ctx))
	if err != nil || r == nil || r.Landing == nil {
		return false, err
	}
	_, includes := r.Landing.Repo(repo)
	return includes, nil
}
