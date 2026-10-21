package store_test

import (
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

// Review 487 A1: with nothing in the platform stopping the candidates, the
// note says no host offering their executor and model asked; a brief taken
// before the pass is not told.
func TestAWaitingBriefNamesWhatNoHostOffered(t *testing.T) {
	t.Parallel()
	w, ask := mailWorld(t, "Anything for me?")
	task, _ := takeAndReady(t, w, ask)
	now := time.Now()
	waitsAt(t, w, now.Add(31*time.Minute))
	if n := tellAt(t, w, now.Add(31*time.Minute)); n != 1 {
		t.Fatalf("told %d", n)
	}
	notes := platformNotes(t, w)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "nothing in the platform stops it") ||
		!strings.Contains(notes[0].Body, "asked for work") {
		t.Fatalf("notes %+v", notes)
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

	_ = task
}
