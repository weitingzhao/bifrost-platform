package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// platformNotes are the notes to the Owner in his conversation with the seat
// that the platform wrote of a waiting brief.
func platformNotes(t *testing.T, w *world) []store.Message {
	t.Helper()
	var out []store.Message
	for _, m := range thread(t, w) {
		if m.From == store.Seat && m.To == "owner" && strings.HasPrefix(m.Body, "From the platform, not the chief of staff") {
			out = append(out, m)
		}
	}
	return out
}

func tellAt(t *testing.T, w *world, now time.Time) int {
	t.Helper()
	n, err := store.TellWaitingBriefs(ctx, w.rt, now)
	ok(t, err)
	return n
}

// coolRoute cools down, for an hour, the Owner's account of each tool the
// task's route names, as his reading of a spent window does.
func coolRoute(t *testing.T, w *world, task string) {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT DISTINCT replace(c.executor_actor_id, 'exec:', '')
	  FROM agentrt.task t, jsonb_array_elements_text(t.route_json) r JOIN agentrt.job_candidate c ON c.job_candidate_id = r
	  WHERE t.task_id = $1`, task)
	ok(t, err)
	tools, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if len(tools) == 0 {
		t.Fatal("the brief's route names no candidate")
	}
	spent, resets := 1.0, time.Now().Add(time.Hour)
	for _, tool := range tools {
		_, err = store.RecordOwnerReading(ctx, w.rt, store.Writer{Actor: "owner"}, "spent-"+tool,
			store.PoolReading{Pool: tool + ":owner:weekly", UsedFraction: &spent, ResetsAt: &resets})
		ok(t, err)
	}
}

// Review 487 A1: a brief no host takes (here every candidate's account cools
// down) waits ready, and the seat that would tell the Owner is what does not
// run; once its wait passes its limit the platform tells him, once, in his
// conversation with the seat: what waits, what it answers and why. The note
// is not the brief's answer: the brief stays ready, and a brief that ends
// unanswered still tells its sender so.
func TestABriefNoHostTakesIsToldToTheOwner(t *testing.T) {
	t.Parallel()
	w, ask := mailWorld(t, "Where do we stand?")
	task, mission := takeAndReady(t, w, ask)
	coolRoute(t, w, task)
	now := time.Now()
	waitsAt(t, w, now.Add(10*time.Minute))
	if n := tellAt(t, w, now.Add(10*time.Minute)); n != 0 || len(platformNotes(t, w)) != 0 {
		t.Fatalf("told before its limit: %d", n)
	}
	if n := waitsAt(t, w, now.Add(31*time.Minute)); n != 1 {
		t.Fatalf("%d waits recorded", n)
	}
	if n := tellAt(t, w, now.Add(31*time.Minute)); n != 1 {
		t.Fatalf("told %d", n)
	}
	notes := platformNotes(t, w)
	if len(notes) != 1 || notes[0].Kind != "note" || notes[0].TaskID != "" || notes[0].MissionID != "" {
		t.Fatalf("notes %+v", notes)
	}
	for _, want := range []string{"brief task " + task, "mission " + mission, "owner's message " + ask, "cool down until", "stays ready"} {
		if !strings.Contains(notes[0].Body, want) {
			t.Fatalf("the note lacks %q:\n%s", want, notes[0].Body)
		}
	}
	if n := tellAt(t, w, now.Add(3*time.Hour)); n != 0 || len(platformNotes(t, w)) != 1 {
		t.Fatalf("told again: %d", n)
	}
	if w.state(t, "task", task) != "ready" {
		t.Fatalf("the brief is %s", w.state(t, "task", task))
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveTask(ctx, tx, "controller", task, "blocked", "test: held")
	}))
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "told" {
		t.Fatalf("a brief that ends unanswered after the note: %q %v", did, err)
	}
}

// Review 487 A1, 490 A2: with nothing found that stops the candidates, the
// note says what was looked at and that why no host took it is not known
// here; it does not guess that no host asked. A brief told is answered as
// usual once a host takes it. A brief taken before the pass is not told.
func TestAWaitingBriefSaysWhatThePlatformDoesNotKnow(t *testing.T) {
	t.Parallel()
	w, ask := mailWorld(t, "Anything for me?")
	task, _ := takeAndReady(t, w, ask)
	now := time.Now()
	waitsAt(t, w, now.Add(31*time.Minute))
	if n := tellAt(t, w, now.Add(31*time.Minute)); n != 1 {
		t.Fatalf("told %d", n)
	}
	notes := platformNotes(t, w)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "finds no cooldown and no quota stop") ||
		!strings.Contains(notes[0].Body, "is not known here") || strings.Contains(notes[0].Body, "so no host") {
		t.Fatalf("notes %+v", notes)
	}
	w.work(t, task, "All quiet.", "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "answered" {
		t.Fatalf("a brief told of, then taken: %q %v", did, err)
	}
	if ms := thread(t, w); !strings.Contains(ms[len(ms)-1].Body, "All quiet.") || ms[len(ms)-1].TaskID != task {
		t.Fatalf("the answer: %+v", ms[len(ms)-1])
	}

	w2, ask2 := mailWorld(t, "Anything else?")
	task2, _ := takeAndReady(t, w2, ask2)
	waitsAt(t, w2, now.Add(31*time.Minute))
	if c := w2.claim(t, "h1", time.Hour, claude); c == nil || c.TaskID != task2 {
		t.Fatalf("the brief was not taken: %+v", c)
	}
	if n := tellAt(t, w2, now.Add(31*time.Minute)); n != 0 || len(platformNotes(t, w2)) != 0 {
		t.Fatalf("a brief taken meanwhile was told: %d", n)
	}
}

// Review 490 A2: a drain in force for every host keeps work from every claim,
// and the note names it rather than guessing at the hosts.
func TestAWaitingBriefNamesADrain(t *testing.T) {
	t.Parallel()
	w, ask := mailWorld(t, "Status?")
	takeAndReady(t, w, ask)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", "c1", "global", "", "drain", "test") }))
	now := time.Now()
	waitsAt(t, w, now.Add(31*time.Minute))
	if n := tellAt(t, w, now.Add(31*time.Minute)); n != 1 {
		t.Fatalf("told %d", n)
	}
	if notes := platformNotes(t, w); len(notes) != 1 || !strings.Contains(notes[0].Body, "A drain is in force for every host") {
		t.Fatalf("notes %+v", notes)
	}
}

// Review 490 A1: a wait recorded but not told (the telling failed, or the
// controller stopped between the two) is told on a later pass however long
// ago it was recorded, once.
func TestAWaitNotToldIsToldADayLater(t *testing.T) {
	t.Parallel()
	w, ask := mailWorld(t, "Still there?")
	task, _ := takeAndReady(t, w, ask)
	now := time.Now()
	if n := waitsAt(t, w, now.Add(31*time.Minute)); n != 1 {
		t.Fatalf("%d waits recorded", n)
	}
	for _, later := range []time.Duration{25 * time.Hour, 26 * time.Hour} {
		tellAt(t, w, now.Add(later))
	}
	if notes := platformNotes(t, w); len(notes) != 1 || !strings.Contains(notes[0].Body, task) {
		t.Fatalf("notes %+v", notes)
	}
}

// Review 490 B1: a pass that waits for the seat's lock (another transaction
// holds it) gives up within its bounds and says so, so the controller's loop
// goes on; once the lock is free a later pass tells the Owner.
func TestATellingWaitsBoundedForTheSeatsLock(t *testing.T) {
	t.Parallel()
	w, ask := mailWorld(t, "Are you there?")
	takeAndReady(t, w, ask)
	now := time.Now()
	waitsAt(t, w, now.Add(31*time.Minute))
	// The lock is held from another session: an advisory lock is its own
	// session's again.
	holder, err := w.db.Pool(t, w.db.Runtime).Begin(ctx)
	ok(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock(742107, hashtext($1))`, store.Seat)
	ok(t, err)
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	start := time.Now()
	if _, err = store.TellWaitingBriefs(bounded, w.rt, now.Add(31*time.Minute)); err == nil {
		t.Fatal("a pass told while the seat's lock was held")
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("the pass waited %s for the seat's lock", took)
	}
	ok(t, holder.Rollback(ctx))
	if n := tellAt(t, w, now.Add(32*time.Minute)); n != 1 || len(platformNotes(t, w)) != 1 {
		t.Fatalf("told %d once the lock was free", n)
	}
}
