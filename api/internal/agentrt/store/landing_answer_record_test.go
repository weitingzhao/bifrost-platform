package store_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// CUT §8 (review 647): an answered landing card is handled once, recorded on
// the repository stream in the transaction that applies it, and acknowledged
// only after that commits. A refusal is a record, not an error.

// applyRetry answers try_again on a fresh card and fails unless it applied.
func applyRetry(db store.DB, root, op, text string, at time.Time) error {
	q := store.Question{ID: fmt.Sprintf("q-retry-%s-%s-%d", root, op, at.UnixNano()), Status: store.QuestionAnswered, Channel: "console",
		Option: "try_again", Text: text, AnsweredBy: "owner", AnsweredAt: at.Format(time.RFC3339Nano)}
	res, err := store.HandleLandingAnswer(ctx, db, q, root, op, at)
	if err != nil {
		return err
	}
	if res.Outcome != store.LandingAnswerApplied {
		return fmt.Errorf("retry %s: %s (%s)", op, res.Outcome, res.Why)
	}
	return nil
}

// failedRoot is a real task of the active mission M-1 whose operation op
// failed with why, written at at.
func failedRoot(t *testing.T, w *world, op, why string) string {
	t.Helper()
	root := w.ready(t, store.Task{OffPlanReason: "answer " + op})
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", why)))
	return root
}

func answerCard(id, option string, at time.Time) store.Question {
	return store.Question{ID: id, Status: store.QuestionAnswered, Channel: "console", Option: option, Text: "owner",
		AnsweredBy: "owner", AnsweredAt: at.Format(time.RFC3339Nano)}
}

