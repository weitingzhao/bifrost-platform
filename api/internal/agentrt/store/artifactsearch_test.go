package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func search(t *testing.T, w *world, q store.ArtifactQuery) ([]string, string) {
	t.Helper()
	list, next, err := store.SearchArtifacts(ctx, w.rt, q)
	ok(t, err)
	ids := make([]string, 0, len(list))
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	return ids, next
}

// allArtifacts are every artifact's id, newest first, as the table has them.
func allArtifacts(t *testing.T, w *world, where string) []string {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT artifact_id FROM agentrt.artifact WHERE `+where+` ORDER BY created_at DESC, artifact_id DESC`)
	ok(t, err)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	return ids
}

// W-31 2.3 search_artifacts (the Owner 2026-10-21 「四条都照推荐 A」): the
// metadata of the artifacts, newest first, narrowed by kind, author, time, a
// task's or a mission's records; pages chained by their cursor list each
// artifact once; an Agent host is shown no exam artifact.
func TestSearchArtifacts(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	thread := wr("thread:s1")
	handover, _, err := store.UploadArtifact(ctx, w.rt, thread, "handover", "text/markdown", []byte("# handover\nwhat I judged"))
	ok(t, err)
	impl := w.ready(t, store.Task{Repo: "bifrost-platform"})
	ic := w.run(t, "h1", cursor, "done")
	if ic.TaskID != impl {
		t.Fatalf("ran %s, want %s", ic.TaskID, impl)
	}
	var report string
	ok(t, w.rt.QueryRow(ctx, `SELECT report_artifact_id FROM agentrt.attempt WHERE attempt_id = $1`, ic.AttemptID).Scan(&report))
	check, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	var checkSpec string
	ok(t, w.rt.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE task_id = $1`, check).Scan(&checkSpec))
	// The check runs and leaves a checkpoint: an artifact only an event names.
	cc := w.claim(t, "h2", time.Hour, deterministic)
	if cc == nil || cc.TaskID != check {
		t.Fatalf("the check's claim %+v", cc)
	}
	ok(t, store.Start(ctx, w.rt, fence(cc, "h2"), "", ""))
	logTail := w.artifact(t, "log_tail", "the check's log so far")
	_, err = store.Checkpoint(ctx, w.rt, fence(cc, "h2"), 1, []string{logTail}, false)
	ok(t, err)
	// It ends, and its verdict on the implementation is a verification with
	// its evidence: the implementation's record.
	exit := 0
	_, err = store.Computed(ctx, w.rt, fence(cc, "h2"), &exit, "exit", "done")
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, fence(cc, "h2"), store.Result{Report: w.artifact(t, "verify", `{"pass": true}`)},
		heads(map[string]string{}, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, fence(cc, "h2"))
	ok(t, err)
	_, err = store.Conclude(ctx, w.rt, check)
	ok(t, err)
	var evidence string
	ok(t, w.rt.QueryRow(ctx, `SELECT evidence_artifact_id FROM agentrt.verification WHERE task_id = $1`, impl).Scan(&evidence))
	// A task of M-2 with a spec of its own.
	otherSpec := w.artifact(t, "task_spec", "# M-2's task\n")
	other := store.NewTaskID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, perr := store.ProposeTask(ctx, tx, "controller", store.Task{ID: other, MissionID: "M-2", OffPlanReason: "test", Role: "implement",
			Class: "go", Title: "other", SpecArtifact: otherSpec, Effect: "branch", Repo: "bifrost-platform"})
		return perr
	}))
	// A message to the chief of staff becomes a brief: its body is the
	// brief task's and the brief mission's; a mission's spec is the mission's.
	ask, err := store.SendMessage(ctx, w.rt, owner, store.Seat, "ask", []byte("Status?"))
	ok(t, err)
	brief, briefMission := takeAndReady(t, w, ask)
	var askBody string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_artifact_id FROM agentrt.message WHERE message_id = $1`, ask).Scan(&askBody))
	missionSpec := w.artifact(t, "mission_spec", "# M-2\n")
	w.legacy(t, `UPDATE agentrt.mission SET spec_artifact_id = '`+missionSpec+`' WHERE mission_id = 'M-2'`)
	exam := w.artifact(t, "exam", "an answer")
	paper := w.artifact(t, "other", "a paper put as other first")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, aerr := store.Append(ctx, tx, store.Event{Stream: store.ExamsStream, Type: "artifact_put", Actor: "controller",
			Body: map[string]any{"artifact_id": paper, "artifact_kind": "other", "size_bytes": 26, "uploaded_by": "controller"}})
		return aerr
	}))

	all := allArtifacts(t, w, "true")
	if got, next := search(t, w, store.ArtifactQuery{Limit: 100}); !slices.Equal(got, all) || next != "" {
		t.Fatalf("everything: %v (next %q), want %v", got, next, all)
	}
	// Pages of two, chained, list each artifact once and in order.
	var paged []string
	before := ""
	for i := 0; ; i++ {
		got, next := search(t, w, store.ArtifactQuery{Limit: 2, Before: before})
		paged = append(paged, got...)
		if next == "" {
			break
		}
		if len(got) != 2 || i > len(all) {
			t.Fatalf("page %d: %v, next %q", i, got, next)
		}
		before = next
	}
	if !slices.Equal(paged, all) {
		t.Fatalf("paged %v, want %v", paged, all)
	}

	if got, _ := search(t, w, store.ArtifactQuery{Kinds: []string{"handover"}}); !slices.Equal(got, []string{handover}) {
		t.Fatalf("kind handover: %v", got)
	}
	if got, _ := search(t, w, store.ArtifactQuery{Author: "thread:s1"}); !slices.Equal(got, []string{handover}) {
		t.Fatalf("author thread:s1: %v", got)
	}
	got, _ := search(t, w, store.ArtifactQuery{Task: impl})
	for _, want := range []string{w.spec, report, evidence} {
		if !slices.Contains(got, want) {
			t.Fatalf("task %s: %v has no %s", impl, got, want)
		}
	}
	for _, not := range []string{otherSpec, handover, checkSpec, logTail, exam} {
		if slices.Contains(got, not) {
			t.Fatalf("task %s: %v has %s", impl, got, not)
		}
	}
	if got, _ = search(t, w, store.ArtifactQuery{Task: check}); !slices.Contains(got, checkSpec) || !slices.Contains(got, logTail) ||
		slices.Contains(got, report) {
		t.Fatalf("the check's: %v", got)
	}
	got, _ = search(t, w, store.ArtifactQuery{Mission: "M-1"})
	for _, want := range []string{w.spec, report, checkSpec, logTail} {
		if !slices.Contains(got, want) {
			t.Fatalf("mission M-1: %v has no %s", got, want)
		}
	}
	if slices.Contains(got, otherSpec) {
		t.Fatalf("mission M-1 lists M-2's spec: %v", got)
	}
	if got, _ = search(t, w, store.ArtifactQuery{Mission: "M-2"}); !slices.Contains(got, otherSpec) || !slices.Contains(got, missionSpec) || len(got) != 2 {
		t.Fatalf("mission M-2: %v", got)
	}
	if got, _ = search(t, w, store.ArtifactQuery{Task: other}); !slices.Equal(got, []string{otherSpec}) {
		t.Fatalf("M-2's task: %v (a mission's spec is not its task's)", got)
	}
	for _, q := range []store.ArtifactQuery{{Task: brief}, {Mission: briefMission}} {
		if got, _ = search(t, w, q); !slices.Contains(got, askBody) {
			t.Fatalf("%+v: %v has no message body %s", q, got, askBody)
		}
	}
	if got, _ = search(t, w, store.ArtifactQuery{Mission: "M-2", Task: impl}); len(got) != 0 {
		t.Fatalf("a task of M-1 within M-2: %v", got)
	}
	if got, _ = search(t, w, store.ArtifactQuery{Task: store.NewTaskID()}); len(got) != 0 {
		t.Fatalf("a task that is not: %v", got)
	}
	// Since: an artifact stored now is listed, the earlier ones are not.
	time.Sleep(10 * time.Millisecond)
	cut := time.Now()
	time.Sleep(10 * time.Millisecond)
	late := w.artifact(t, "other", "stored after the cut")
	if got, _ = search(t, w, store.ArtifactQuery{Since: cut}); !slices.Equal(got, []string{late}) {
		t.Fatalf("since: %v, want %s", got, late)
	}
	// An Agent host is shown no exam artifact: of kind exam or named on the
	// exams' stream.
	got, _ = search(t, w, store.ArtifactQuery{HideExams: true, Limit: 100})
	if slices.Contains(got, exam) || slices.Contains(got, paper) || len(got) != len(all)+1-2 {
		t.Fatalf("hidden from a host: %v", got)
	}
	if got, _ = search(t, w, store.ArtifactQuery{Limit: 100}); !slices.Contains(got, exam) || !slices.Contains(got, paper) {
		t.Fatalf("the exams are listed to others: %v", got)
	}
}

// A search that cannot be answered as asked is refused as the request's
// error, never answered with something else.
func TestSearchArtifactsRefusals(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for name, q := range map[string]store.ArtifactQuery{
		"kind":     {Kinds: []string{"report", "patch"}},
		"task":     {Task: "t1"},
		"mission":  {Mission: "M-0"},
		"author":   {Author: "someone"},
		"limit":    {Limit: store.ArtifactSearchLimit + 1},
		"negative": {Limit: -1},
		"before":   {Before: "2026-10-21T00:00:00Z"},
		"digest":   {Before: "1792600000000000.ABC"},
	} {
		if _, _, err := store.SearchArtifacts(ctx, w.rt, q); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}
