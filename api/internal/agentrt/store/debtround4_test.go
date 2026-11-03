package store_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Round 4 of the review of the debt guards (review 631d): the reviewer's
// probes, their attacks kept (each assertion states the required
// behaviour), and the independent negatives the round asked for.
func r4step(tx pgx.Tx, ev store.Event, statements ...string) error {
	if _, err := store.Append(ctx, tx, ev); err != nil {
		return fmt.Errorf("append %s: %w", ev.Type, err)
	}
	for _, q := range statements {
		if _, err := tx.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func r4importRun(t *testing.T, dw *debtWorld, debt string, id int64, result string) {
	t.Helper()
	ok(t, dw.run(t, de(debt, "debt_acceptance_ran", "controller", map[string]any{
		"debt_acceptance_run_id": id, "runner": "imported", "result": result,
	}), fmt.Sprintf(`INSERT INTO agentrt.debt_acceptance_run
	  (debt_acceptance_run_id, debt_id, runner, source_sha256, result, ran_at, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', 'imported', repeat('0',64), '%s', clock_timestamp(), 'controller')`, id, debt, result)))
}

// A legitimate rejection followed by withdrawal in one transaction has two
// different destinations. Both updates must be checked against their own event.
func TestReviewR4TwoRealMovesInOneTransaction(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const D = "TDS-980"
	importAwaiting(t, dw, D, "pass", 0, 980000)
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		if err := r4step(tx, de(D, "debt_signoff_rejected", "owner", map[string]any{
			"from": "awaiting_signoff", "to": "doing", "debt_text_ids": []any{980020},
		}), debtTextSQL(980020, D, "reject_reason", "owner"), debtMoveSQL(D, "doing", "")); err != nil {
			return err
		}
		return r4step(tx, de(D, "debt_state_changed", "owner", map[string]any{
			"from": "doing", "to": "closed", "conclusion": "withdrawn", "debt_text_ids": []any{980021},
		}), debtTextSQL(980021, D, "note", "owner"), debtMoveSQL(D, "closed", "withdrawn"))
	}))
}

// One genuine update cannot certify a second event with a fabricated from.
func TestReviewR4OneMoveCannotCertifyTwoEvents(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	D := dw.mustFile(t, "stg", "two events, only one move")
	err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		work := de(D, "debt_work_linked", dw.crew.Actor, map[string]any{
			"debt_work_id": 980020, "from": "open", "to": "doing",
		})
		work.MissionID = "M-1"
		if err := r4step(tx, work, fmt.Sprintf(`INSERT INTO agentrt.debt_work
		  (debt_work_id, debt_id, mission_id, started_at) OVERRIDING SYSTEM VALUE
		  VALUES (980020, '%s', 'M-1', now())`, D), debtMoveSQL(D, "doing", "")); err != nil {
			return err
		}
		return r4step(tx, de(D, "debt_signoff_rejected", "owner", map[string]any{
			"from": "awaiting_signoff", "to": "doing", "debt_text_ids": []any{980021},
		}), debtTextSQL(980021, D, "reject_reason", "owner"))
	})
	wantErr(t, err, "")
}

// An initial imported state is not an UPDATE and must not certify a move.
func TestReviewR4ImportInitialStateCannotCertifyMove(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const D = "TDS-980"
	err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		if err := r4step(tx, de(D, "debt_filed", "controller", map[string]any{
			"source": "import", "project": "crew", "priority": "P3", "state": "doing", "flow": "manual",
		}), insertDebtSQL(D, "doing", "controller")); err != nil {
			return err
		}
		return r4step(tx, de(D, "debt_signoff_rejected", "owner", map[string]any{
			"from": "awaiting_signoff", "to": "doing", "debt_text_ids": []any{980021},
		}), debtTextSQL(980021, D, "reject_reason", "owner"))
	})
	wantErr(t, err, "")
}

