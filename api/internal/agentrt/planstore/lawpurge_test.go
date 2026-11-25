package planstore_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-24, Owner in the legislation thread: a repealed law must
// not linger as poison, "满七天或者我直接要求删..都可以物理删除". These check
// the one deletion the ledger allows (migration 0047): only a repealed law,
// only seven days after its repeal or by the Owner's purge card, the whole
// stream at once, and a tombstone that keeps its number and card ids so
// neither comes back.

// purgeWorld transcribes the two laws of lawTable and adds law 3 and repeals
// it by cards, the repeal written at repealedAt.
func purgeWorld(t *testing.T, repealedAt time.Time) (*world, planstore.LawCard, planstore.LawCard) {
	t.Helper()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	at := eventlog.WithEventClock(ctx, func() time.Time { return repealedAt })
	add := planstore.LawCard{ApprovalID: "appr_00000000000000e1", Number: 601, Op: "add", After: "a law to repeal",
		Standards: []planstore.LawStandard{{ID: "s1", Standard: "x", Measure: "y"}}}
	rep := planstore.LawCard{ApprovalID: "appr_00000000000000e2", Number: 602, Op: "repeal_many",
		Repeals: []planstore.LawRepeal{{Law: "3", Before: "a law to repeal", Reason: "test"}}}
	_, err = planstore.RecordLaws(at, w.rt, []planstore.LawCard{add, rep})
	ok(t, err)
	if l, have, _ := planstore.CurrentLaw(ctx, w.rt, "3"); !have || l.Status != planstore.RepealedStatus {
		t.Fatalf("law 3 before the purge = %+v, %v", l, have)
	}
	return w, add, rep
}

func lawGone(t *testing.T, w *world, n string) {
	t.Helper()
	if l, have, err := planstore.CurrentLaw(ctx, w.rt, n); err != nil || have {
		t.Fatalf("law %s is still in the register: %+v, %v", n, l, err)
	}
	if c := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1`, planstore.LawStream(n)); c != 0 {
		t.Fatalf("law %s still has %d records", n, c)
	}
	if c := w.count(t, `SELECT count(*) FROM agentrt.event_stream WHERE event_stream_id = $1`, planstore.LawStream(n)); c != 0 {
		t.Fatalf("law %s's stream is still there", n)
	}
}

// Seven days after its repeal a law is deleted by the laws pass; its cards
// are not written in again and its number is not given again.
func TestRepealedLawIsPurgedAfterSevenDays(t *testing.T) {
	t.Parallel()
	w, add, rep := purgeWorld(t, time.Now().Add(-8*24*time.Hour))
	gone, err := planstore.PurgeRepealedLaws(ctx, w.rt, time.Now())
	ok(t, err)
	if len(gone) != 1 || gone[0] != "3" {
		t.Fatalf("purged %v, want [3]", gone)
	}
	lawGone(t, w, "3")
	if c := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'law_purged'
	  AND body_json->'approval_ids' @> '["appr_00000000000000e1","appr_00000000000000e2"]'::jsonb AND NOT body_json ? 'text'`,
		planstore.LawPurgedStream("3")); c != 1 {
		t.Fatalf("the tombstone of law 3 is missing or wrong (%d)", c)
	}
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{add, rep})
	ok(t, err)
	lawGone(t, w, "3")
	next, err := planstore.NextLawNumber(ctx, w.rt)
	ok(t, err)
	if next != "4" {
		t.Fatalf("next number after purging law 3 = %q, want 4", next)
	}
	// Laws in force and material are untouched.
	for _, n := range []string{"1", "2"} {
		if _, have, _ := planstore.CurrentLaw(ctx, w.rt, n); !have {
			t.Fatalf("law %s went with the purge", n)
		}
		w.chainHolds(t, planstore.LawStream(n))
	}
	w.chainHolds(t, planstore.LawPurgedStream("3"))
}

// The positive control of the test above: a law repealed less than seven
// days ago stays, and the database refuses it even when asked directly.
func TestRecentlyRepealedLawStays(t *testing.T) {
	t.Parallel()
	w, _, _ := purgeWorld(t, time.Now().Add(-6*24*time.Hour))
	gone, err := planstore.PurgeRepealedLaws(ctx, w.rt, time.Now().Add(2*24*time.Hour))
	ok(t, err)
	if len(gone) != 0 {
		t.Fatalf("purged %v six days after the repeal", gone)
	}
	if _, have, _ := planstore.CurrentLaw(ctx, w.rt, "3"); !have {
		t.Fatal("law 3 went six days after its repeal")
	}
}

