package store_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// commitHold is the advisory lock a claim's commit waits for until the test
// lets it go (TD-410).
const commitHold = 410399

// TD-399: a claim whose context ends while it commits reads back whether
// its lease landed, and says so: the claim when the commit landed, none when
// it did not; never a claim the database rolled back, nor none for a lease
// it holds; on its only connection, which the cut closes, that what landed
// is not known (review td395-2 C4).
//
// TD-410: by state, not time (commit_delay's window moved when the machine
// was busy). A trigger deferred to COMMIT waits for a lock the test holds,
// and the test cuts the claim's context once the claim's COMMIT waits there:
// pgx gives up on the commit, closes the connection and sends the server a
// cancel. Rolled back: the cancel interrupts the trigger, as it would any
// work deferred to COMMIT. Landed: the trigger takes the cancel and waits
// again, saying so (application_name), which is what the claim sees of a
// commit past its record (the WAL flush, which no cancel interrupts): pgx
// gave up on it, the server makes it. It takes both of the cancel's
// signals: the server delivers a cancel as SIGINT to the backend and again
// to its process group (signal_child), and a backend that took the first
// can see the second in its handler, which plpgsql runs unprotected (it
// checks for interrupts before each statement); so the handler runs inside
// a block that takes the second. The test lets it go once it waits again and the claim has given up on its
// commit (on a pool, the read-back waits for the claim's transaction; on
// one connection, the claim has answered). What the database holds is read
// once the claim's transaction has ended. The one time bound a case meets
// is the claim's own read-back limit (ConfirmLimit, 3 s), from the cut to
// the end of the claim's transaction: the cancel's delivery and two of the
// test's looks.
func TestClaimReadsBackACutCommit(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		landed  bool
		oneConn bool // the claim's only connection, which the cut closes: nothing to read back with
	}{
		"rolled back":              {false, false},
		"landed":                   {true, false},
		"landed on one connection": {true, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			task := w.ready(t, store.Task{})
			admin := w.db.Connect(t, w.db.Admin)
			hold := strconv.Itoa(commitHold)
			wait := `PERFORM pg_advisory_xact_lock_shared(` + hold + `);`
			if c.landed {
				again := `PERFORM set_config('application_name', 'td410 cut', false); ` + wait
				wait = `BEGIN BEGIN ` + wait + ` EXCEPTION WHEN query_canceled THEN ` + again + ` END;
				   EXCEPTION WHEN query_canceled THEN ` + again + ` END;`
			}
			for _, q := range []string{
				`CREATE FUNCTION public.td399_held_commit() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$
				 BEGIN ` + wait + ` RETURN NULL; END $$`,
				`CREATE CONSTRAINT TRIGGER td399_held_commit AFTER INSERT ON agentrt.attempt
				 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.td399_held_commit()`,
				`SELECT pg_advisory_lock(` + hold + `)`,
			} {
				_, err := admin.Exec(ctx, q)
				ok(t, err)
			}
			held := true
			release := func() error {
				if !held {
					return nil
				}
				held = false
				_, err := admin.Exec(ctx, `SELECT pg_advisory_unlock(`+hold+`)`)
				return err
			}
			defer func() { _ = release() }()
			// A pool, as the platform's: the cut connection is closed, and the
			// read-back takes another; or one connection, which the cut leaves
			// nothing to read back with.
			pool := w.db.Pool(t, w.db.Runtime)
			var db store.DB = pool
			if c.oneConn {
				db = w.db.Connect(t, w.db.Runtime)
			}
			cctx, cut := context.WithCancel(ctx)
			defer cut()
			type claimed struct {
				cl  *store.Claim
				err error
			}
			done := make(chan claimed, 1)
			go func() {
				cl, err := store.ClaimNext(cctx, db, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{cursor}})
				done <- claimed{cl, err}
			}()
			// A backend of the runtime whose COMMIT waits for the test's lock.
			waiting := `SELECT a.pid FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
			  WHERE l.locktype = 'advisory' AND NOT l.granted AND l.classid = 0 AND l.objid = ` + hold + ` AND l.objsubid = 1
			    AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
			    AND a.usename = 'platform_agentrt_dev' AND a.query ILIKE 'commit%'`
			claim := seenPid(t, admin, "the claim's COMMIT waiting for the test's lock", waiting)
			cut()
			answer := func() claimed {
				t.Helper()
				select {
				case r := <-done:
					return r
				case <-time.After(30 * time.Second):
					t.Fatal("the claim did not answer")
				}
				return claimed{}
			}
			var r claimed
			switch {
			case !c.landed:
				r = answer() // what it says is checked below
			case c.oneConn:
				r = answer() // nothing to read back with
				seenPid(t, admin, "the claim's COMMIT waiting again after the cut", waiting+` AND a.pid = $1 AND a.application_name = 'td410 cut'`, claim)
			default:
				seenPid(t, admin, "the claim's COMMIT waiting again after the cut", waiting+` AND a.pid = $1 AND a.application_name = 'td410 cut'`, claim)
				seenPid(t, admin, "the read-back waiting for the claim's transaction to end", `SELECT pid FROM pg_stat_activity
				  WHERE datname = current_database() AND usename = 'platform_agentrt_dev' AND pg_blocking_pids(pid) @> ARRAY[$1::int]`, claim)
			}
			ok(t, release())
			if c.landed && !c.oneConn {
				r = answer()
			}
			// What the database holds, once the claim's transaction has ended:
			// its lock on the task's row, held to its end, is taken.
			var leased []string
			ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR SHARE`, task); err != nil {
					return err
				}
				rows, err := tx.Query(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id = $1`, task)
				if err != nil {
					return err
				}
				leased, err = pgx.CollectRows(rows, pgx.RowTo[string])
				return err
			}))
			notKnown := r.err != nil && strings.Contains(r.err.Error(), "is not known")
			switch {
			case c.oneConn:
				// The lease landed, and the claim says what landed is not known:
				// no claim, an error, never a 204 for it.
				if r.cl != nil || !notKnown || len(leased) != 1 {
					t.Fatalf("claimed %v (%v), the database leased %v; want an error saying the lease is not known, and the lease", r.cl, r.err, leased)
				}
			case c.landed:
				if r.cl == nil || r.err != nil || len(leased) != 1 || r.cl.AttemptID != leased[0] {
					t.Fatalf("claimed %v (%v), the database leased %v; want the claim of the lease", r.cl, r.err, leased)
				}
			default:
				if r.cl != nil || r.err == nil || notKnown || len(leased) != 0 {
					t.Fatalf("claimed %v (%v), the database leased %v; want no claim, the commit's error and no lease", r.cl, r.err, leased)
				}
			}
		})
	}
}