// Independent positive control for state_since: the target state already
// exists before the event's transaction; no other predicate rejects the claim.
func TestReviewR4OldStateCannotCertifyMove(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 970000)
	wantErr(t, dw.run(t, de(c.D, "debt_signoff_rejected", "owner", map[string]any{
		"from": "awaiting_signoff", "to": "doing", "debt_text_ids": []any{980021},
	}), debtTextSQL(980021, c.D, "reject_reason", "owner")), "it did not make")
}

// Missing one end of a move is invalid. If the work-only event is accepted,
// it must at least leave the actual import eligibility intact.
func TestReviewR4HalfMoveDoesNotPoisonImport(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	dw.mustFile(t, "stg", "register crew")
	const D = "TDS-980"
	importAwaiting(t, dw, D, "pass", 0, 980000)
	ev := de(D, "debt_work_linked", dw.crew.Actor, map[string]any{
		"debt_work_id": 980020, "from": "awaiting_signoff",
	})
	ev.MissionID = "M-1"
	_ = store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		return r4step(tx, ev, `INSERT INTO agentrt.debt_work
		  (debt_work_id, debt_id, mission_id, started_at) OVERRIDING SYSTEM VALUE
		  VALUES (980020, 'TDS-980', 'M-1', now())`)
	})
	ok(t, (&closing{dw: dw, t: t, D: D}).signOff())
}

// Empty bytes are already refused; spaces, tabs and newlines also cannot
// substitute for any one of the three required imported sign-off texts.
func TestReviewR4ImportedSignoffRejectsBlankText(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"signoff_summary", "ratchet_text", "follow_up_text"} {
		for _, body := range []string{"", " ", "\t\n", "\u3000"} {
			t.Run(fmt.Sprintf("%s/%x", kind, []byte(body)), func(t *testing.T) {
				t.Parallel()
				dw := newDebtWorld(t, true)
				const D = "TDS-980"
				var kinds []string
				for _, k := range []string{"signoff_summary", "ratchet_text", "follow_up_text"} {
					if k != kind {
						kinds = append(kinds, k)
					}
				}
				importAwaitingOf(t, dw, D, "pass", 0, 980000, kinds)
				sum := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
				err := dw.run(t, de(D, "debt_text_added", "controller", map[string]any{
					"debt_text_id": 980020, "text_kind": kind, "sha256": sum,
				}), fmt.Sprintf(`INSERT INTO agentrt.debt_text
				  (debt_text_id, debt_id, text_kind, body, sha256, created_by_actor_id) OVERRIDING SYSTEM VALUE
				  VALUES (980020, '%s', '%s', '%s', '%s', 'controller')`, D, kind, body, sum))
				wantErr(t, err, "check constraint")
				wantErr(t, (&closing{dw: dw, t: t, D: D}).signOff(), "")
			})
		}
	}
}

// Sequence reservation is not event order: the lower number can be written
// last, without overriding the sequence or changing any existing run.
func TestReviewR4LatestImportedRunUsesRecordOrder(t *testing.T) {
	t.Parallel()
	for _, lowerLast := range []bool{false, true} {
		t.Run(fmt.Sprintf("lower_id_written_last=%v", lowerLast), func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			const D = "TDS-980"
			importAwaiting(t, dw, D, "", 0, 980000)
			var low, high int64
			ok(t, dw.rt.QueryRow(ctx, `SELECT nextval('agentrt.debt_acceptance_run_debt_acceptance_run_id_seq')`).Scan(&low))
			ok(t, dw.rt.QueryRow(ctx, `SELECT nextval('agentrt.debt_acceptance_run_debt_acceptance_run_id_seq')`).Scan(&high))
			passID, failID := low, high
			if lowerLast {
				passID, failID = high, low
			}
			r4importRun(t, dw, D, passID, "pass")
			r4importRun(t, dw, D, failID, "fail")
			wantErr(t, (&closing{dw: dw, t: t, D: D}).signOff(), "not the ledger's imported pass")
		})
	}
}

