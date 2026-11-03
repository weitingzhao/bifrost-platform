package store_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 631e probes (round 5), kept as regression tests, with the guards'
// own refusals named and their allowed counterparts added.

// r5forge has the runtime attach the SECURITY DEFINER transition function to
// a temporary table of its own, as trigger, and move that table, claiming a
// move of d that the debt never made.
func r5forge(tx pgx.Tx, d, trigger string) error {
	return r4step(tx, de(d, "debt_state_changed", "controller", map[string]any{
		"from": "awaiting_signoff", "to": "doing",
	}),
		`CREATE TEMP TABLE review_fake_debt(debt_id text, state text) ON COMMIT DROP`,
		`INSERT INTO review_fake_debt VALUES ('`+d+`', 'awaiting_signoff')`,
		`CREATE TRIGGER `+trigger+` AFTER UPDATE ON review_fake_debt
		 FOR EACH ROW EXECUTE FUNCTION agentrt.record_debt_transition()`,
		`UPDATE review_fake_debt SET state = 'doing'`)
}

func r5forgeRefused(t *testing.T, dw *debtWorld, d string, err error, want string) {
	t.Helper()
	if err == nil {
		var state string
		var n int
		ok(t, dw.rt.QueryRow(ctx, `SELECT state FROM agentrt.debt WHERE debt_id=$1`, d).Scan(&state))
		ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_transition WHERE debt_id=$1`, d).Scan(&n))
		t.Fatalf("fake table certified a move: real state=%s, transitions=%d", state, n)
	}
	wantErr(t, err, want)
	// A refused attempt must leave the real import eligible for sign-off.
	ok(t, (&closing{dw: dw, t: t, D: d}).signOff())
}

// The runtime may not attach the function at all: EXECUTE is revoked.
func TestReviewR5RuntimeCannotForgeTransitionThroughTempTable(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const d = "TDS-980"
	importAwaiting(t, dw, d, "pass", 0, 980000)
	err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error { return r5forge(tx, d, "review_fake") })
	r5forgeRefused(t, dw, d, err, "permission denied for function")
}

// Granted EXECUTE (as a careless later grant would), the runtime still
// cannot forge, even with a trigger shaped and named as the real one: the
// function refuses any table but agentrt.debt.
func TestDebtTransitionRefusesAForeignTable(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const d = "TDS-980"
	importAwaiting(t, dw, d, "pass", 0, 980000)
	var role string
	ok(t, dw.rt.QueryRow(ctx, `SELECT current_user`).Scan(&role))
	_, err := dw.db.Connect(t, dw.db.Admin).Exec(ctx,
		`GRANT EXECUTE ON FUNCTION agentrt.record_debt_transition() TO `+pgx.Identifier{role}.Sanitize())
	ok(t, err)
	err = store.InTx(ctx, dw.rt, func(tx pgx.Tx) error { return r5forge(tx, d, "debt_20_transition") })
	r5forgeRefused(t, dw, d, err, "records only as debt_20_transition")
}

// Attached to agentrt.debt in any other shape (by its owner, the only role
// that may attach it), the function refuses as well: another trigger's
// name, BEFORE, FOR EACH STATEMENT, on INSERT.
func TestDebtTransitionRefusesOtherTriggerShapes(t *testing.T) {
	t.Parallel()
	const drop = `DROP TRIGGER debt_20_transition ON agentrt.debt`
	const moved = ` WHEN (OLD.state IS DISTINCT FROM NEW.state) EXECUTE FUNCTION agentrt.record_debt_transition()`
	const plain = ` EXECUTE FUNCTION agentrt.record_debt_transition()`
	for name, ddl := range map[string][]string{
		"name":  {`CREATE TRIGGER debt_21_copy AFTER UPDATE OF state ON agentrt.debt FOR EACH ROW` + moved},
		"when":  {drop, `CREATE TRIGGER debt_20_transition BEFORE UPDATE OF state ON agentrt.debt FOR EACH ROW` + moved},
		"level": {drop, `CREATE TRIGGER debt_20_transition AFTER UPDATE OF state ON agentrt.debt FOR EACH STATEMENT` + plain},
		"op":    {drop, `CREATE TRIGGER debt_20_transition AFTER INSERT ON agentrt.debt FOR EACH ROW` + plain},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			const d = "TDS-980"
			importAwaiting(t, dw, d, "pass", 0, 980000)
			admin := dw.db.Connect(t, dw.db.Admin)
			for _, q := range ddl {
				_, err := admin.Exec(ctx, q)
				ok(t, err)
			}
			var err error
			if name == "op" {
				_, err = dw.file(t, dw.rt, "stg", "filed under a misplaced trigger")
			} else {
				err = (&closing{dw: dw, t: t, D: d}).signOff()
			}
			wantErr(t, err, "records only as debt_20_transition")
		})
	}
}

// The event check is right whenever it runs: run early, after the event's
// move, it passes and the transaction commits; run early, before the move,
// it refuses (review 631e 2).
func TestDebtEventCheckedEarly(t *testing.T) {
	t.Parallel()
	for name, before := range map[string]bool{"after the move": false, "before the move": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			const d = "TDS-980"
			importAwaiting(t, dw, d, "pass", 0, 980000)
			err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
				_, err := store.Append(ctx, tx, de(d, "debt_signoff_rejected", "owner", map[string]any{
					"from": "awaiting_signoff", "to": "doing", "debt_text_ids": []any{980020}}))
				if err != nil {
					return err
				}
				stmts := []string{debtTextSQL(980020, d, "reject_reason", "owner"), debtMoveSQL(d, "doing", ""), `SET CONSTRAINTS ALL IMMEDIATE`}
				if before {
					stmts = []string{`SET CONSTRAINTS ALL IMMEDIATE`}
				}
				for _, q := range stmts {
					if _, err = tx.Exec(ctx, q); err != nil {
						return err
					}
				}
				return nil
			})
			if before {
				wantErr(t, err, "it did not make")
				return
			}
			ok(t, err)
			var state string
			var n int
			ok(t, dw.rt.QueryRow(ctx, `SELECT state FROM agentrt.debt WHERE debt_id=$1`, d).Scan(&state))
			ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_transition WHERE debt_id=$1`, d).Scan(&n))
			if state != "doing" || n != 1 {
				t.Fatalf("state %s, %d transitions; want doing, 1", state, n)
			}
		})
	}
}

