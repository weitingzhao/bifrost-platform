package cirunview

import (
	"context"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	busdrift "github.com/weitingzhao/bifrost-platform/api/internal/bus/drift"
)

// LedgerKnown says whether the evidence ledger already has the run uid of a
// commit (ScopeShadow.Known).
func LedgerKnown(db func() eventlog.DB) func(ctx context.Context, repo, commit, uid string) (bool, error) {
	return func(ctx context.Context, repo, commit, uid string) (bool, error) {
		d := db()
		if d == nil {
			return false, fmt.Errorf("the evidence ledger is not connected")
		}
		rows, err := evidence.EvidenceCIByCommit(ctx, d, repo, commit, 50)
		for _, r := range rows {
			if r.RunUID == uid {
				return true, err
			}
		}
		return false, err
	}
}

// LedgerIncremental is the run of a passed incremental row the ledger has for
// a commit, by commit and then by its tree (ScopeShadow.Incremental).
func LedgerIncremental(db func() eventlog.DB, tree func(ctx context.Context, repo, commit string) (string, error)) func(ctx context.Context, repo, commit string) (string, error) {
	return func(ctx context.Context, repo, commit string) (string, error) {
		d := db()
		if d == nil {
			return "", fmt.Errorf("the evidence ledger is not connected")
		}
		rows, err := evidence.EvidenceCIByCommit(ctx, d, repo, commit, 50)
		if err != nil {
			return "", err
		}
		if t, terr := tree(ctx, repo, commit); terr == nil {
			more, err := evidence.EvidenceByTree(ctx, d, repo, t, 50)
			if err != nil {
				return "", err
			}
			rows = append(rows, more...)
		}
		for _, r := range rows {
			if evidence.IncrementalBase(r) != "" {
				return r.RunName, nil
			}
		}
		return "", nil
	}
}

// AnnounceDrift tells the bus of a miss (ScopeShadow.Drift).
func AnnounceDrift(db func() eventlog.DB) func(ctx context.Context, repo, commit, passed, contradicts string) error {
	return func(ctx context.Context, repo, commit, passed, contradicts string) error {
		d := db()
		if d == nil {
			return fmt.Errorf("the ledger is not connected")
		}
		_, err := busdrift.Announce(ctx, d, busdrift.Found{Repo: repo, Commit: commit, Kind: busdrift.FullVsIncremental, Passed: passed, Contradicts: contradicts})
		return err
	}
}
