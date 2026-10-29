package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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
	r, err := CurrentRule(ctx, tx, "landing:"+tenant, DecisionAt())
	if err != nil || r == nil || r.Landing == nil {
		return false, err
	}
	_, includes := r.Landing.Repo(repo)
	return includes, nil
}

// Infra never reads or folds grants, even if its stream contains a malformed
// grant record. Grant appenders independently reject this repository.
func infraLandingState(ctx context.Context, tx pgx.Tx, stream string) (landing.State, error) {
	rows, err := tx.Query(ctx, `SELECT at, body_json->'landing' FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note' AND body_json ? 'landing' ORDER BY seq`, stream)
	if err != nil {
		return landing.State{}, err
	}
	defer rows.Close()
	var entries []landing.Entry
	for rows.Next() {
		var at time.Time
		var raw []byte
		if err := rows.Scan(&at, &raw); err != nil {
			return landing.State{}, err
		}
		v, err := canon.Parse(raw)
		if err != nil {
			return landing.State{}, err
		}
		r, err := landing.Parse(v)
		if err != nil {
			return landing.State{}, err
		}
		if r.String("kind") == "awaiting_grant" {
			return landing.State{}, fmt.Errorf("infra cannot await a grant")
		}
		entries = append(entries, landing.Entry{At: at, Record: r})
	}
	if err := rows.Err(); err != nil {
		return landing.State{}, err
	}
	return landing.Fold(entries)
}