// answerRecords counts the record notes of a question and the retries of a
// root on the repository stream.
func answerRecords(t *testing.T, w *world, question, root string) (records, retries int) {
	t.Helper()
	ok(t, w.rt.QueryRow(ctx, `SELECT
	    count(*) FILTER (WHERE body_json->'landing_answer'->>'question_id' = $2),
	    count(*) FILTER (WHERE body_json->'landing'->>'kind' = 'retry' AND body_json->'landing'->>'root' = $3)
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, landingRepoStream, question, root).Scan(&records, &retries))
	return records, retries
}

// Serial: FreezeEventClock.
func TestLandingAnswerRefusedAfterCloseIsRecorded(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "refused:1", "bound")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "stopped", "test") }))
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-refused", "try_again", at.Add(time.Minute))

	res, err := store.HandleLandingAnswer(ctx, w.rt, q, root, "refused:1", at.Add(time.Minute))
	ok(t, err)
	if res.Outcome != store.LandingAnswerRefused || res.Why != "mission_not_active" || !res.Recorded {
		t.Fatalf("a retry under a stopped mission: %+v", res)
	}
	again, err := store.HandleLandingAnswer(ctx, w.rt, q, root, "refused:1", at.Add(time.Minute))
	ok(t, err)
	if again.Outcome != res.Outcome || again.Why != res.Why || again.Recorded {
		t.Fatalf("second pass: %+v", again)
	}
	if records, retries := answerRecords(t, w, "q-refused", root); records != 1 || retries != 0 {
		t.Fatalf("records=%d retries=%d", records, retries)
	}
	if landingState(t, w, landingRepoStream).Operations["refused:1"].Retried {
		t.Fatal("retry folded under a stopped mission")
	}
}

// Serial: FreezeEventClock.
func TestLandingAnswerAckAfterCommitAndOnlyOnce(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "ack:1", "stale")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-ack", "try_again", at.Add(time.Minute))
	read := func(context.Context, string, string) (store.Question, bool) { return q, true }

	var acks []string
	fail := true
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	other, err := w.db.Pool(t, w.db.Runtime).Acquire(ctx)
	ok(t, err)
	defer other.Release()
	ctl.Sources.Ack = func(_ context.Context, id string) error {
		// The record and the retry are committed before the card is acked:
		// another connection sees them.
		var records, retries int
		ok(t, other.Conn().QueryRow(ctx, `SELECT
		    count(*) FILTER (WHERE body_json->'landing_answer'->>'question_id' = $2),
		    count(*) FILTER (WHERE body_json->'landing'->>'kind' = 'retry' AND body_json->'landing'->>'root' = $3)
		  FROM agentrt.event WHERE event_stream_id = $1`, landingRepoStream, id, root).Scan(&records, &retries))
		if records != 1 || retries != 1 {
			t.Errorf("ack before commit: records=%d retries=%d", records, retries)
		}
		acks = append(acks, id)
		if fail {
			fail = false
			return errors.New("question store down")
		}
		return nil
	}
	if _, err := ctl.LandingAnswer(ctx, root, "ack:1", read); err == nil {
		t.Fatal("a failed ack was not reported")
	}
	// The next pass, or a restarted controller, finds the record and only acks.
	ctl2 := controller.Controller{DB: w.rt, Env: "stg", Sources: ctl.Sources}
	// A later operation by now does not change the stored verdict.
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("ack:2", "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	res, err := ctl2.LandingAnswer(ctx, root, "ack:1", read)
	ok(t, err)
	if res.Outcome != store.LandingAnswerApplied || res.Recorded || !res.Acked {
		t.Fatalf("second pass: %+v", res)
	}
	if records, retries := answerRecords(t, w, "q-ack", root); records != 1 || retries != 1 || len(acks) != 2 {
		t.Fatalf("records=%d retries=%d acks=%v", records, retries, acks)
	}
}

// commitFailDB fails the first commit: the transaction rolls back as a
// database failure would.
type commitFailDB struct {
	store.DB
	failed *bool
}
type commitFailTx struct {
	pgx.Tx
	failed *bool
}

func (d commitFailDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	if err != nil {
		return nil, err
	}
	return &commitFailTx{Tx: tx, failed: d.failed}, nil
}
func (tx *commitFailTx) Commit(c context.Context) error {
	if !*tx.failed {
		*tx.failed = true
		_ = tx.Tx.Rollback(c)
		return errors.New("commit lost")
	}
	return tx.Tx.Commit(c)
}

// Serial: FreezeEventClock.
func TestLandingAnswerDatabaseFailureLeavesNothing(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "transient:1", "ci_red")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-transient", "try_again", at.Add(time.Minute))
	read := func(context.Context, string, string) (store.Question, bool) { return q, true }
	acked := 0
	failed := false
	ctl := controller.Controller{DB: commitFailDB{DB: w.rt, failed: &failed}, Env: "stg"}
	ctl.Sources.Ack = func(context.Context, string) error { acked++; return nil }
	if _, err := ctl.LandingAnswer(ctx, root, "transient:1", read); err == nil {
		t.Fatal("a lost commit was not reported")
	}
	if records, retries := answerRecords(t, w, "q-transient", root); records != 0 || retries != 0 || acked != 0 {
		t.Fatalf("after a lost commit: records=%d retries=%d acked=%d", records, retries, acked)
	}
	res, err := ctl.LandingAnswer(ctx, root, "transient:1", read)
	ok(t, err)
	if res.Outcome != store.LandingAnswerApplied || !res.Recorded || acked != 1 {
		t.Fatalf("next pass: %+v acked=%d", res, acked)
	}
}

// Serial: FreezeEventClock.
func TestLandingAnswerVerdicts(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	type c struct {
		name, outcome, why, option string
		fail                       string // the operation's failure; "" leaves it claimed
		setup                      func(root, op string)
		answered                   time.Time
	}
	later := at.Add(time.Hour)
	cases := []c{
		// Moot: nothing at all is needed.
		{"retried", store.LandingAnswerMoot, "already_retried", "try_again", "bound", func(root, op string) { ok(t, applyRetry(w.rt, root, op, "first", later)) }, later},
		{"declined", store.LandingAnswerMoot, "already_declined", "do_not_merge", "bound", func(root, op string) {
			_, err := store.HandleLandingAnswer(ctx, w.rt, answerCard("q-first-"+op, "do_not_merge", later), root, op, later)
			ok(t, err)
		}, later},
		{"superseded", store.LandingAnswerMoot, "superseded", "try_again", "bound", func(root, op string) {
			ok(t, applyRetry(w.rt, root, op, "first", later))
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op+":2", "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
		}, later},
		// Refused: not carried out; the Owner answers a new card (review 655 R1).
		{"unknown", store.LandingAnswerRefused, "unknown_choice", "maybe", "bound", nil, later},
		{"early", store.LandingAnswerRefused, "answered_before", "try_again", "bound", nil, at.Add(-time.Hour)},
		{"running", store.LandingAnswerRefused, "not_failed", "try_again", "", nil, later},
		{"conflict", store.LandingAnswerRefused, "not_retryable", "try_again", "conflict", nil, later},
		{"floor", store.LandingAnswerRefused, "not_retryable", "try_again", "floor", nil, later},
		// Applied.
		{"decline", store.LandingAnswerApplied, "declined", "do_not_merge", "", nil, later},
	}
	for i, k := range cases {
		op := fmt.Sprintf("verdict:%d", i)
		store.FreezeEventClock(at)
		root := w.ready(t, store.Task{OffPlanReason: "verdict " + k.name})
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
		if k.fail != "" {
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", k.fail)))
		}
		store.FreezeEventClock(later)
		if k.setup != nil {
			k.setup(root, op)
		}
		before := landingState(t, w, landingRepoStream)
		id := "q-verdict-" + k.name
		res, err := store.HandleLandingAnswer(ctx, w.rt, answerCard(id, k.option, k.answered), root, op, k.answered)
		ok(t, err)
		if res.Outcome != k.outcome || res.Why != k.why || !res.Recorded {
			t.Fatalf("%s: %+v", k.name, res)
		}
		if records, _ := answerRecords(t, w, id, root); records != 1 {
			t.Fatalf("%s: records=%d", k.name, records)
		}
		// A record note is not a landing record: only an applied answer
		// changes the fold.
		if after := landingState(t, w, landingRepoStream); (k.outcome == store.LandingAnswerApplied) == reflect.DeepEqual(after, before) {
			t.Fatalf("%s: the fold changed=%v", k.name, !reflect.DeepEqual(after, before))
		}
	}
}

// Serial: FreezeEventClock. A stored verdict is returned before anything is
// judged again: a later operation, or a mission that came back, does not
// change what the card already did (review 655 R3).
func TestLandingAnswerStoredVerdictComesFirst(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "stored:1", "bound")
	store.FreezeEventClock(at.Add(time.Minute))
	applied := answerCard("q-stored-applied", "try_again", at.Add(time.Minute))
	res, err := store.HandleLandingAnswer(ctx, w.rt, applied, root, "stored:1", at.Add(time.Minute))
	ok(t, err)
	if res.Outcome != store.LandingAnswerApplied {
		t.Fatal(res)
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("stored:2", "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	again, err := store.HandleLandingAnswer(ctx, w.rt, applied, root, "stored:1", at.Add(time.Minute))
	ok(t, err)
	if again.Outcome != store.LandingAnswerApplied || again.Why != "retry" || again.Recorded {
		t.Fatalf("after a successor: %+v", again)
	}

	other := failedRoot(t, w, "stored:3", "stale")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "test") }))
	paused := answerCard("q-stored-paused", "try_again", at.Add(time.Minute))
	res, err = store.HandleLandingAnswer(ctx, w.rt, paused, other, "stored:3", at.Add(time.Minute))
	ok(t, err)
	if res.Outcome != store.LandingAnswerRefused || res.Why != "mission_not_active" {
		t.Fatal(res)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	again, err = store.HandleLandingAnswer(ctx, w.rt, paused, other, "stored:3", at.Add(time.Minute))
	ok(t, err)
	if again.Outcome != store.LandingAnswerRefused || again.Recorded {
		t.Fatalf("a refusal turned into a retry when the mission came back: %+v", again)
	}
	if _, retries := answerRecords(t, w, "q-stored-paused", other); retries != 0 {
		t.Fatal("retry written for a refused card")
	}
	// The answer as given is kept with the record (review 655 R2).
	var text, channel string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing_answer'->>'answer_text', body_json->'landing_answer'->>'channel' FROM agentrt.event
	  WHERE event_stream_id = $1 AND body_json->'landing_answer'->>'question_id' = 'q-stored-paused'`, landingRepoStream).Scan(&text, &channel))
	if text != "owner" || channel != "console" {
		t.Fatal(text, channel)
	}
}

