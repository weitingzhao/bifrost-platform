package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const voidApproval = "appr_0123456789abcdef"

// TD-407 (the Owner 2026-10-20): a paper no run was handed in for is voided
// by an approval; it is settled at once, so its executor is handed a new
// paper, and it is never graded, takes no answer and records no score. The
// same approval again is answered with what it did; another is refused.
func TestExamVoidUnansweredPaper(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	p, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
	ok(t, err)
	if _, err = store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set); !errors.Is(err, store.ErrState) {
		t.Fatalf("a second paper while the first is open: %v", err)
	}
	out, err := store.VoidExamPaper(ctx, w.rt, p.RunID, "agentd refused it before any run (short commits)", voidApproval)
	ok(t, err)
	if out["voided_by"] != voidApproval || out["run_id"] != p.RunID || out["duplicate"] != nil {
		t.Fatalf("void: %v", out)
	}
	var cause int64
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT v.body_json, v.cause_event_id FROM agentrt.event v WHERE v.event_stream_id = $1 AND v.event_type = 'note'`,
		store.ExamsStream).Scan(&body, &cause))
	var issuedBy int64
	ok(t, w.rt.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'artifact_put'
	  AND body_json->>'artifact_id' = $2`, store.ExamsStream, "sha256:"+p.RunID).Scan(&issuedBy))
	if body["voided"] != voidApproval || cause != issuedBy || body["text"] != "paper voided: agentd refused it before any run (short commits)" {
		t.Fatalf("the void's note: %v caused by %d, the paper's issue is %d", body, cause, issuedBy)
	}
	next, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
	ok(t, err)
	if next.RunID == p.RunID {
		t.Fatal("the voided paper was handed out again")
	}
	if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS")), set); !errors.Is(err, store.ErrState) {
		t.Fatalf("an answer to the voided paper: %v", err)
	}
	again, err := store.VoidExamPaper(ctx, w.rt, p.RunID, "retried", voidApproval)
	ok(t, err)
	if again["duplicate"] != true || again["event_id"] != out["event_id"] {
		t.Fatalf("the same approval again: %v", again)
	}
	if _, err = store.VoidExamPaper(ctx, w.rt, p.RunID, "another", "appr_fedcba9876543210"); !errors.Is(err, store.ErrState) {
		t.Fatalf("another approval for a voided paper: %v", err)
	}
	// Past both lives nothing grades it: no score, no failure.
	_, err = store.IssueExamLater(ctx, w.rt, examAsk("h2", "exec:codex"), set, 3*store.ExamPaperLife)
	ok(t, err)
	if n := w.examEvents(t, "capability_scored"); n != 0 {
		t.Fatalf("%d scores after a void", n)
	}
	if n := w.examEvents(t, "note"); n != 1 {
		t.Fatalf("%d notes", n)
	}
}

// A paper with any answer, a graded one and a lapsed one are not voided, and
// nothing is written; nor is a run no paper names, or a void without an
// approval or a reason.
func TestExamVoidRefusals(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	answered, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
	ok(t, err)
	_, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(answered, ans("F2", 3, "PASS")), set)
	ok(t, err)
	if _, err = store.VoidExamPaper(ctx, w.rt, answered.RunID, "one answer", voidApproval); !errors.Is(err, store.ErrState) {
		t.Fatalf("a paper with an answer: %v", err)
	}
	graded, err := store.IssueExam(ctx, w.rt, examAsk("h2", "exec:codex"), set)
	ok(t, err)
	var all []store.ExamAnswer
	for _, it := range graded.Items {
		for _, n := range it.Runs {
			all = append(all, ans(it.Item, n, "PASS"))
		}
	}
	_, err = store.RecordExamAnswers(ctx, w.rt, "h2", "agentd:h2", part(graded, all...), set)
	ok(t, err)
	if _, err = store.VoidExamPaper(ctx, w.rt, graded.RunID, "graded", voidApproval); !errors.Is(err, store.ErrState) {
		t.Fatalf("a graded paper: %v", err)
	}
	lapsed, err := store.IssueExam(ctx, w.rt, examAsk("h3", "exec:cursor"), set)
	ok(t, err)
	if _, err = store.VoidExamPaperLater(ctx, w.rt, lapsed.RunID, "too late", voidApproval, store.ExamPaperLife+time.Minute); !errors.Is(err, store.ErrState) {
		t.Fatalf("a lapsed paper: %v", err)
	}
	for name, c := range map[string]struct{ run, reason, approval string }{
		"no such paper":     {"ab" + lapsed.RunID[2:], "x", voidApproval},
		"a short run id":    {lapsed.RunID[:12], "x", voidApproval},
		"no approval":       {lapsed.RunID, "x", ""},
		"an approval's num": {lapsed.RunID, "x", "#212"},
		"no reason":         {lapsed.RunID, "  ", voidApproval},
		"two lines":         {lapsed.RunID, "a\nb", voidApproval},
	} {
		if _, err = store.VoidExamPaper(ctx, w.rt, c.run, c.reason, c.approval); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := w.examEvents(t, "note"); n != 0 {
		t.Fatalf("%d notes after refusals", n)
	}
	// The answered paper still holds exec:claude: a refused void settled nothing.
	if _, err = store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set); !errors.Is(err, store.ErrState) {
		t.Fatalf("exec:claude after a refused void: %v", err)
	}
}