// Plain INSERT is denied to runtime and reader. The migration owner is
// trusted: its direct INSERT succeeds; its UPDATE/DELETE/TRUNCATE still fail.
func TestReviewR5TransitionDirectRoleBoundaries(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	d := dw.mustFile(t, "stg", "direct transition role checks")
	q := `INSERT INTO agentrt.debt_transition(debt_id,event_id,from_state,to_state)
	 SELECT debt_id,birth_event_id,'open','doing' FROM agentrt.debt WHERE debt_id=$1`
	for name, dsn := range map[string]string{"runtime": dw.db.Runtime, "reader": dw.db.Reader} {
		t.Run(name, func(t *testing.T) {
			_, err := dw.db.Connect(t, dsn).Exec(ctx, q, d)
			wantErr(t, err, "permission denied")
		})
	}
	owner := dw.db.Connect(t, dw.db.Owner)
	_, err := owner.Exec(ctx, q, d)
	ok(t, err)
	for _, q := range []string{
		`UPDATE agentrt.debt_transition SET to_state='watching'`,
		`DELETE FROM agentrt.debt_transition`, `TRUNCATE agentrt.debt_transition`,
	} {
		_, err := owner.Exec(ctx, q)
		wantErr(t, err, "append only")
	}
}

// Validate exactly-one even when the caller flushes the pending constraint
// trigger early and later reuses the same event within the same transaction.
func TestReviewR5OneEventCannotMoveTwice(t *testing.T) {
	t.Parallel()
	for _, early := range []bool{false, true} {
		t.Run(fmt.Sprintf("flush_early=%v", early), func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			const d = "TDS-980"
			importAwaiting(t, dw, d, "pass", 0, 980000)
			ok(t, (&closing{dw: dw, t: t, D: d}).signOff())
			err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
				a, err := store.Append(ctx, tx, de(d, "debt_reopened", "owner", map[string]any{
					"from": "closed", "to": "doing", "debt_text_ids": []any{980020},
				}))
				if err != nil {
					return err
				}
				for _, q := range []string{debtTextSQL(980020, d, "note", "owner"), debtMoveSQL(d, "doing", "")} {
					if _, err = tx.Exec(ctx, q); err != nil {
						return err
					}
				}
				if early {
					if _, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
						return err
					}
					if _, err = tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
						return err
					}
				}
				if err = r4step(tx, de(d, "debt_state_changed", "owner", map[string]any{
					"from": "doing", "to": "closed", "conclusion": "withdrawn", "debt_text_ids": []any{980021},
				}), debtTextSQL(980021, d, "note", "owner"), debtMoveSQL(d, "closed", "withdrawn")); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `SELECT set_config('agentrt.event_id',$1,true)`, strconv.FormatInt(a.EventID, 10)); err != nil {
					return err
				}
				_, err = tx.Exec(ctx, debtMoveSQL(d, "doing", ""))
				return err
			})
			wantErr(t, err, "debt_transition_one_per_event")
		})
	}
}

