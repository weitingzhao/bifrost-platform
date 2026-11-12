package store_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.5-18, the records' checks one at a time, and the recoveries: each
// case breaks one condition on a fresh state and names the reason it must
// be refused for.

func rwAppend(w *world, stream, actor, task string, body map[string]any) error {
	b := map[string]any{"text": "rework"}
	for k, v := range body {
		b[k] = v
	}
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: actor, TaskID: task, Body: b})
		return err
	})
}

func rwSeenBody(root, op, oldA string, sibs []store.ReworkSibling) map[string]any {
	list := []any{}
	for _, x := range sibs {
		list = append(list, map[string]any{"by": x.By, "by_op": x.ByOp, "new_a": x.NewA, "why": x.Why})
	}
	return map[string]any{"v": "1", "root": root, "op": op, "old_a": oldA, "set_key": store.ReworkSetKey(sibs), "siblings": list}
}

func (r *rwWorld) rwAll() []store.ReworkSibling {
	return []store.ReworkSibling{{By: r.b, ByOp: r.bOp, NewA: rwA1, Why: "different"}, {By: r.cc, ByOp: r.cOp, NewA: rwA2, Why: "different"},
		{By: r.d, ByOp: r.dOp, NewA: rwA3, Why: "not_comparable"}}
}

func sortedSibs(s []store.ReworkSibling) []store.ReworkSibling {
	out := append([]store.ReworkSibling(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].By < out[i].By {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func refusedFor(t *testing.T, name string, err error, reason string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Errorf("%s: %v, want refused for %q", name, err, reason)
	}
}

// Serial: FreezeEventClock. §3.1 one check at a time, before any record
// exists (so the duplicate check cannot stand in for another), then the
// record that passes, then the duplicate.
func TestReworkSeenAppendChecksOneAtATime(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	all := sortedSibs(r.rwAll())
	good := rwSeenBody(r.root, r.rootOp, dbOldA, all)
	seen := func(b map[string]any) map[string]any { return map[string]any{"landing_rework_seen": b} }
	part := sortedSibs(all[:2])
	refusal := map[string]any{"v": "1", "task": r.root, "reason": "stale"}
	for _, c := range []struct {
		name, stream, actor, task string
		body                      map[string]any
		reason                    string
	}{
		{"another actor", landingRepoStream, "owner", r.root, seen(good), "written by the controller"},
		{"another repository's stream", "landing:" + infraRepo, "controller", r.root, seen(good), "of the stream's repository"},
		{"under another task", landingRepoStream, "controller", r.b, seen(good), "written by the controller"},
		{"with another reader's record", landingRepoStream, "controller", r.root,
			map[string]any{"landing_rework_seen": good, "landing_refusal": refusal}, "its body is its text and this record alone"},
		{"a part of the set", landingRepoStream, "controller", r.root, seen(rwSeenBody(r.root, r.rootOp, dbOldA, part)),
			"siblings are not the step's landed siblings now"},
		{"old_a is not root's", landingRepoStream, "controller", r.root, seen(rwSeenBody(r.root, r.rootOp, rwA1, all)), "old_a is not the A op claimed"},
		{"op is another task's", landingRepoStream, "controller", r.root, seen(rwSeenBody(r.root, r.bOp, dbOldA, all)),
			"root's latest operation is not op"},
		{"root has landed", landingRepoStream, "controller", r.b, seen(rwSeenBody(r.b, r.bOp, rwA1, sortedSibs([]store.ReworkSibling{all[1], all[2]}))),
			"root's latest operation is not op"},
	} {
		refusedFor(t, c.name, rwAppend(r.w, c.stream, c.actor, c.task, c.body), c.reason)
	}
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 0 {
		t.Fatalf("%d records written by refused appends", n)
	}
	ok(t, rwAppend(r.w, landingRepoStream, "controller", r.root, seen(good)))
	refusedFor(t, "the same set again", rwAppend(r.w, landingRepoStream, "controller", r.root, seen(good)), "already recorded")

	// A closed step: the append itself refuses it.
	w2 := newRWWorld(t, &deliveredGit{})
	ok(t, store.InTx(ctx, w2.w.rt, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", root, s2, "stopped", "test")
		return err
	}))
	refusedFor(t, "a closed step", rwAppend(w2.w, landingRepoStream, "controller", w2.root,
		seen(rwSeenBody(w2.root, w2.rootOp, dbOldA, sortedSibs(w2.rwAll())))), "root's step is closed")
}

// Serial: FreezeEventClock. §3.2: no landing_replaced without the Owner's
// count_replaced on this card: not with no answer, not for a record that is
// not the trigger, not after still_owed, not a second for one question.
func TestReworkReplacedNeedsCountReplaced(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	all := sortedSibs(r.rwAll())
	seq, st, err := store.RecordReworkSeen(ctx, r.w.rt, "bifrost-platform", r.root, r.rootOp, dbOldA, all)
	ok(t, err)
	if st != "found" {
		t.Fatalf("fixture: %q", st)
	}
	replaced := func(q string, at int64) map[string]any {
		return map[string]any{"landing_replaced": map[string]any{"v": "1", "root": r.root, "op": r.rootOp, "trigger_seq": at, "question_id": q,
			"set_key": store.ReworkSetKey(all)}}
	}
	refusedFor(t, "no answer", rwAppend(r.w, landingRepoStream, "controller", r.root, replaced("q-none", seq)), "count_replaced on this card")
	refusedFor(t, "no such record", rwAppend(r.w, landingRepoStream, "controller", r.root, replaced("q-none", seq+1000)), "trigger_seq is not")

	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	f.answer(card.QuestionID, letterOf(t, card, "still_owed"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))
	refusedFor(t, "after still_owed", rwAppend(r.w, landingRepoStream, "controller", r.root, replaced(card.QuestionID, seq)), "count_replaced on this card")
	if n := rwNotes(t, r.w, "landing_replaced"); n != 0 {
		t.Fatalf("%d replaced records", n)
	}

	r2 := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r2.rwPass(t)
	}
	f2 := newFakeCards(func() time.Time { return r2.now })
	cardPass(t, r2.w, f2, r2.now)
	c2 := onlyCard(t, r2.w, store.CardReworkLanded)
	f2.answer(c2.QuestionID, letterOf(t, c2, "count_replaced"), r2.now.Add(time.Minute))
	cardPass(t, r2.w, f2, r2.now.Add(2*time.Minute))
	all2 := sortedSibs(r2.rwAll())
	seq2, _, err := store.RecordReworkSeen(ctx, r2.w.rt, "bifrost-platform", r2.root, r2.rootOp, dbOldA, all2)
	ok(t, err)
	refusedFor(t, "a second for the question", rwAppend(r2.w, landingRepoStream, "controller", r2.root, map[string]any{"landing_replaced": map[string]any{
		"v": "1", "root": r2.root, "op": r2.rootOp, "trigger_seq": seq2, "question_id": c2.QuestionID, "set_key": store.ReworkSetKey(all2)}}),
		"already has its landing_replaced")
}

