package store

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// The mission row is locked before these streams. Sort in Go: SQL's text
// collation need not be byte order. Include legacy repositories in that order
// when a mixed mission's old hold needs to append an unclaimed settlement.
func missionLandingRepos(ctx context.Context, tx pgx.Tx, mission string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT repo FROM agentrt.task WHERE mission_id=$1 AND role='implement' AND repo <> ''`, mission)
	if err != nil {
		return nil, err
	}
	repos, err := pgx.CollectRows(rows, pgx.RowTo[string])
	slices.Sort(repos)
	return repos, err
}

func lockMissionLandingStreams(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string, repos []string) ([]string, error) {
	var streams, live []string
	for _, repo := range repos {
		s, err := folds.state(ctx, tx, "landing:"+repo)
		if err != nil {
			return nil, err
		}
		if landing.RepoSupported(repo) && !s.LiveAt.IsZero() {
			live = append(live, repo)
		}
		if !s.LiveAt.IsZero() || !s.Fence().IsZero() || len(s.Operations) > 0 {
			streams = append(streams, "landing:"+repo)
		}
	}
	if len(live) == 0 {
		return nil, nil // preserve the old close path's locking before live enable
	}
	hook("close: mission locked", mission)
	for _, stream := range streams {
		if err := lockStream(ctx, tx, stream); err != nil {
			return nil, err
		}
		hook("close: landing stream locked", stream)
	}
	return live, nil
}

// A closing round belongs to the entire mission, never to a partial fold of
// one repository. Keep the old root keys so already-open platform rounds
// survive upgrade; task identities are globally unique and bind the repo.
func classifyLandingRoots(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string, round *closeRound, at time.Time) (bool, []LandingRootClass, string, error) {
	repos, err := missionLandingRepos(ctx, tx, mission)
	if err != nil {
		return false, nil, "", err
	}
	return classifyMissionLandingRepos(ctx, tx, folds, mission, round, at, repos)
}

func classifyMissionLandingRepos(ctx context.Context, tx pgx.Tx, folds landingFolds, mission string, round *closeRound, at time.Time, repos []string) (bool, []LandingRootClass, string, error) {
	var roots []LandingRootClass
	var notes []string
	for _, repo := range repos {
		if !landing.RepoSupported(repo) {
			continue
		}
		inScope, rr, note, err := classifyLandingRepo(ctx, tx, folds, mission, repo, at)
		if err != nil {
			return false, nil, "", err
		}
		if inScope {
			roots = append(roots, rr...)
			notes = append(notes, note)
		}
	}
	snapshot, _ := closeSnapshot(roots)
	for i := range roots {
		r := &roots[i]
		if round.validFor(snapshot, at) && r.evidenceAt.After(round.At) {
			r.Class, r.Reason, r.NeedsEvidence = RootDelivered, "landed", false
		} else {
			r.evidenceAt = time.Time{}
		}
	}
	return len(roots) > 0, roots, strings.Join(notes, "; "), nil
}

func liveRootRepos(roots []LandingRootClass) map[string]bool {
	repos := map[string]bool{}
	for _, r := range roots {
		repos[r.Repo] = true
	}
	return repos
}