// A genuine signed-off import can be reopened by the Owner. A duplicate
// has the same specified right; clearing its conclusion must clear its target.
func TestReviewR4OwnerCanReopenEveryClosedConclusion(t *testing.T) {
	t.Parallel()
	for _, conclusion := range []string{"signed_off", "duplicate", "withdrawn", "superseded", "boundary", "auto_closed"} {
		t.Run(conclusion, func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			D := dw.mustFile(t, "stg", "reopen every conclusion")
			E := dw.mustFile(t, "stg", "merge target")
			switch conclusion {
			case "signed_off":
				D = "TDS-980"
				importAwaiting(t, dw, D, "pass", 0, 980000)
				ok(t, (&closing{dw: dw, t: t, D: D}).signOff())
			case "auto_closed":
				c := dw.closingDebt(t, 970000)
				c.ready()
				ok(t, c.autoClose())
				D = c.D
			case "boundary":
				ok(t, dw.run(t, store.Event{Stream: "gap:AG-1", Type: "gap_accepted", Actor: "owner", Body: map[string]any{"accepted_gap_id": "AG-1"}},
					gapSQL("AG-1", "accepted")))
				dw.refToGap(t, D, 980030, "AG-1")
				ok(t, dw.run(t, de(D, "debt_state_changed", dw.crew.Actor, map[string]any{
					"from": "open", "to": "closed", "conclusion": "boundary", "debt_text_ids": []any{980020},
				}), debtTextSQL(980020, D, "note", dw.crew.Actor), debtMoveSQL(D, "closed", "boundary")))
			case "duplicate":
				ok(t, dw.run(t, de(D, "debt_merged", dw.crew.Actor, map[string]any{
					"from": "open", "to": "closed", "into": E, "debt_text_id": 980020,
				}), debtTextSQL(980020, D, "note", dw.crew.Actor),
					strings.Replace(debtMoveSQL(D, "closed", "duplicate"), "version =", fmt.Sprintf("duplicate_of = '%s', version =", E), 1)))
			default:
				ok(t, dw.run(t, de(D, "debt_state_changed", dw.crew.Actor, map[string]any{
					"from": "open", "to": "closed", "conclusion": conclusion, "debt_text_ids": []any{980020},
				}), debtTextSQL(980020, D, "note", dw.crew.Actor), debtMoveSQL(D, "closed", conclusion)))
			}
			ok(t, dw.run(t, de(D, "debt_reopened", "owner", map[string]any{
				"from": "closed", "to": "doing", "debt_text_ids": []any{980021},
			}), debtTextSQL(980021, D, "note", "owner"),
				strings.Replace(debtMoveSQL(D, "doing", ""), "version =", "duplicate_of = NULL, version =", 1)))
		})
	}
}