// Serial: FreezeEventClock, SetHook. The answer's two records are one
// transaction: the second failing (its context cancelled at the hook) rolls
// the outcome back too; handled again, both are written once.
func TestReworkAnswerIsOneTransaction(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	at := r.now.Add(time.Minute)
	f.answer(card.QuestionID, letterOf(t, card, "count_replaced"), at)
	q := f.byID[card.QuestionID].q
	one, cancel := context.WithCancel(ctx)
	unhook := store.SetHook(func(point, _ string) {
		if point == "rework-replacing" {
			cancel()
		}
	})
	_, _, err := store.HandleLandingCardAnswer(one, r.w.rt, card, q, at)
	unhook()
	if err == nil {
		t.Fatal("the cancelled answer was handled")
	}
	if rwNotes(t, r.w, "landing_card_outcome") != 0 || rwNotes(t, r.w, "landing_replaced") != 0 {
		t.Fatalf("a part of the answer stayed: %d outcomes, %d replaced", rwNotes(t, r.w, "landing_card_outcome"), rwNotes(t, r.w, "landing_replaced"))
	}
	outcome, why, err := store.HandleLandingCardAnswer(ctx, r.w.rt, card, q, at)
	ok(t, err)
	if outcome != "applied" || why != "counted_replaced" || rwNotes(t, r.w, "landing_replaced") != 1 {
		t.Fatalf("handled again: %s/%s, %d replaced", outcome, why, rwNotes(t, r.w, "landing_replaced"))
	}
	// Handled a third time (an Ack lost): the stored outcome, nothing new.
	if _, _, err := store.HandleLandingCardAnswer(ctx, r.w.rt, card, q, at); err != nil || rwNotes(t, r.w, "landing_replaced") != 1 {
		t.Fatalf("handled again after it was recorded: %v, %d replaced", err, rwNotes(t, r.w, "landing_replaced"))
	}
	r.rwLanded(t, "counted delivered", plan.True)
}

