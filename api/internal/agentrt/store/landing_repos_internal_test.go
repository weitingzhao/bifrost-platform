package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type repoCIStateTx struct {
	waitReviewTx
	t      *testing.T
	stream string
	reads  int
}

func (tx *repoCIStateTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	tx.reads++
	if len(args) != 1 || args[0] != tx.stream {
		tx.t.Fatalf("state read crossed repositories: %v", args)
	}
	return tx.waitReviewTx.Query(ctx, query, args...)
}

// The merged CI reader preserves both repositories' state semantics:
// platform may wait for a grant, infra may not; both use one stream read.
// Row fixtures exercise the production folds, not PostgreSQL execution.
func TestS6StateCIKeysKeepRepositoryGrantPolicy(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	entries := reviewWaitEntries(t, at.Add(-time.Hour), at)
	const op = "land:t_00000000000000000000000001:1"
	for _, tc := range []struct {
		name, repo, phase string
		count             int
		refused           bool
	}{
		{"platform waiting", "bifrost-platform", "awaiting_grant", len(entries), false},
		{"infra ordinary", "bifrost-trade-infra", "branch_pushed", 4, false},
		{"infra waiting refused", "bifrost-trade-infra", "", len(entries), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &repoCIStateTx{waitReviewTx: waitReviewTx{entries: entries[:tc.count]}, t: t, stream: "landing:" + tc.repo}
			state, keys, err := LandingStateCIKeys(context.Background(), tx, tx.stream)
			if tx.reads != 1 || len(keys) != 0 {
				t.Fatal("unexpected reads or pinned keys", tx.reads, keys)
			}
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "infra cannot await a grant") {
					t.Fatal("infra accepted a waiting operation", err)
				}
			} else if err != nil || state.Operations[op].Phase != tc.phase {
				t.Fatal("repository state changed", state, err)
			}
		})
	}
}