// Another table cannot transfer a landed part's evidence to a different debt
// or repository. Test both the part and its append-only landing, as well as IDs.
func TestReviewR4LandedPartEvidenceCannotMove(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	a, b := dw.closingDebt(t, 970000), dw.closingDebt(t, 990000)
	a.land("0123456789abcdef0123456789abcdef01234567", "2 days")
	var landing int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT debt_part_landing_id FROM agentrt.debt_part_landing WHERE debt_part_id=$1`, a.part).Scan(&landing))
	wantErr(t, dw.run(t, de(b.D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_ids": []any{a.part}}),
		fmt.Sprintf(`UPDATE agentrt.debt_part SET debt_id='%s',version=version+1 WHERE debt_part_id=%d`, b.D, a.part)), "stays its debt")
	wantErr(t, dw.run(t, de(a.D, "debt_part_landed", "controller", map[string]any{
		"debt_part_id": a.part, "debt_part_landing_id": landing, "commit": "0123456789abcdef0123456789abcdef01234567", "via": "runtime_landing",
	}), fmt.Sprintf(`UPDATE agentrt.debt_part_landing SET debt_part_id=%d WHERE debt_part_landing_id=%d`, b.part, landing)),
		"permission denied") // the runtime may not update it at all; the owner meets the append-only trigger
	wantErr(t, dw.run(t, de(a.D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_ids": []any{880001}}),
		fmt.Sprintf(`UPDATE agentrt.debt_part SET debt_part_id=880001,version=version+1 WHERE debt_part_id=%d`, a.part)),
		"can only be updated to DEFAULT") // an identity never changes, before any guard
}

// A later event that merely says "filed/import/awaiting" cannot change the
// origin of a debt that was really filed open and reached awaiting normally.
func TestReviewR4FilingEventCannotInventImportedOrigin(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 970000)
	c.ready()
	c.signsByRule()
	ok(t, c.toOwner())
	// Imported runs are valid record shapes on any debt. They must not make
	// this normally filed debt eligible for the historical import exception.
	r4importRun(t, dw, c.D, c.id(), "pass")
	ok(t, dw.run(t, de(c.D, "debt_acceptance_set", dw.crew.Actor, map[string]any{
		"acceptance_sha256": strings.Repeat("0", 64),
	}), fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_json='[{"kind":"manual","say":"changed","expect":"fine"}]',
	  acceptance_by='%s',acceptance_reviewed_sha256=NULL,acceptance_reviewed_by=NULL,version=version+1
	  WHERE debt_id='%s'`, dw.crew.Actor, c.D)))
	wantErr(t, c.signOff(), "its acceptance spec was not reviewed")
	// No second debt INSERT, no trigger disabled, same existing event stream.
	_ = store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		return r4step(tx, de(c.D, "debt_filed", dw.crew.Actor, map[string]any{
			"source": "import", "project": "crew", "priority": "P3", "state": "awaiting_signoff", "flow": "manual",
		}))
	})
	wantErr(t, c.signOff(), "its acceptance spec was not reviewed")
}

// R4-S1, past the store: the database itself refuses a debt event naming
// half a move, whatever the store would have said.
func TestDebtHalfMovePastTheStore(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	D := dw.mustFile(t, "stg", "a half move")
	half := func(body string) error {
		_, err := dw.rt.Exec(ctx, fmt.Sprintf(`INSERT INTO agentrt.event (event_stream_id, seq, event_type, actor_id, body_json, prev_sha256, sha256, at)
		  SELECT event_stream_id, last_seq + 1, 'debt_text_added', 'thread:crew-1', '%s'::jsonb, last_sha256, repeat('a', 64), now()
		  FROM agentrt.event_stream WHERE event_stream_id = 'debt:%s'`, body, D))
		return err
	}
	wantErr(t, half(fmt.Sprintf(`{"debt_id": "%s", "from": "awaiting_signoff"}`, D)), "names a move whole")
	wantErr(t, half(fmt.Sprintf(`{"debt_id": "%s", "to": "doing"}`, D)), "names a move whole")
	wantErr(t, half(fmt.Sprintf(`{"debt_id": "%s", "from": "open", "to": "nowhere"}`, D)), "names a move whole")
}

// R4-M1: two events claiming the same move that only one of them made: the
// other has no transition of its own and is refused at commit.
func TestDebtTwoEventsCannotClaimOneMove(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	D := dw.mustFile(t, "stg", "one move, two claims")
	err := store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		work := de(D, "debt_work_linked", dw.crew.Actor, map[string]any{"debt_work_id": 980020, "from": "open", "to": "doing"})
		work.MissionID = "M-1"
		if err := r4step(tx, work, fmt.Sprintf(`INSERT INTO agentrt.debt_work (debt_work_id, debt_id, mission_id, started_at)
		  OVERRIDING SYSTEM VALUE VALUES (980020, '%s', 'M-1', now())`, D), debtMoveSQL(D, "doing", "")); err != nil {
			return err
		}
		return r4step(tx, de(D, "debt_state_changed", dw.crew.Actor, map[string]any{"from": "open", "to": "doing"}))
	})
	wantErr(t, err, "it did not make")
}