// Serial: FreezeEventClock, SetHook. A record whose write fails is written
// on a later pass from the comparisons already settled, with no new read;
// two writers at once write it once.
func TestReworkSeenWriteRetriedAndWrittenOnce(t *testing.T) {
	differ := &deliveredGit{}
	r := newRWWorld(t, differ)
	r.rwPass(t)
	r.rwPass(t)
	one, cancel := context.WithCancel(ctx)
	unhook := store.SetHook(func(point, _ string) {
		if point == "rework-seen-writing" {
			cancel()
		}
	})
	r.now = r.c.dbTick()
	err := r.ctl.LandingDeliveredByPass(one, "bifrost-platform", landingState(t, r.w, landingRepoStream))
	unhook()
	if err == nil || rwNotes(t, r.w, "landing_rework_seen") != 0 || differ.calls != 3 {
		t.Fatalf("the failed write: %v, %d records, %d reads", err, rwNotes(t, r.w, "landing_rework_seen"), differ.calls)
	}
	r.rwPass(t)
	if rwNotes(t, r.w, "landing_rework_seen") != 1 || differ.calls != 3 {
		t.Fatalf("after the failed write: %d records, %d reads; want 1, 3", rwNotes(t, r.w, "landing_rework_seen"), differ.calls)
	}

	r2 := newRWWorld(t, &deliveredGit{})
	all := r2.rwAll()
	var wg sync.WaitGroup
	statuses := make([]string, 2)
	errs := make([]error, 2)
	conns := []*pgx.Conn{r2.w.db.Connect(t, r2.w.db.Runtime), r2.w.db.Connect(t, r2.w.db.Runtime)}
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, statuses[i], errs[i] = store.RecordReworkSeen(ctx, conns[i], "bifrost-platform", r2.root, r2.rootOp, dbOldA, all)
		}()
	}
	wg.Wait()
	ok(t, errs[0])
	ok(t, errs[1])
	got := statuses[0] + "+" + statuses[1]
	if (got != "written+found" && got != "found+written") || rwNotes(t, r2.w, "landing_rework_seen") != 1 {
		t.Fatalf("two writers: %s, %d records", got, rwNotes(t, r2.w, "landing_rework_seen"))
	}
}

// Serial: FreezeEventClock. A set the Owner answered still_owed comes back
// after another: its own record and seq again, its answer stands, no card.
func TestReworkSetComesBack(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	first := onlyCard(t, r.w, store.CardReworkLanded)
	f.answer(first.QuestionID, letterOf(t, first, "still_owed"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))

	moveToStep(t, r.w, r.cc, dbStep)
	r.rwPass(t)
	cardPass(t, r.w, f, r.now.Add(laterPass))
	if f.asks != 2 {
		t.Fatalf("the smaller set was not asked: %d asks", f.asks)
	}
	moveToStep(t, r.w, r.cc, s2)
	r.rwPass(t)
	cardPass(t, r.w, f, r.now.Add(2*laterPass))
	if f.asks != 2 || rwNotes(t, r.w, "landing_rework_seen") != 2 {
		t.Fatalf("the first set again: %d asks, %d records; want 2, 2", f.asks, rwNotes(t, r.w, "landing_rework_seen"))
	}
	r.rwLanded(t, "the first set is still owed", plan.False)
}

