package agentrtapi_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// A claim whose long poll ends while the poll that leased a task is still
// committing answers that lease, or leases nothing: never a 204 for a commit
// that landed (TD-399). The commit is stretched past the poll's deadline by a
// trigger deferred to COMMIT that sleeps; pgx gives up on the connection at
// its deadline and does not cancel the statement, so the server commits after
// the client stopped waiting, as a slow COMMIT does.
func TestClaimDeadlineInCommitAnswersTheLease(t *testing.T) {
	t.Parallel()
	pool, task := world(t)
	var db string
	must(t, pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	// The test server's long poll is 1 second; the lease's commit takes 1.5.
	for _, q := range []string{
		`CREATE FUNCTION public.td399_slow_commit() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$
		 BEGIN PERFORM pg_sleep(1.5); RETURN NULL; END $$`,
		`CREATE CONSTRAINT TRIGGER td399_slow_commit AFTER INSERT ON agentrt.attempt
		 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.td399_slow_commit()`,
	} {
		_, err = admin.Exec(ctx, q)
		must(t, err)
	}
	s := server(t, pool)
	start := time.Now()
	code, out := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1,
		"tools": []any{map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}}})
	took := time.Since(start)
	// Whatever the answer, wait for the commit to land or not before reading.
	for end := time.Now().Add(10 * time.Second); ; {
		var sleeping int
		must(t, admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND wait_event = 'PgSleep'`,
			db).Scan(&sleeping))
		if sleeping == 0 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("the stretched commit never ended")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var leased []string
	rows, err := pool.Query(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id = $1 AND host_id = 'h1'`, task)
	must(t, err)
	leased, err = pgx.CollectRows(rows, pgx.RowTo[string])
	must(t, err)
	switch code {
	case http.StatusOK:
		if len(leased) != 1 || out["attempt_id"] != leased[0] || out["task_id"] != task {
			t.Fatalf("the claim answered %v after %v, the database leased %v", out, took, leased)
		}
		if took < time.Second {
			t.Fatalf("the claim answered after %v, before the poll's deadline: the commit was not stretched past it", took)
		}
	case http.StatusNoContent:
		if len(leased) != 0 {
			t.Fatalf("the claim answered 204 after %v, yet %v was leased to h1 (TD-399)", took, leased)
		}
	default:
		t.Fatalf("claim: %d %v", code, out)
	}
}