// Serial: SetHook, FreezeEventClock. A failure between the business record
// and the verdict leaves neither (review 655 R3).
func TestLandingAnswerFailureBetweenAppendsLeavesNothing(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "between:1", "ci_red")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-between", "try_again", at.Add(time.Minute))
	cut, cancel := context.WithCancel(ctx)
	defer store.SetHook(func(point, _ string) {
		if point == "landing answer: business appended" {
			cancel()
		}
	})()
	if _, err := store.HandleLandingAnswer(cut, w.rt, q, root, "between:1", at.Add(time.Minute)); err == nil {
		t.Fatal("a failure after the retry was not reported")
	}
	if records, retries := answerRecords(t, w, "q-between", root); records != 0 || retries != 0 {
		t.Fatalf("records=%d retries=%d", records, retries)
	}
}

// Serial: FreezeEventClock. A card bound to no operation of its root, or
// recorded for another root, is S3's binding error: nothing is recorded.
func TestLandingAnswerBindingMismatch(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	a := failedRoot(t, w, "bind:a", "bound")
	b := failedRoot(t, w, "bind:b", "bound")
	q := answerCard("q-bind", "try_again", at)
	if _, err := store.HandleLandingAnswer(ctx, w.rt, q, b, "bind:a", at); !errors.Is(err, store.ErrLandingAnswerBinding) {
		t.Fatal("an operation of another root", err)
	}
	ok(t, applyRetry(w.rt, a, "bind:a", "", at))
	recorded := answerCard("q-bind-recorded", "do_not_merge", at)
	_, err := store.HandleLandingAnswer(ctx, w.rt, recorded, a, "bind:a", at)
	ok(t, err)
	if _, err := store.HandleLandingAnswer(ctx, w.rt, recorded, b, "bind:b", at); !errors.Is(err, store.ErrLandingAnswerBinding) {
		t.Fatal("a recorded card under another root", err)
	}
	if records, _ := answerRecords(t, w, "q-bind", b); records != 0 {
		t.Fatal("a binding error was recorded")
	}
}

