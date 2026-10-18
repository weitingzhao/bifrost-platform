package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var owner = store.Writer{Actor: "owner", Token: "admin"}

// mailWorld is a kernel world where the Owner wrote to the chief of staff;
// it returns the message.
func mailWorld(t *testing.T, body string) (*world, string) {
	t.Helper()
	w := newKernelWorld(t)
	id, err := store.SendMessage(ctx, w.rt, owner, store.Seat, "ask", []byte(body))
	ok(t, err)
	return w, id
}

// takeAndReady takes the message into a brief and readies the brief.
func takeAndReady(t *testing.T, w *world, message string) (task, mission string) {
	t.Helper()
	task, err := store.TakeMail(ctx, w.rt, message)
	ok(t, err)
	if task == "" {
		t.Fatal("the message was not taken")
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&mission))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") }))
	if _, err = store.Dispatch(ctx, w.rt); err != nil || w.state(t, "task", task) != "ready" {
		t.Fatalf("dispatch: %v; the brief is %s", err, w.state(t, "task", task))
	}
	return task, mission
}

func thread(t *testing.T, w *world) []store.Message {
	t.Helper()
	ms, err := store.Thread(ctx, w.rt, "owner", store.Seat, 50)
	ok(t, err)
	return ms
}

// A message to the seat becomes a mission with one brief, routed to the chief
// of staff; its report goes back to the Owner as the answer, the brief is
// accepted and the mission closes.
func TestMailToTheChief(t *testing.T) {
	w, ask := mailWorld(t, "What is running now?\nAnd what waits on me?")
	if _, err := store.SendMessage(ctx, w.rt, owner, store.Seat, "answer", []byte("an answer is not a question")); err != nil {
		t.Fatal(err)
	}
	mail, err := store.PendingMail(ctx, w.rt)
	if err != nil || len(mail) != 1 || mail[0] != ask {
		t.Fatalf("pending mail %v %v", mail, err)
	}
	task, mission := takeAndReady(t, w, ask)
	again, err := store.TakeMail(ctx, w.rt, ask)
	if err != nil || again != "" {
		t.Fatalf("taken twice: %q %v", again, err)
	}
	if mail, _ = store.PendingMail(ctx, w.rt); len(mail) != 0 {
		t.Fatalf("still pending: %v", mail)
	}
	var title, role, effect, class, job string
	ok(t, w.rt.QueryRow(ctx, `SELECT m.title, t.role, t.effect_class, t.task_class FROM agentrt.task t JOIN agentrt.mission m USING (mission_id)
	  WHERE t.task_id = $1`, task).Scan(&title, &role, &effect, &class))
	if title != "Chief: What is running now?" || role != "brief" || effect != "none" || class != "brief" {
		t.Fatalf("mission %q, task %s %s %s", title, role, effect, class)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'job' FROM agentrt.event WHERE task_id = $1 AND event_type = 'route_decided'`, task).Scan(&job))
	if job != "chief_of_staff" {
		t.Fatalf("the brief is routed to %s", job)
	}
	var spec string
	ok(t, w.rt.QueryRow(ctx, `SELECT convert_from(b.body, 'UTF8') FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.task_id = $1`, task).Scan(&spec))
	if !strings.Contains(spec, "And what waits on me?") || !strings.Contains(spec, "None: this is the first message.") {
		t.Fatalf("the spec does not hold the message: %.300s", spec)
	}

	w.work(t, task, "Nothing runs now; one card waits on you.", "done")
	if briefs, err := store.PendingBriefs(ctx, w.rt); err != nil || len(briefs) != 1 {
		t.Fatalf("pending briefs %v %v", briefs, err)
	}
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "answered" {
		t.Fatalf("accept: %q %v", did, err)
	}
	if s := w.state(t, "task", task); s != "accepted" {
		t.Fatalf("the brief is %s", s)
	}
	ms := thread(t, w)
	if len(ms) != 3 || ms[0].ID != ask || ms[0].AckedAt == nil || ms[0].TaskID != task {
		t.Fatalf("thread %+v", ms)
	}
	a := ms[2]
	if a.From != store.Seat || a.To != "owner" || a.Kind != "answer" || a.TaskID != task || a.MissionID != mission ||
		!strings.HasPrefix(a.Body, "Nothing runs now; one card waits on you.") || a.AckedAt != nil {
		t.Fatalf("answer %+v", a)
	}
	if briefs, _ := store.PendingBriefs(ctx, w.rt); len(briefs) != 0 {
		t.Fatalf("still pending: %v", briefs)
	}
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || len(closed) != 1 || closed[0] != mission {
		t.Fatalf("closed %v %v", closed, err)
	}

	// The next message reads the conversation so far.
	next, err := store.SendMessage(ctx, w.rt, owner, store.Seat, "ask", []byte("Thanks. Anything else?"))
	ok(t, err)
	task2, _ := takeAndReady(t, w, next)
	ok(t, w.rt.QueryRow(ctx, `SELECT convert_from(b.body, 'UTF8') FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.task_id = $1`, task2).Scan(&spec))
	if !strings.Contains(spec, "owner (ask): What is running now?") || !strings.Contains(spec, "seat:chief_of_staff (answer): Nothing runs now") {
		t.Fatalf("the second spec does not hold the conversation: %.600s", spec)
	}
}

// A brief whose attempt does not end done is blocked and the Owner is told;
// one blocked before it ran is told too, once; readied again and answered,
// it sends the answer after all.
func TestABriefWithoutAnAnswerSaysSo(t *testing.T) {
	w, ask := mailWorld(t, "Status?")
	task, _ := takeAndReady(t, w, ask)
	w.work(t, task, "it broke", "failed")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "blocked" {
		t.Fatalf("accept: %q %v", did, err)
	}
	ms := thread(t, w)
	if len(ms) != 2 || !strings.Contains(ms[1].Body, "could not answer") || ms[1].TaskID != task {
		t.Fatalf("thread %+v", ms)
	}
	if briefs, _ := store.PendingBriefs(ctx, w.rt); len(briefs) != 0 {
		t.Fatalf("told twice: %v", briefs)
	}

	w2, ask2 := mailWorld(t, "Status again?")
	task2, _ := takeAndReady(t, w2, ask2)
	ok(t, store.InTx(ctx, w2.rt, func(tx pgx.Tx) error {
		return store.MoveTask(ctx, tx, "controller", task2, "blocked", "test: held")
	}))
	if did, err := store.AcceptBrief(ctx, w2.rt, task2); err != nil || did != "told" {
		t.Fatalf("a blocked brief: %q %v", did, err)
	}
	if did, err := store.AcceptBrief(ctx, w2.rt, task2); err != nil || did != "" {
		t.Fatalf("told twice: %q %v", did, err)
	}
	ok(t, store.InTx(ctx, w2.rt, func(tx pgx.Tx) error {
		return store.MoveTask(ctx, tx, "controller", task2, "ready", "test: again")
	}))
	w2.work(t, task2, "All quiet.", "done")
	if did, err := store.AcceptBrief(ctx, w2.rt, task2); err != nil || did != "answered" {
		t.Fatalf("answered after all: %q %v", did, err)
	}
	if ms := thread(t, w2); len(ms) != 3 || !strings.HasPrefix(ms[2].Body, "All quiet.") {
		t.Fatalf("thread %+v", ms)
	}
}

// A long answer is cut and says so; the thread is bounded and refuses a bad
// counterpart.
func TestBriefAnswerAndThreadBounds(t *testing.T) {
	w, ask := mailWorld(t, "Tell me everything.")
	task, _ := takeAndReady(t, w, ask)
	w.work(t, task, strings.Repeat("长", 7000), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, task); err != nil || did != "answered" {
		t.Fatalf("accept: %q %v", did, err)
	}
	ms := thread(t, w)
	a := ms[len(ms)-1].Body
	if len(a) > store.MaxMessageBody || !strings.HasSuffix(a, "(The answer was longer; this is its beginning.)") {
		t.Fatalf("an answer of %d bytes ending %q", len(a), a[len(a)-60:])
	}
	if last, err := store.Thread(ctx, w.rt, "owner", store.Seat, 1); err != nil || len(last) != 1 || last[0].From != store.Seat {
		t.Fatalf("the newest one: %+v %v", last, err)
	}
	for _, with := range []string{"", "owner", "nobody"} {
		if _, err := store.Thread(ctx, w.rt, "owner", with, 10); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("thread with %q: %v", with, err)
		}
	}
}