// Serial: FreezeEventClock. The answer is judged against the trigger as it
// is then: a sibling reverted before the answer makes it moot.
func TestReworkAnswerAfterARevertIsMoot(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	r.c.dbTick()
	ok(t, appendLanding(r.w, landingRepoStream, newLandingRecord(r.cOp, "reverted", "R", commitB, "by", "owner")))
	f.answer(card.QuestionID, letterOf(t, card, "count_replaced"), r.now.Add(time.Minute))
	q := f.byID[card.QuestionID].q
	outcome, why, err := store.HandleLandingCardAnswer(ctx, r.w.rt, card, q, r.now.Add(time.Minute))
	ok(t, err)
	if outcome != "moot" || why != "trigger_gone" || rwNotes(t, r.w, "landing_replaced") != 0 {
		t.Fatalf("answered after a revert: %s/%s, %d replaced", outcome, why, rwNotes(t, r.w, "landing_replaced"))
	}
}

// Serial: FreezeEventClock. The excuse is for the declined root alone: an
// implementation of the step that has not landed still holds it, and the
// STG reading is the landed siblings' only.
func TestReworkExcuseIsTheRootsAlone(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	before := stepEvidence(t, r.w, s2, "accepted", 1).STG
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	f.answer(card.QuestionID, letterOf(t, card, "count_replaced"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))
	r.rwLanded(t, "counted delivered", plan.True)
	if after := stepEvidence(t, r.w, s2, "accepted", 1).STG; after != before {
		t.Fatalf("step_stg_settled moved with the excuse: %s → %s", before, after)
	}
	dbAccepted(t, r.w, r.c, s2, "bifrost-platform")
	r.rwLanded(t, "another implementation, not landed", plan.False)
}

