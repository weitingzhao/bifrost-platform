package artifact_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func search(t *testing.T, w *world, q artifact.ArtifactQuery) ([]string, string) {
	t.Helper()
	list, next, err := artifact.SearchArtifacts(ctx, w.rt, q)
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
	w := newWorld(t)
	thread := wr("thread:s1")
	handover, _, err := artifact.UploadArtifact(ctx, w.rt, thread, "handover", "text/markdown", []byte("# handover\nwhat I judged"))
	ok(t, err)
	// No task has rows any more (0020): a task id finds nothing of its own.
	impl := "t_01M5192P0JPMJAP63ZKRPXJ2VV"
	report := w.artifact(t, "report", "a report")
	// A mission's spec is the mission's.
	missionSpec := w.artifact(t, "mission_spec", "# M-2\n")
	w.legacy(t, `UPDATE agentrt.mission SET spec_artifact_id = '`+missionSpec+`' WHERE mission_id = 'M-2'`)
	exam := w.artifact(t, "exam", "an answer")
	paper := w.artifact(t, "other", "a paper put as other first")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, aerr := store.Append(ctx, tx, store.Event{Stream: artifact.ExamsStream, Type: "artifact_put", Actor: "controller",
			Body: map[string]any{"artifact_id": paper, "artifact_kind": "other", "size_bytes": 26, "uploaded_by": "controller"}})
		return aerr
	}))

	all := allArtifacts(t, w, "true")
	if got, next := search(t, w, artifact.ArtifactQuery{Limit: 100}); !slices.Equal(got, all) || next != "" {
		t.Fatalf("everything: %v (next %q), want %v", got, next, all)
	}
	// Pages of two, chained, list each artifact once and in order.
	var paged []string
	before := ""
	for i := 0; ; i++ {
		got, next := search(t, w, artifact.ArtifactQuery{Limit: 2, Before: before})
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

	if got, _ := search(t, w, artifact.ArtifactQuery{Kinds: []string{"handover"}}); !slices.Equal(got, []string{handover}) {
		t.Fatalf("kind handover: %v", got)
	}
	if got, _ := search(t, w, artifact.ArtifactQuery{Author: "thread:s1"}); !slices.Equal(got, []string{handover}) {
		t.Fatalf("author thread:s1: %v", got)
	}
	if got, _ := search(t, w, artifact.ArtifactQuery{Task: impl}); len(got) != 0 {
		t.Fatalf("task %s: %v", impl, got)
	}
	got, _ := search(t, w, artifact.ArtifactQuery{Mission: "M-1"})
	if slices.Contains(got, report) || slices.Contains(got, handover) {
		t.Fatalf("mission M-1 lists artifacts no record of it names: %v", got)
	}
	// A mission's own spec is still its record; its tasks' specs are not read.
	if got, _ = search(t, w, artifact.ArtifactQuery{Mission: "M-2"}); !slices.Equal(got, []string{missionSpec}) {
		t.Fatalf("mission M-2: %v", got)
	}
	if got, _ = search(t, w, artifact.ArtifactQuery{Mission: "M-2", Task: impl}); len(got) != 0 {
		t.Fatalf("a task of M-1 within M-2: %v", got)
	}
	if got, _ = search(t, w, artifact.ArtifactQuery{Task: store.NewTaskID()}); len(got) != 0 {
		t.Fatalf("a task that is not: %v", got)
	}
	// Since: an artifact stored now is listed, the earlier ones are not.
	time.Sleep(10 * time.Millisecond)
	cut := time.Now()
	time.Sleep(10 * time.Millisecond)
	late := w.artifact(t, "other", "stored after the cut")
	if got, _ = search(t, w, artifact.ArtifactQuery{Since: cut}); !slices.Equal(got, []string{late}) {
		t.Fatalf("since: %v, want %s", got, late)
	}
	// An Agent host is shown no exam artifact: of kind exam or named on the
	// exams' stream.
	got, _ = search(t, w, artifact.ArtifactQuery{HideExams: true, Limit: 100})
	if slices.Contains(got, exam) || slices.Contains(got, paper) || len(got) != len(all)+1-2 {
		t.Fatalf("hidden from a host: %v", got)
	}
	if got, _ = search(t, w, artifact.ArtifactQuery{Limit: 100}); !slices.Contains(got, exam) || !slices.Contains(got, paper) {
		t.Fatalf("the exams are listed to others: %v", got)
	}
}