// Hash a raw event exactly as Append does, bypassing only the Go vocabulary.
func r5RawEvent(tx pgx.Tx, d, typ string, body map[string]any) error {
	stream := store.DebtStream(d)
	var seq int64
	var prev string
	if err := tx.QueryRow(ctx, `SELECT last_seq,last_sha256 FROM agentrt.event_stream WHERE event_stream_id=$1 FOR UPDATE`, stream).Scan(&seq, &prev); err != nil {
		return err
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	env := map[string]any{"v": 1, "stream": stream, "seq": seq + 1, "type": typ, "by": "controller",
		"at": at.Format("2006-01-02T15:04:05.000000Z"), "prev_sha256": prev, "mission_id": nil,
		"plan_node_id": nil, "task_id": nil, "attempt_id": nil, "cause_event_id": nil, "body": body}
	sum, err := canon.Sum(env)
	if err != nil {
		return err
	}
	raw, err := canon.Encode(body)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.event(event_stream_id,seq,event_type,actor_id,body_json,prev_sha256,sha256,at)
	 VALUES($1,$2,$3,'controller',$4::jsonb,$5,$6,$7)`, stream, seq+1, typ, string(raw), prev, sum, at)
	return err
}

// A move's ends are JSON strings of a debt's states (631e 3). Strings that
// name a move pass the shape and are then held to it; equal ends name none.
func TestReviewR5DatabaseRejectsNullMoveEndpoints(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		from, to any
		want     string
	}{
		{nil, nil, "names a move whole"}, {nil, "doing", "names a move whole"}, {"open", nil, "names a move whole"},
		{1, "doing", "names a move whole"}, {[]any{"open"}, "doing", "names a move whole"},
		{"open", "doing", "it did not make"}, {"open", "open", ""},
	} {
		t.Run(fmt.Sprintf("%v_%v", c.from, c.to), func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			d := dw.mustFile(t, "stg", "null endpoints")
			err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
				return r5RawEvent(tx, d, "debt_state_changed", map[string]any{"debt_id": d, "from": c.from, "to": c.to})
			})
			if c.want == "" {
				ok(t, err)
				return
			}
			wantErr(t, err, c.want)
		})
	}
}

func TestReviewR5RoundTripAndBirthRemainAccurate(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const d = "TDS-980"
	importAwaiting(t, dw, d, "pass", 0, 980000)
	ok(t, (&closing{dw: dw, t: t, D: d}).signOff())
	var birth int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT birth_event_id FROM agentrt.debt WHERE debt_id=$1`, d).Scan(&birth))
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		if err := r4step(tx, de(d, "debt_reopened", "owner", map[string]any{"from": "closed", "to": "doing", "debt_text_ids": []any{980020}}),
			debtTextSQL(980020, d, "note", "owner"), debtMoveSQL(d, "doing", "")); err != nil {
			return err
		}
		return r4step(tx, de(d, "debt_state_changed", "owner", map[string]any{"from": "doing", "to": "closed", "conclusion": "withdrawn", "debt_text_ids": []any{980021}}),
			debtTextSQL(980021, d, "note", "owner"), debtMoveSQL(d, "closed", "withdrawn"))
	}))
	var after int64
	var state string
	var n, events int
	ok(t, dw.rt.QueryRow(ctx, `SELECT birth_event_id,state FROM agentrt.debt WHERE debt_id=$1`, d).Scan(&after, &state))
	ok(t, dw.rt.QueryRow(ctx, `SELECT count(*),count(DISTINCT event_id) FROM agentrt.debt_transition WHERE debt_id=$1`, d).Scan(&n, &events))
	if after != birth || state != "closed" || n != 3 || events != 3 {
		t.Fatalf("birth %d -> %d, state %s, rows %d, events %d", birth, after, state, n, events)
	}
}