// seenPid polls the admin connection until the query returns a backend's
// pid, and returns it. Its deadline only bounds a failure.
func seenPid(t *testing.T, admin *pgx.Conn, what, query string, args ...any) int {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
		var pid int
		err := admin.QueryRow(ctx, query+` LIMIT 1`, args...).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s: %v", what, err)
		}
	}
	t.Fatalf("%s: not seen in 30 s; the database's backends: %s", what, backends(admin))
	return 0
}

// backends is what a failure says of the test database's backends: each
// one's state, what it waits for, its application name, its query and the
// locks it waits for.
func backends(admin *pgx.Conn) string {
	rows, err := admin.Query(ctx, `SELECT a.pid, a.usename, COALESCE(a.state, ''), COALESCE(a.wait_event_type, ''), COALESCE(a.wait_event, ''),
	  a.application_name, left(COALESCE(a.query, ''), 40), COALESCE((SELECT string_agg(l.locktype || ':' || COALESCE(l.objid::text, ''), ',')
	    FROM pg_locks l WHERE l.pid = a.pid AND NOT l.granted), '')
	  FROM pg_stat_activity a WHERE a.datname = current_database() AND a.pid <> pg_backend_pid() ORDER BY a.pid`)
	if err != nil {
		return err.Error()
	}
	var out []string
	for rows.Next() {
		var pid int
		var user, state, waitType, wait, app, query, waiting string
		if err := rows.Scan(&pid, &user, &state, &waitType, &wait, &app, &query, &waiting); err != nil {
			return err.Error()
		}
		out = append(out, fmt.Sprintf("%d %s %s %s/%s app=%q %q waits=%s", pid, user, state, waitType, wait, app, query, waiting))
	}
	if err := rows.Err(); err != nil {
		return err.Error()
	}
	return strings.Join(out, "; ")
}