// A search that cannot be answered as asked is refused as the request's
// error, never answered with something else.
func TestSearchArtifactsRefusals(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for name, q := range map[string]artifact.ArtifactQuery{
		"kind":     {Kinds: []string{"report", "patch"}},
		"task":     {Task: "t1"},
		"mission":  {Mission: "M-0"},
		"author":   {Author: "someone"},
		"nul":      {Author: "thread:a\x00b"},
		"utf8":     {Author: "thread:\xff"},
		"limit":    {Limit: artifact.ArtifactSearchLimit + 1},
		"negative": {Limit: -1},
		"before":   {Before: "2026-10-21T00:00:00Z"},
		"digest":   {Before: "1792600000000000.ABC"},
	} {
		if _, _, err := artifact.SearchArtifacts(ctx, w.rt, q); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

// Review 478: since to the nanosecond against times stored to the
// microsecond; two artifacts stored in the same microsecond, paged one at a
// time, in their digests' order; exam artifacts never on any page of a host's.
func TestSearchArtifactsEdges(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	// One transaction: created_at is its start, the same microsecond for all three.
	var a, b, exam string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		for _, x := range []struct {
			id         *string
			kind, body string
		}{{&a, "report", "the first of a microsecond"}, {&b, "report", "the second of it"}, {&exam, "exam", "an answer of the same time"}} {
			if *x.id, err = artifact.PutArtifact(ctx, tx, "controller", x.kind, "text/plain", []byte(x.body)); err != nil {
				return err
			}
		}
		return nil
	}))
	var at time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT created_at FROM agentrt.artifact WHERE artifact_id = $1`, a).Scan(&at))
	var same int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact WHERE created_at = $1`, at).Scan(&same))
	if same != 3 {
		t.Fatalf("%d artifacts at %s, want the three", same, at.Format(time.RFC3339Nano))
	}
	for _, c := range []struct {
		since time.Time
		in    bool
	}{
		{at.Add(-time.Nanosecond), true}, {at, true}, {at.Add(time.Nanosecond), false}, {at.Add(time.Microsecond), false},
	} {
		got, _ := search(t, w, artifact.ArtifactQuery{Kinds: []string{"report"}, Since: c.since})
		if in := slices.Contains(got, a) && slices.Contains(got, b); in != c.in || (!c.in && len(got) != 0) {
			t.Errorf("since %s: %v, want listed %v", c.since.Format(time.RFC3339Nano), got, c.in)
		}
	}
	// The same microsecond: by digest, descending, one a page.
	want := []string{a, b}
	slices.Sort(want)
	slices.Reverse(want)
	first, next := search(t, w, artifact.ArtifactQuery{Kinds: []string{"report"}, Since: at, Limit: 1})
	second, last := search(t, w, artifact.ArtifactQuery{Kinds: []string{"report"}, Since: at, Limit: 1, Before: next})
	if !slices.Equal(append(first, second...), want) || next == "" || last != "" {
		t.Fatalf("one a page: %v then %v (next %q, last %q), want %v", first, second, next, last, want)
	}
	// A host's pages, one at a time over everything, list every artifact but
	// the exam, in order, each page one, and end (review 481 B1: a page
	// emptied by hiding after the limit, or a paging that never ends, fails).
	var visible []string
	for _, id := range allArtifacts(t, w, "true") {
		if id != exam {
			visible = append(visible, id)
		}
	}
	var paged []string
	before := ""
	for i := 0; ; i++ {
		if i > len(visible) {
			t.Fatalf("a host's paging did not end after %d pages: %v", i, paged)
		}
		got, n := search(t, w, artifact.ArtifactQuery{HideExams: true, Limit: 1, Before: before})
		if len(got) != 1 {
			t.Fatalf("page %d of a host's: %v (next %q), want one", i, got, n)
		}
		paged = append(paged, got...)
		if n == "" {
			break
		}
		before = n
	}
	if !slices.Equal(paged, visible) || !slices.Contains(paged, a) || !slices.Contains(paged, b) {
		t.Fatalf("a host's pages %v, want %v", paged, visible)
	}
}
