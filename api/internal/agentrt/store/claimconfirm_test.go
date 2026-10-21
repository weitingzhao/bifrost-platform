package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-399: a claim whose context ends while it commits reads back whether
// its lease landed, and says so: the claim when the commit landed (the cut
// fell in the WAL flush, which commit_delay stretches and no cancel
// interrupts), none when it did not (it fell in a trigger deferred to
// COMMIT, which pgx's cancel request interrupts). Never a claim the database
// rolled back, nor none for a lease it holds. The test cuts the claim's
// context once it sees the claim's backend there.
func TestClaimReadsBackACutCommit(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		body, at string        // the trigger's body; where the backend is when the context is cut
		after    time.Duration // how long after it is seen there: into the 100 ms commit_delay
		landed   bool
	}{
		"rolled back": {`PERFORM pg_sleep(5);`, `wait_event = 'PgSleep'`, 0, false},
		"landed": {`PERFORM set_config('commit_delay', '100000', true); PERFORM set_config('commit_siblings', '0', true);`,
			`state = 'active' AND query ILIKE 'commit%'`, 30 * time.Millisecond, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			task := w.ready(t, store.Task{})
			admin := w.db.Connect(t, w.db.Admin)
			for _, q := range []string{
				`CREATE FUNCTION public.td399_slow_commit() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$
				 BEGIN ` + c.body + ` RETURN NULL; END $$`,
				`CREATE CONSTRAINT TRIGGER td399_slow_commit AFTER INSERT ON agentrt.attempt
				 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.td399_slow_commit()`,
			} {
				_, err := admin.Exec(ctx, q)
				ok(t, err)
			}
			var db string
			ok(t, admin.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
			// A pool, as the platform's: the cut connection is closed, and the
			// read-back takes another.
			pool := w.db.Pool(t, w.db.Runtime)
			cctx, cut := context.WithCancel(ctx)
			defer cut()
			seen := make(chan bool, 1)
			go func() {
				for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
					var n int
					if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND usename = 'platform_agentrt_dev'
					  AND `+c.at, db).Scan(&n); err != nil || n > 0 {
						time.Sleep(c.after)
						cut()
						seen <- err == nil
						return
					}
				}
				seen <- false
			}()
			cl, err := store.ClaimNext(cctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{cursor}})
			if !<-seen {
				t.Fatal("the claim's commit was not seen where the test cuts it")
			}
			var leased []string
			rows, qerr := pool.Query(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id = $1`, task)
			ok(t, qerr)
			for rows.Next() {
				var id string
				ok(t, rows.Scan(&id))
				leased = append(leased, id)
			}
			ok(t, rows.Err())
			switch {
			case cl != nil && (err != nil || len(leased) != 1 || cl.AttemptID != leased[0]):
				t.Fatalf("claimed %s (%v), the database leased %v", cl.AttemptID, err, leased)
			case cl == nil && len(leased) != 0:
				t.Fatalf("claimed nothing (%v), yet the database leased %v", err, leased)
			case cl == nil && err == nil:
				t.Fatal("claimed nothing and said nothing failed")
			case (cl != nil) != c.landed:
				t.Fatalf("claimed %v (%v); want the commit landed %v", cl, err, c.landed)
			}
		})
	}
}