// Serial: FreezeEventClock.
func TestLandingAnswerNotRetryableIsRefused(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "conflict:1", "conflict")
	store.FreezeEventClock(at.Add(time.Minute))
	res, err := store.HandleLandingAnswer(ctx, w.rt, answerCard("q-conflict", "try_again", at.Add(time.Minute)), root, "conflict:1", at.Add(time.Minute))
	ok(t, err)
	if res.Outcome != store.LandingAnswerRefused || res.Why != "not_retryable" || !res.Recorded {
		t.Fatalf("retry after conflict: %+v", res)
	}
	if _, retries := answerRecords(t, w, "q-conflict", root); retries != 0 {
		t.Fatal("retry written after conflict")
	}
}

// Serial: FreezeEventClock.
func TestLandingAnswerConcurrentPassesActOnce(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "race:1", "bound")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-race", "try_again", at.Add(time.Minute))
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool := w.db.Pool(t, w.db.Runtime)
	var wg sync.WaitGroup
	results := make([]store.LandingAnswerResult, 2)
	errs := make([]error, 2)
	for i := range 2 {
		conn, err := pool.Acquire(bounded)
		ok(t, err)
		defer conn.Release()
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = store.HandleLandingAnswer(bounded, conn.Conn(), q, root, "race:1", at.Add(time.Minute))
		}()
	}
	wg.Wait()
	ok(t, errors.Join(errs...))
	if results[0].Recorded == results[1].Recorded || results[0].Outcome != store.LandingAnswerApplied || results[1].Outcome != store.LandingAnswerApplied {
		t.Fatalf("two passes: %+v", results)
	}
	if records, retries := answerRecords(t, w, "q-race", root); records != 1 || retries != 1 {
		t.Fatalf("records=%d retries=%d", records, retries)
	}
}

