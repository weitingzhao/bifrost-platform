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
// that landed, nor a lease the database rolled back (TD-399). pgx gives up on
// the connection at its deadline and sends a cancel request, so where the
// deadline falls decides the commit: in a trigger deferred to COMMIT that
// sleeps, the cancel rolls it back; in the WAL flush, which commit_delay
// stretches and no cancel interrupts, it lands after the client stopped
// waiting.
func TestClaimDeadlineInCommitAnswersTheLease(t *testing.T) {
	t.Parallel()
	// The test server's long poll is 1 second from its start; now() in the
	// trigger is the start of the poll's transaction, just after it.
	for name, body := range map[string]string{
		"rolled back": `PERFORM pg_sleep(1.5);`,
		"landed": `PERFORM set_config('commit_delay', '100000', true); PERFORM set_config('commit_siblings', '0', true);
		  PERFORM pg_sleep_until(now() + interval '960 milliseconds');`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			claimAcrossTheDeadline(t, body)
		})
	}
}

// claimAcrossTheDeadline claims the world's task with the lease's commit
// running trigger body, and checks the answer against the database.
func claimAcrossTheDeadline(t *testing.T, body string) {
	pool, task := world(t)
	var db string
	must(t, pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	for _, q := range []string{
		`CREATE FUNCTION public.td399_slow_commit() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$
		 BEGIN ` + body + ` RETURN NULL; END $$`,
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
		var committing int
		must(t, admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()
		  AND state = 'active'`, db).Scan(&committing))
		if committing == 0 {
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
		if took < 900*time.Millisecond {
			t.Fatalf("the claim answered after %v, well before the poll's deadline: the commit was not stretched to it", took)
		}
	case http.StatusNoContent:
		if len(leased) != 0 {
			t.Fatalf("the claim answered 204 after %v, yet %v was leased to h1 (TD-399)", took, leased)
		}
	default:
		t.Fatalf("claim: %d %v", code, out)
	}
}