// The Owner's purge card deletes a repealed law at once; a law not repealed
// is never deleted, by the card or by the database function.
func TestPurgeCardDeletesAtOnce(t *testing.T) {
	t.Parallel()
	w, _, _ := purgeWorld(t, time.Now())
	purge := planstore.LawCard{ApprovalID: "appr_00000000000000e3", Number: 603, Op: "purge",
		Repeals: []planstore.LawRepeal{{Law: "3", Before: "a law to repeal", Reason: "Owner asked"}, {Law: "1", Before: "x", Reason: "not repealed"}}}
	for range 2 {
		_, err := planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{purge})
		ok(t, err)
	}
	lawGone(t, w, "3")
	if _, have, _ := planstore.CurrentLaw(ctx, w.rt, "1"); !have {
		t.Fatal("the purge card deleted law 1, which is not repealed")
	}
	var deleted int
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		deleted, _, err = eventlog.PurgeRepealedLaw(ctx, tx, planstore.LawStream("1"), true)
		return err
	}))
	if deleted != 0 {
		t.Fatalf("the database deleted %d records of law 1, which is not repealed", deleted)
	}
}

// 2026-11-24, the Owner asked whether keeping laws in the shared event table
// mixes them with data a clean-up rule could remove (law 42 asks growing data
// to have one). Since migration 0047 the one deletion is the function above;
// outside it nothing is deleted: not by the runtime role, not by the table's
// owner, not a repealed law, not with the setting forged. Whoever relaxes the
// table for a clean-up must keep law:* out of it, or this goes red.
func TestLawRecordsCannotBeDeleted(t *testing.T) {
	t.Parallel()
	w, _, _ := purgeWorld(t, time.Now().Add(-8*24*time.Hour))
	// As the table's owner, the role a clean-up migration would run as: the
	// runtime role cannot even alter the table, so trying only as it would
	// stay green with the protection gone (checked 2026-11-24: with the
	// trigger disabled, the owner's delete removes law:1).
	own, err := pgx.Connect(ctx, w.db.Owner)
	ok(t, err)
	defer own.Close(ctx)
	for name, q := range map[string]string{
		"delete":          `DELETE FROM agentrt.event WHERE event_stream_id = 'law:1'`,
		"delete repealed": `DELETE FROM agentrt.event WHERE event_stream_id = 'law:3'`,
		"update":          `UPDATE agentrt.event SET body_json = body_json || '{"text":"rewritten"}'::jsonb WHERE event_stream_id = 'law:1'`,
		"truncate":        `TRUNCATE agentrt.event`,
		"stream":          `DELETE FROM agentrt.event_stream WHERE event_stream_id = 'law:3'`,
		"forged":          `BEGIN; SET LOCAL agentrt.purge_law = 'law:1'; DELETE FROM agentrt.event WHERE event_stream_id = 'law:1'; COMMIT`,
	} {
		if _, err := own.Exec(ctx, q); err == nil {
			t.Errorf("%s of a law's records was allowed to the table's owner", name)
		}
		_, _ = own.Exec(ctx, `ROLLBACK`)
	}
	rt, err := pgx.Connect(ctx, w.db.Runtime)
	ok(t, err)
	defer rt.Close(ctx)
	if _, err := rt.Exec(ctx, `DELETE FROM agentrt.event WHERE event_stream_id = 'law:3'`); err == nil {
		t.Error("the runtime role deleted a law's records directly")
	}
	for _, n := range []string{"1", "3"} {
		if _, have, _ := planstore.CurrentLaw(ctx, w.rt, n); !have {
			t.Fatalf("law %s is gone after the attempts", n)
		}
		w.chainHolds(t, planstore.LawStream(n))
	}
}

// Origin: 2026-11-25, LAW-1 s5 (「废止即走」) had no reading: a repealed law is
// not in the register's view, so the laws route says how many wait to be
// deleted and how many are overdue, numbers and times only.
func TestPurgeQueueCountsWaitingAndOverdue(t *testing.T) {
	t.Parallel()
	fresh, _, _ := purgeWorld(t, time.Now())
	q, err := planstore.LawPurgeQueue(ctx, fresh.rt, time.Now())
	ok(t, err)
	if q.Waiting != 1 || q.Overdue != 0 || q.OldestRepealedAt == nil || q.NextPurgeAt == nil ||
		q.NextPurgeAt.Sub(*q.OldestRepealedAt) != planstore.LawPurgeAfter {
		t.Fatalf("just repealed: %+v", q)
	}
	// Positive control of the case above: repealed eight days ago, it is overdue.
	old, _, _ := purgeWorld(t, time.Now().Add(-8*24*time.Hour))
	q, err = planstore.LawPurgeQueue(ctx, old.rt, time.Now())
	ok(t, err)
	if q.Waiting != 1 || q.Overdue != 1 {
		t.Fatalf("eight days on: %+v", q)
	}
	// Deleted, it waits no more.
	_, err = planstore.PurgeRepealedLaws(ctx, old.rt, time.Now())
	ok(t, err)
	q, err = planstore.LawPurgeQueue(ctx, old.rt, time.Now())
	ok(t, err)
	if q.Waiting != 0 || q.Overdue != 0 || q.OldestRepealedAt != nil {
		t.Fatalf("after the purge: %+v", q)
	}
}