// Serial: FreezeEventClock. A note longer than the protocol's reason is
// still a retry: the retry keeps a bounded reason, the record the whole text
// (review 656 A-R5). 1001 bytes, and multibyte text cut on a rune.
func TestLandingAnswerLongTextStillRetries(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	for i, text := range []string{strings.Repeat("a", 1000), strings.Repeat("a", 1001), strings.Repeat("重试", 400)} {
		op := fmt.Sprintf("long:%d", i)
		store.FreezeEventClock(at)
		root := failedRoot(t, w, op, "bound")
		store.FreezeEventClock(at.Add(time.Minute))
		q := answerCard(fmt.Sprintf("q-long-%d", i), "try_again", at.Add(time.Minute))
		q.Text = text
		res, err := store.HandleLandingAnswer(ctx, w.rt, q, root, op, at.Add(time.Minute))
		ok(t, err)
		if res.Outcome != store.LandingAnswerApplied || res.Record["answer_text"] != text {
			t.Fatalf("%d bytes: %+v", len(text), res.Outcome)
		}
		var reason string
		ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'landing'->>'reason' FROM agentrt.event WHERE event_stream_id = $1
		  AND body_json->'landing'->>'kind' = 'retry' AND body_json->'landing'->>'root' = $2`, landingRepoStream, root).Scan(&reason))
		if len(reason) > 1000 || !utf8.ValidString(reason) || (len(text) <= 1000 && reason != text) {
			t.Fatalf("%d bytes: reason of %d bytes", len(text), len(reason))
		}
	}
}

// Serial: FreezeEventClock. A lost commit reports no verdict (review 656
// A-R2), and a stored record returns as committed, whatever the card says
// now (A-R3).
func TestLandingAnswerReportsOnlyWhatCommitted(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "commit:1", "bound")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-commit", "try_again", at.Add(time.Minute))
	failed := false
	res, err := store.HandleLandingAnswer(ctx, commitFailDB{DB: w.rt, failed: &failed}, q, root, "commit:1", at.Add(time.Minute))
	if err == nil || res.Outcome != "" || res.Recorded || res.Record != nil {
		t.Fatalf("a lost commit reported %+v (%v)", res, err)
	}
	first, err := store.HandleLandingAnswer(ctx, w.rt, q, root, "commit:1", at.Add(time.Minute))
	ok(t, err)
	q.Text = "changed since"
	again, err := store.HandleLandingAnswer(ctx, w.rt, q, root, "commit:1", at.Add(time.Minute))
	ok(t, err)
	if !first.Recorded || again.Recorded || !reflect.DeepEqual(again.Record, first.Record) || again.Record["answer_text"] != "owner" {
		t.Fatalf("the stored record did not come back as committed: %v / %v", first.Record, again.Record)
	}
}

// Serial: FreezeEventClock. A stored record is a credential: one that is not
// the controller's, or whose fields contradict each other, is an error and
// is not acked (review 656 A-R1). The append guard keeps anyone else from
// writing one.
func TestLandingAnswerStoredRecordMustBeTrustworthy(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "trust:1", "bound")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-trust", "do_not_merge", at.Add(time.Minute))
	_, err := store.HandleLandingAnswer(ctx, w.rt, q, root, "trust:1", at.Add(time.Minute))
	ok(t, err)
	admin := w.db.Connect(t, w.db.Admin)
	tamper := func(q string, args ...any) {
		t.Helper()
		tx, e := admin.Begin(ctx)
		ok(t, e)
		defer func() { _ = tx.Rollback(ctx) }()
		_, e = tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
		ok(t, e)
		_, e = tx.Exec(ctx, q, args...)
		ok(t, e)
		ok(t, tx.Commit(ctx))
	}
	// applied / declined under a try_again: a contradiction.
	tamper(`UPDATE agentrt.event SET body_json = jsonb_set(body_json, '{landing_answer,choice}', '"try_again"')
	  WHERE event_stream_id = $1 AND body_json->'landing_answer'->>'question_id' = 'q-trust'`, landingRepoStream)
	acked := 0
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	ctl.Sources.Ack = func(context.Context, string) error { acked++; return nil }
	read := func(context.Context, string, string) (store.Question, bool) { return q, true }
	if _, err := ctl.LandingAnswer(ctx, root, "trust:1", read); err == nil || acked != 0 {
		t.Fatalf("a contradictory record was trusted: err=%v acked=%d", err, acked)
	}
	tamper(`UPDATE agentrt.event SET body_json = jsonb_set(body_json, '{landing_answer,choice}', '"do_not_merge"'), actor_id = 'owner'
	  WHERE event_stream_id = $1 AND body_json->'landing_answer'->>'question_id' = 'q-trust'`, landingRepoStream)
	if _, err := ctl.LandingAnswer(ctx, root, "trust:1", read); err == nil || acked != 0 {
		t.Fatalf("a record not written by the controller was trusted: err=%v acked=%d", err, acked)
	}
	// Only the controller, on the repository stream, under the root it names.
	body := map[string]any{"question_id": "q-forged", "root": root, "op": "trust:1", "choice": "try_again", "outcome": "applied", "why": "retry",
		"by": "owner", "answered_at": at.Format(time.RFC3339Nano), "channel": "console", "answer_text": "", "mission_state": "active"}
	for name, ev := range map[string]store.Event{
		"owner":   {Stream: landingRepoStream, Type: "note", Actor: "owner", TaskID: root, Body: map[string]any{"text": "x", "landing_answer": body}},
		"no root": {Stream: landingRepoStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "x", "landing_answer": body}},
		"mission": {Stream: store.MissionStream("M-1"), Type: "note", Actor: "controller", MissionID: "M-1", TaskID: root, Body: map[string]any{"text": "x", "landing_answer": body}},
	} {
		if err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, e := store.Append(ctx, tx, ev); return e }); err == nil {
			t.Fatal("a landing_answer record was taken from", name)
		}
	}
}

// Serial: FreezeEventClock. Every record the decision writes reads back
// (review 657 B657-1): a superseded card with an unknown or empty choice,
// and a refusal under a mission in any real state (a root rebound to a
// mission awaiting the Owner).
func TestLandingAnswerEveryDecidedRecordReadsBack(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "back:1", "bound")
	later := at.Add(time.Hour)
	store.FreezeEventClock(later)
	ok(t, applyRetry(w.rt, root, "back:1", "first", later))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("back:2", "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	for _, choice := range []string{"none", ""} {
		q := answerCard("q-back-"+choice, choice, later)
		for pass := 0; pass < 2; pass++ {
			res, err := store.HandleLandingAnswer(ctx, w.rt, q, root, "back:1", later)
			ok(t, err)
			if res.Outcome != store.LandingAnswerMoot || res.Why != "superseded" || res.Recorded == (pass == 1) {
				t.Fatalf("%q pass %d: %+v", choice, pass, res)
			}
		}
	}
	other := failedRoot(t, w, "back:3", "bound")
	admin := w.db.Connect(t, w.db.Admin)
	tx, err := admin.Begin(ctx)
	ok(t, err)
	_, err = tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
	ok(t, err)
	_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET state = 'awaiting_owner' WHERE mission_id = 'M-1'`)
	ok(t, err)
	ok(t, tx.Commit(ctx))
	q := answerCard("q-back-awaiting", "try_again", later)
	for pass := 0; pass < 2; pass++ {
		res, err := store.HandleLandingAnswer(ctx, w.rt, q, other, "back:3", later)
		ok(t, err)
		if res.Outcome != store.LandingAnswerRefused || res.Why != "mission_not_active" || res.Record["mission_state"] != "awaiting_owner" {
			t.Fatalf("pass %d: %+v", pass, res)
		}
	}
}

