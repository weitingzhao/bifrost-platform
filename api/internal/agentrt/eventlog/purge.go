package eventlog

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PurgeRepealedLaw deletes the whole stream of a repealed law (law:<n>), the
// one deletion the ledger allows (migration 0047, Owner 2026-11-24: a
// repealed law is deleted seven days after its repeal, or at once when he
// asks). The database decides: the stream's last record must say the law is
// repealed, and unless forced it must be seven days old. It returns how many
// events went (0 when the stream does not qualify) and the approval ids they
// carried. The ledger's writes stay this package's; the caller records what
// must outlive the stream (planstore's tombstone) in the same transaction.
func PurgeRepealedLaw(ctx context.Context, tx pgx.Tx, stream string, forced bool) (int, []string, error) {
	var n int
	var ids []string
	err := tx.QueryRow(ctx, `SELECT deleted, approval_ids FROM agentrt.purge_repealed_law($1, $2)`, stream, forced).Scan(&n, &ids)
	return n, ids, err
}