// Serial: FreezeEventClock. A closed step is put off, not remembered: once
// the step is active again the record is written and the card asked, by the
// same controller.
func TestReworkClosedStepComesBack(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	state := func(to string) {
		t.Helper()
		ok(t, store.InTx(ctx, r.w.rt, func(tx pgx.Tx) error {
			_, err := store.SetStepState(ctx, tx, "controller", root, s2, to, "test")
			return err
		}))
	}
	state("stopped")
	for range 4 {
		r.rwPass(t)
	}
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 0 {
		t.Fatalf("%d records for a stopped step", n)
	}
	state("active")
	r.rwPass(t)
	r.c.at = r.c.at.Add(11 * time.Minute) // past the controller's put-off
	r.rwPass(t)
	if n := rwNotes(t, r.w, "landing_rework_seen"); n != 1 {
		t.Fatalf("%d records once the step is active again, want 1", n)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	if f.asks != 1 {
		t.Fatalf("%d asks once the step is active again", f.asks)
	}
}

// Serial: FreezeEventClock. §6.2 through the real trigger: a set too large
// for even task ids prepares no card.
func TestReworkTooManySiblingsPrepareNoCard(t *testing.T) {
	slowTest(t)
	w, c := dbWorld(t)
	rootTask := dbAccepted(t, w, c, s2, "bifrost-platform")
	rootOp := dbOp(t, w, c, rootTask, dbOldA, "declined")
	var sibs []store.ReworkSibling
	for i := range 56 {
		id := dbAccepted(t, w, c, s2, "bifrost-platform")
		a := fmt.Sprintf("%040x", i+1)
		sibs = append(sibs, store.ReworkSibling{By: id, ByOp: dbOp(t, w, c, id, a, "landed"), NewA: a, Why: "different"})
	}
	now := c.dbTick()
	_, st, err := store.RecordReworkSeen(ctx, w.rt, "bifrost-platform", rootTask, rootOp, dbOldA, sibs)
	ok(t, err)
	if st != "written" {
		t.Fatalf("fixture: %q", st)
	}
	prepared, err := store.PrepareLandingCards(ctx, w.rt, "bifrost-platform", now)
	ok(t, err)
	for _, cd := range cardsNow(t, w) {
		if cd.Kind == store.CardReworkLanded {
			t.Fatalf("a card was prepared for %d siblings: %v", len(sibs), prepared)
		}
	}
	// The explanation says so from the same judgment (R5:5.5-19 §4.3):
	// the current set is recorded and not asked.
	w.rule(t, s2, `{"step_landed":{}}`)
	exActive(t, w, s2)
	b := exLanded(t, exStep(t, exSteps(t, w), s2))[rootTask]
	rw, _ := b["rework"].(map[string]any)
	if seen, _ := rw["seen"].(map[string]any); b["state"] != "landing_declined" || seen["current"] != true || rw["not_asked"] != true || rw["card"] != nil {
		t.Fatalf("explained as %v", b)
	}
}

// Serial: FreezeEventClock. RecordReworkSeen reads again, under the lock,
// what would excuse the root without a card: with no record of the set
// yet, a same change landed or an excuse in force makes it stale.
func TestReworkSeenReadsTheExcusesAgain(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	r.c.dbTick()
	ok(t, store.RecordLandingDeliveredBy(ctx, r.w.rt, "bifrost-platform", deliveredBy(r.root, r.rootOp, r.b, r.bOp, dbOldA, rwA1)))
	if seq, st, err := store.RecordReworkSeen(ctx, r.w.rt, "bifrost-platform", r.root, r.rootOp, dbOldA, r.rwAll()); err != nil || st != "stale" || seq != 0 {
		t.Fatalf("a same change landed: %d %q %v, want stale", seq, st, err)
	}

	// An excuse in force for a smaller set: a fourth sibling lands, the new
	// set has no record, and the excuse still holds.
	r2 := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r2.rwPass(t)
	}
	f := newFakeCards(func() time.Time { return r2.now })
	cardPass(t, r2.w, f, r2.now)
	card := onlyCard(t, r2.w, store.CardReworkLanded)
	f.answer(card.QuestionID, letterOf(t, card, "count_replaced"), r2.now.Add(time.Minute))
	cardPass(t, r2.w, f, r2.now.Add(2*time.Minute))
	e := dbAccepted(t, r2.w, r2.c, s2, "bifrost-platform")
	eA := strings.Repeat("a7", 20)
	eOp := dbOp(t, r2.w, r2.c, e, eA, "landed")
	more := append(r2.rwAll(), store.ReworkSibling{By: e, ByOp: eOp, NewA: eA, Why: "different"})
	if seq, st, err := store.RecordReworkSeen(ctx, r2.w.rt, "bifrost-platform", r2.root, r2.rootOp, dbOldA, more); err != nil || st != "stale" || seq != 0 {
		t.Fatalf("an excuse in force: %d %q %v, want stale", seq, st, err)
	}
	r2.rwLanded(t, "the excuse holds with a fourth sibling", plan.True)
}

// Serial: FreezeEventClock. A prepared rework card must bind to its root's
// record for the operation: one naming another seq is refused.
func TestReworkPreparedCardBindsItsRecord(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	for range 3 {
		r.rwPass(t)
	}
	_, err := store.PrepareLandingCards(ctx, r.w.rt, "bifrost-platform", r.now)
	ok(t, err)
	var body map[string]any
	ok(t, r.w.rt.QueryRow(ctx, `SELECT body_json->'landing_card_prepared' FROM agentrt.event WHERE event_stream_id = $1
	  AND body_json->'landing_card_prepared'->>'kind' = $2`, store.CardsStream("bifrost-platform"), store.CardReworkLanded).Scan(&body))
	seq, _ := body["trigger_seq"].(float64)
	// Each forgery is otherwise a well-formed prepared card (its prep names
	// its trigger and number, its fingerprint is its request's), so only
	// the binding can refuse it.
	for name, c := range map[string]struct {
		seq int64
		n   int
		op  string
	}{
		"another seq":       {int64(seq) + 1000, 1, r.rootOp},
		"another operation": {int64(seq), 2, r.bOp},
	} {
		forged := rwInts(body).(map[string]any)
		forged["trigger_seq"], forged["n"], forged["op"] = c.seq, c.n, c.op
		forged["prep"] = store.CardPrepID(store.CardReworkLanded, c.seq, c.n)
		fp, err := canon.Sum(forged["request"])
		ok(t, err)
		forged["request_fp"] = strings.TrimPrefix(fp, "sha256:")
		err = rwAppend(r.w, store.CardsStream("bifrost-platform"), "controller", "", map[string]any{"landing_card_prepared": forged})
		refusedFor(t, name, err, "rework_landed card's trigger is its root's landing_rework_seen")
	}
}