func TestReviewR5SameStateUpdateRecordsNoMove(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	d := dw.mustFile(t, "stg", "same state")
	ok(t, dw.run(t, de(d, "debt_edited", dw.crew.Actor, map[string]any{"fields": []string{"title"}}),
		fmt.Sprintf(`UPDATE agentrt.debt SET state=state,title='same state edited',version=version+1 WHERE debt_id='%s'`, d)))
	var n int
	ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_transition WHERE debt_id=$1`, d).Scan(&n))
	if n != 0 {
		t.Fatalf("same-state update recorded %d transitions", n)
	}
	wantErr(t, dw.run(t, de(d, "debt_state_changed", dw.crew.Actor, map[string]any{"from": "open", "to": "doing"}),
		fmt.Sprintf(`UPDATE agentrt.debt SET state=state,version=version+1 WHERE debt_id='%s'`, d)), "it did not make")
}

func TestReviewR5CreateAndMoveInOneTransaction(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const d = "TDS-980"
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		if err := r4step(tx, de(d, "debt_filed", "controller", map[string]any{"source": "file", "project": "crew", "priority": "P3", "state": "open", "flow": "manual"}),
			insertDebtSQL(d, "open", "controller")); err != nil {
			return err
		}
		work := de(d, "debt_work_linked", "controller", map[string]any{"debt_work_id": 980020, "from": "open", "to": "doing"})
		work.MissionID = "M-1"
		return r4step(tx, work, `INSERT INTO agentrt.debt_work(debt_work_id,debt_id,mission_id,started_at) OVERRIDING SYSTEM VALUE
		 VALUES(980020,'TDS-980','M-1',now())`, debtMoveSQL(d, "doing", ""))
	}))
}

func TestReviewR5InitialImportStatesAndSplitFiling(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	for i, state := range []string{"open", "doing", "watching", "awaiting_signoff"} {
		d := fmt.Sprintf("TDS-%d", 980+i)
		q := fmt.Sprintf(`INSERT INTO agentrt.debt(debt_id,project,title,priority,state,flow,watch_until,filed_at,state_since,created_by_actor_id)
		 VALUES('%s','crew','initial import','P3','%s','manual',%s,now(),now(),'controller')`, d, state,
			map[bool]string{true: "now()+interval '1 day'", false: "NULL"}[state == "watching"])
		ok(t, dw.run(t, de(d, "debt_filed", "controller", map[string]any{"source": "import", "project": "crew", "priority": "P3", "state": state, "flow": "manual"}), q))
		var n int
		ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_transition WHERE debt_id=$1`, d).Scan(&n))
		if n != 0 {
			t.Fatalf("initial %s generated a transition", state)
		}
	}
	wantErr(t, dw.run(t, de("TDS-990", "debt_filed", "controller", map[string]any{"source": "import", "project": "crew", "priority": "P3", "state": "open", "flow": "manual"})), "created no debt")
}