// committedThenLostDB commits for real, then tells the caller the commit
// failed: the outcome a lost reply leaves (review 657 R657-3).
type committedThenLostDB struct{ store.DB }
type committedThenLostTx struct{ pgx.Tx }

func (d committedThenLostDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	if err != nil {
		return nil, err
	}
	return &committedThenLostTx{tx}, nil
}
func (tx *committedThenLostTx) Commit(c context.Context) error {
	if err := tx.Tx.Commit(c); err != nil {
		return err
	}
	return errors.New("reply lost")
}

// Serial: FreezeEventClock. A commit that landed but whose reply was lost:
// nothing is reported or acked; another connection sees the record; the next
// pass finds it, does not act again and acks.
func TestLandingAnswerCommitLandedReplyLost(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "lost:1", "bound")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-lost-reply", "try_again", at.Add(time.Minute))
	read := func(context.Context, string, string) (store.Question, bool) { return q, true }
	acked := 0
	ack := func(context.Context, string) error { acked++; return nil }
	ctl := controller.Controller{DB: committedThenLostDB{w.rt}, Env: "stg"}
	ctl.Sources.Ack = ack
	res, err := ctl.LandingAnswer(ctx, root, "lost:1", read)
	if err == nil || res.Outcome != "" || res.Recorded || acked != 0 {
		t.Fatalf("a lost reply reported %+v err=%v acked=%d", res, err, acked)
	}
	other, err := w.db.Pool(t, w.db.Runtime).Acquire(ctx)
	ok(t, err)
	defer other.Release()
	var records int
	ok(t, other.Conn().QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json->'landing_answer'->>'question_id' = 'q-lost-reply'`, landingRepoStream).Scan(&records))
	if records != 1 {
		t.Fatal("the commit did not land", records)
	}
	next := controller.Controller{DB: w.rt, Env: "stg"}
	next.Sources.Ack = ack
	res, err = next.LandingAnswer(ctx, root, "lost:1", read)
	ok(t, err)
	if res.Outcome != store.LandingAnswerApplied || res.Recorded || !res.Acked || acked != 1 {
		t.Fatalf("next pass: %+v acked=%d", res, acked)
	}
	if _, retries := answerRecords(t, w, "q-lost-reply", root); retries != 1 {
		t.Fatal("acted again", retries)
	}
}

// Serial: SetHook, FreezeEventClock. Two passes on one card: the second
// really waits on the first's stream lock (pg_blocking_pids), then reads its
// record: one record, one retry, the same snapshot, recorded once (review
// 657 R657-3).
func TestLandingAnswerSecondPassWaitsAndReads(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	root := failedRoot(t, w, "wait:1", "bound")
	store.FreezeEventClock(at.Add(time.Minute))
	q := answerCard("q-wait", "try_again", at.Add(time.Minute))
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool := w.db.Pool(t, w.db.Runtime)
	connA, err := pool.Acquire(bounded)
	ok(t, err)
	defer connA.Release()
	connB, err := pool.Acquire(bounded)
	ok(t, err)
	defer connB.Release()
	holding, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer store.SetHook(func(point, _ string) {
		if point == "landing answer: decided" {
			once.Do(func() { close(holding); <-release })
		}
	})()
	type out struct {
		res store.LandingAnswerResult
		err error
	}
	a, b := make(chan out, 1), make(chan out, 1)
	go func() {
		r, e := store.HandleLandingAnswer(bounded, connA.Conn(), q, root, "wait:1", at.Add(time.Minute))
		a <- out{r, e}
	}()
	select {
	case <-holding:
	case <-bounded.Done():
		t.Fatal("the first pass never decided")
	}
	go func() {
		r, e := store.HandleLandingAnswer(bounded, connB.Conn(), q, root, "wait:1", at.Add(time.Minute))
		b <- out{r, e}
	}()
	for waiting := false; !waiting; {
		ok(t, w.rt.QueryRow(ctx, `SELECT $2::integer = ANY(pg_blocking_pids($1::integer))`, connB.Conn().PgConn().PID(), connA.Conn().PgConn().PID()).Scan(&waiting))
		if bounded.Err() != nil {
			t.Fatal("the second pass never waited on the first")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	ra, rb := <-a, <-b
	ok(t, errors.Join(ra.err, rb.err))
	if !ra.res.Recorded || rb.res.Recorded || !reflect.DeepEqual(ra.res.Record, rb.res.Record) {
		t.Fatalf("first %+v second %+v", ra.res, rb.res)
	}
	if records, retries := answerRecords(t, w, "q-wait", root); records != 1 || retries != 1 {
		t.Fatalf("records=%d retries=%d", records, retries)
	}
}

// Serial: FreezeEventClock. do_not_merge from every phase an S2 drill reaches
// (review 657 R657-3): it applies (failed{declined}) wherever the protocol
// takes it and is refused (not_declinable) on a rehearsal; one record each,
// none twice.
func TestLandingAnswerDeclineSources(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	for i, phase := range []string{"claimed", "prepared", "branch_intent", "branch_pushed", "parked", "failed", "rehearsed"} {
		op := fmt.Sprintf("decline:%d", i)
		store.FreezeEventClock(at)
		root := w.ready(t, store.Task{OffPlanReason: "decline " + phase})
		ref := "refs/heads/land/stg/" + root + "/1"
		chain := [][]any{
			{"claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1},
			{"prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true},
			{"branch_intent", "ref", ref, "R", commitB},
			{"branch_pushed", "ref", ref, "R", commitB},
		}
		for _, r := range chain {
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, r[0].(string), r[1:]...)))
			if r[0] == phase {
				break
			}
		}
		switch phase {
		case "parked":
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "parked", "why", "window", "retry_at", at.Add(time.Minute).Format(time.RFC3339Nano))))
		case "failed":
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "bound")))
		case "rehearsed":
			store.FreezeEventClock(at.Add(2 * time.Second))
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_seen", "R", commitB, "run", "green", "status", "Succeeded", "created_at", at.Add(time.Second).Format(time.RFC3339Nano))))
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "rehearsed", "R", commitB)))
		}
		later := at.Add(time.Minute)
		store.FreezeEventClock(later)
		q := answerCard("q-decline-"+phase, "do_not_merge", later)
		want, why := store.LandingAnswerApplied, "declined"
		if phase == "rehearsed" {
			want, why = store.LandingAnswerRefused, "not_declinable"
		}
		for pass := 0; pass < 2; pass++ {
			res, err := store.HandleLandingAnswer(ctx, w.rt, q, root, op, later)
			ok(t, err)
			if res.Outcome != want || res.Why != why || res.Recorded == (pass == 1) {
				t.Fatalf("%s pass %d: %+v", phase, pass, res)
			}
		}
		if o := landingState(t, w, landingRepoStream).Operations[op]; (want == store.LandingAnswerApplied) != (o.Phase == "failed" && o.Outcome == "declined") {
			t.Fatalf("%s: %+v", phase, o)
		}
	}
}