// Serial: FreezeEventClock. wait holds the card a day, then it is asked
// again; a pair the remote cannot compare is recorded not_comparable.
func TestReworkWaitAndNotComparable(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{err: fmt.Errorf("%w: binary", landinggit.ErrNotComparable)})
	for range 3 {
		r.rwPass(t)
	}
	var whys []string
	rows, err := r.w.rt.Query(ctx, `SELECT s->>'why' FROM agentrt.event, jsonb_array_elements(body_json->'landing_rework_seen'->'siblings') s
	  WHERE body_json ? 'landing_rework_seen'`)
	ok(t, err)
	for rows.Next() {
		var w string
		ok(t, rows.Scan(&w))
		whys = append(whys, w)
	}
	rows.Close()
	if strings.Join(whys, ",") != "not_comparable,not_comparable,not_comparable" {
		t.Fatalf("whys %v", whys)
	}
	f := newFakeCards(func() time.Time { return r.now })
	cardPass(t, r.w, f, r.now)
	card := onlyCard(t, r.w, store.CardReworkLanded)
	f.answer(card.QuestionID, letterOf(t, card, "wait"), r.now.Add(time.Minute))
	cardPass(t, r.w, f, r.now.Add(2*time.Minute))
	cardPass(t, r.w, f, r.now.Add(time.Hour))
	if f.asks != 1 {
		t.Fatalf("asked again within the hold: %d", f.asks)
	}
	cardPass(t, r.w, f, r.now.Add(store.CardHoldFor+2*time.Minute))
	if f.asks != 2 {
		t.Fatalf("not asked again after the hold: %d", f.asks)
	}
}

// Serial: FreezeEventClock, SetHook. Two writers meet at the lock: the
// first holds it at its write until the second has started; one record.
func TestReworkSeenWritersMeetAtTheLock(t *testing.T) {
	r := newRWWorld(t, &deliveredGit{})
	all := r.rwAll()
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unhook := store.SetHook(func(point, _ string) {
		if point == "rework-seen-writing" {
			once.Do(func() { close(held); <-release })
		}
	})
	defer unhook()
	conns := []*pgx.Conn{r.w.db.Connect(t, r.w.db.Runtime), r.w.db.Connect(t, r.w.db.Runtime)}
	statuses, errs := make([]string, 2), make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, statuses[0], errs[0] = store.RecordReworkSeen(ctx, conns[0], "bifrost-platform", r.root, r.rootOp, dbOldA, all)
	}()
	<-held
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, statuses[1], errs[1] = store.RecordReworkSeen(ctx, conns[1], "bifrost-platform", r.root, r.rootOp, dbOldA, all)
	}()
	// The second waits on the stream lock the first holds.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting bool
		ok(t, r.w.rt.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database())`).Scan(&waiting))
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second writer never waited on the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	ok(t, errs[0])
	ok(t, errs[1])
	if statuses[0] != "written" || statuses[1] != "found" || rwNotes(t, r.w, "landing_rework_seen") != 1 {
		t.Fatalf("writers at the lock: %v, %d records", statuses, rwNotes(t, r.w, "landing_rework_seen"))
	}
}

// rwInts turns the whole numbers a JSON read gives back as float64 into
// int64 again, as the record was written (canon takes integers only).
func rwInts(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = rwInts(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = rwInts(e)
		}
		return out
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
	}
	return v
}
