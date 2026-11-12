package store_test

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// Fixtures of the plan import, for the tests that start from an imported
// plan; the import's results through the routes are in
// agentrtapi/planimport_test.go.

func importDB(t *testing.T) (*pgxpool.Pool, planimport.Request) {
	t.Helper()
	db := seededDB(t)
	text, err := os.ReadFile("../planimport/testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return db.Pool(t, db.Runtime), req
}

// rows counts what an import writes: nodes, events, artifacts and their
// bodies, streams, outbox rows.
func rows(t *testing.T, pool *pgxpool.Pool) [6]int {
	t.Helper()
	var c [6]int
	ok(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.plan_node), (SELECT count(*) FROM agentrt.event),
	  (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob),
	  (SELECT count(*) FROM agentrt.event_stream), (SELECT count(*) FROM agentrt.outbox)`).Scan(&c[0], &c[1], &c[2], &c[3], &c[4], &c[5]))
	return c
}