// R4-M3: the latest run is the one written last, whatever its id: a pass
// written after a fail of a larger id closes, and a fail written after a
// pass of a larger id sends a watched debt back to doing; the writer
// cannot choose the order.
func TestDebtLatestRunIsTheLastWritten(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	c := dw.closingDebt(t, 970000)
	c.ready()
	// Ids above every id the fixture took, so the largest id is one of these.
	low, high := int64(979998), int64(979999)
	ran := func(debt string, id int64, result string) {
		ok(t, dw.run(t, de(debt, "debt_acceptance_ran", "controller", map[string]any{"debt_acceptance_run_id": id, "runner": "ci_evidence",
			"result": result}), debtRunSQL(id, debt, "ci_evidence", result, "controller", "", "", 0)))
	}
	ran(c.D, high, "fail")
	ran(c.D, low, "pass")
	ok(t, c.autoClose())
	d := dw.closingDebt(t, 990000)
	d.ready()
	low, high = 999998, 999999
	ran(d.D, high, "pass")
	ran(d.D, low, "fail")
	ok(t, dw.run(t, de(d.D, "debt_state_changed", dw.crew.Actor, map[string]any{"from": "watching", "to": "doing"}),
		fmt.Sprintf(`UPDATE agentrt.debt SET state = 'doing', watch_until = NULL, watch_text_id = NULL, state_since = clock_timestamp(),
		  version = version + 1 WHERE debt_id = '%s'`, d.D)))
	// The order is the database's: a writer naming one is overruled.
	var seq int64
	id := d.id()
	ok(t, dw.run(t, de(d.D, "debt_acceptance_ran", "controller", map[string]any{"debt_acceptance_run_id": id, "runner": "ci_evidence",
		"result": "fail"}), strings.Replace(strings.Replace(debtRunSQL(id, d.D, "ci_evidence", "fail", "controller", "", "", 0),
		"checks_run_id, created_by_actor_id)", "checks_run_id, recorded_seq, created_by_actor_id)", 1), ", NULL, 'controller'", ", NULL, 1000000000, 'controller'", 1)))
	ok(t, dw.rt.QueryRow(ctx, `SELECT recorded_seq FROM agentrt.debt_acceptance_run WHERE debt_acceptance_run_id = $1`, id).Scan(&seq))
	if seq == 1000000000 {
		t.Fatal("the writer chose the run's place in the order")
	}
}

// R4-M2: a debt's birth is the filing event that created it, whatever the
// writer put in the row.
func TestDebtBirthIsTheFilingEvent(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	importAwaiting(t, dw, "TDS-980", "pass", 0, 980000)
	ok(t, dw.run(t, de("TDS-981", "debt_filed", "controller", map[string]any{"source": "file", "project": "crew", "priority": "P3",
		"state": "open", "flow": "manual"}),
		`INSERT INTO agentrt.debt (debt_id, project, title, priority, state, flow, filed_at, state_since, created_by_actor_id, birth_event_id)
		  SELECT 'TDS-981', 'crew', 'claims another birth', 'P3', 'open', 'manual', now(), now(), 'controller', birth_event_id
		  FROM agentrt.debt WHERE debt_id = 'TDS-980'`))
	var birth, filing int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT birth_event_id FROM agentrt.debt WHERE debt_id = 'TDS-981'`).Scan(&birth))
	ok(t, dw.rt.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_stream_id = 'debt:TDS-981' AND event_type = 'debt_filed'`).Scan(&filing))
	if birth != filing {
		t.Fatalf("TDS-981 was born of event %d, not its filing %d", birth, filing)
	}
}