func TestReviewR5SequenceAndDeleteBoundaries(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	d := dw.mustFile(t, "stg", "immutable birth")
	for _, q := range []string{`SELECT setval('agentrt.debt_record_seq',1,false)`,
		`ALTER SEQUENCE agentrt.debt_record_seq RESTART WITH 1`,
		`DELETE FROM agentrt.debt`, `TRUNCATE agentrt.debt`,
		`UPDATE agentrt.debt_acceptance_run SET recorded_seq=1`} {
		_, err := dw.rt.Exec(ctx, q)
		wantErr(t, err, "")
	}
	// Back-to-back sequence reservations may make gaps, but do not reorder writes.
	var a, b int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT nextval('agentrt.debt_record_seq')`).Scan(&a))
	ok(t, dw.rt.QueryRow(ctx, `SELECT nextval('agentrt.debt_record_seq')`).Scan(&b))
	if b <= a {
		t.Fatalf("sequence went backwards: %d -> %d", a, b)
	}
	_, err := dw.db.Connect(t, dw.db.Owner).Exec(ctx, `DELETE FROM agentrt.debt WHERE debt_id=$1`, d)
	wantErr(t, err, "not by deletion")
}

// Prove overlap on the same debt, rather than merely launching two goroutines.
func TestReviewR5ConcurrentMovesKeepOnlyActualTransitions(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const d = "TDS-980"
	importAwaiting(t, dw, d, "pass", 0, 980000)
	c1, c2 := dw.db.Connect(t, dw.db.Runtime), dw.db.Connect(t, dw.db.Runtime)
	tx1, err := c1.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx1.Rollback(ctx) }()
	ok(t, r4step(tx1, de(d, "debt_signoff_rejected", "owner", map[string]any{"from": "awaiting_signoff", "to": "doing", "debt_text_ids": []any{980020}}),
		debtTextSQL(980020, d, "reject_reason", "owner"), debtMoveSQL(d, "doing", "")))
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	// The second transaction is rolled back (when it did not commit) before
	// its result is sent: the test's cleanup closes c2 once it has the result.
	go func() {
		done <- func() error {
			tx, err := c2.Begin(deadline)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err = store.Append(deadline, tx, de(d, "debt_signed_off", "owner", map[string]any{"from": "awaiting_signoff", "to": "closed"})); err != nil {
				return err
			}
			if _, err = tx.Exec(deadline, debtMoveSQL(d, "closed", "signed_off")); err != nil {
				return err
			}
			return tx.Commit(deadline)
		}()
	}()
	admin := dw.db.Connect(t, dw.db.Admin)
	for {
		var blocked bool
		ok(t, admin.QueryRow(deadline, `SELECT $1::int = ANY(pg_blocking_pids($2))`, int(c1.PgConn().PID()), int(c2.PgConn().PID())).Scan(&blocked))
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("second transaction ended before blocking: %v", err)
		case <-deadline.Done():
			t.Fatal(deadline.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	ok(t, tx1.Commit(deadline))
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "is not the one") {
			t.Fatalf("stale signoff: %v", err)
		}
	case <-deadline.Done():
		t.Fatal(deadline.Err())
	}
	var n int
	ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_transition WHERE debt_id=$1`, d).Scan(&n))
	if n != 1 {
		t.Fatalf("%d transitions, want one committed rejection", n)
	}
}
