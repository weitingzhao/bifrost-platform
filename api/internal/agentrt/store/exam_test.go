package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// fakeExam is a made-up set, not the exam: three items of the chief's exam
// under two classes; an answer passes when it says PASS. Each item renders at
// a t0 of its own, 61 minutes before the paper's time (as a scheduled brief
// renders at its slot), and the grader keeps the t0s it was given.
type fakeExam struct {
	digest string
	mu     sync.Mutex
	t0s    []time.Time
}

func newFakeExam() *fakeExam { return &fakeExam{digest: strings.Repeat("e1", 32)} }

func (f *fakeExam) Digest() string { return f.digest }

func (f *fakeExam) Items(job string) []store.ExamItem {
	if job != "chief_of_staff" {
		return nil
	}
	return []store.ExamItem{{ID: "F1", Class: "exam-brief"}, {ID: "F2", Class: "exam-brief"}, {ID: "F3", Class: "exam-alert"}}
}

func (f *fakeExam) Render(item string, now time.Time) (store.ExamSpec, error) {
	sp := store.ExamSpec{T0: now.Add(-61 * time.Minute), Spec: []byte(`{"text": "# ` + item + `"}`)}
	if item == "F3" {
		sp.Repo, sp.BaseCommit = "bifrost-trade-infra", "792a5ac"
	}
	return sp, nil
}

func (f *fakeExam) Grade(_, answer string, t0 time.Time) (store.ExamVerdict, error) {
	f.mu.Lock()
	f.t0s = append(f.t0s, t0)
	f.mu.Unlock()
	if strings.Contains(answer, "PASS") {
		return store.ExamVerdict{Passed: true}, nil
	}
	return store.ExamVerdict{Failed: []string{"says-pass"}, Flagged: []string{"soft"}}, nil
}

func examAsk(host, executor string) store.ExamRequest {
	return store.ExamRequest{Host: host, Token: "agentd:" + host, Job: "chief_of_staff", Executor: executor}
}

func part(p *store.ExamPaper, answers ...store.ExamAnswer) store.ExamSubmission {
	return store.ExamSubmission{RunID: p.RunID, Executor: p.Executor, Provider: "anthropic", Model: "opus", ToolVersion: "2.1.0 (Claude Code)",
		ExamSHA256: p.SetSHA256, PinsOK: true, Answers: answers}
}

func ans(item string, run int, text string) store.ExamAnswer {
	return store.ExamAnswer{Item: item, RunNo: run, Round: 1, Text: text, WallS: 60 * run, Conclusion: "done"}
}

// examEvents counts the events of a type on the exams stream.
func (w *world) examEvents(t *testing.T, typ string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = $2`, store.ExamsStream, typ).Scan(&n))
	return n
}

// W-31 2.4 ④ (the exam's cut §6 甲): a paper is issued, answered in two
// parts and graded once, every run at the t0 its item was rendered at; one
// row a question; handing a part in again is answered with the first result.
func TestExamIssueAnswerGrade(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	p, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
	ok(t, err)
	if len(p.RunID) != 64 || len(p.Items) != 3 || !p.ExpiresAt.Equal(p.IssuedAt.Add(store.ExamPaperLife)) || p.SetSHA256 != set.digest {
		t.Fatalf("paper %+v", p)
	}
	for _, it := range p.Items {
		want := 1
		if it.Item == "F3" {
			want = 2
		}
		if fmt.Sprint(it.Runs) != "[1 2 3]" || it.Rounds != want || !it.T0.Equal(p.T0.Add(-61*time.Minute)) || len(it.Spec) == 0 {
			t.Fatalf("item %+v of paper at %s", it, p.T0)
		}
	}
	out, err := store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS"), ans("F1", 2, "PASS"), ans("F1", 3, "PASS"),
		ans("F2", 1, "no")), set)
	ok(t, err)
	if out["graded"] != false || out["taken"] != 4 || out["answered"] != 4 || out["expected"] != 9 {
		t.Fatalf("first part: %v", out)
	}
	oow := ans("F3", 3, "PASS")
	oow.OutOfWorkspace = true
	// An executor that timed out (or failed) is not passed, whatever its
	// text says (review 415 A1).
	late := ans("F2", 3, "PASS")
	late.Conclusion = "failed"
	second := part(p, ans("F2", 2, "PASS"), late, ans("F3", 1, "PASS"), ans("F3", 2, "no"), oow)
	out, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", second, set)
	ok(t, err)
	if out["graded"] != true || out["rows"] != 2 || out["answered"] != 9 || out["duplicate"] != false {
		t.Fatalf("second part: %v", out)
	}
	for k := range out {
		if k == "results" || k == "failed" || k == "samples" {
			t.Fatalf("the answer carries %s: %v", k, out)
		}
	}
	// Graded at the paper's t0s, never the clock's; the run that read
	// outside its workspace, and the one that did not end done, are not
	// graded at all.
	if len(set.t0s) != 7 {
		t.Fatalf("graded %d runs, want 7", len(set.t0s))
	}
	for _, t0 := range set.t0s {
		if !t0.Equal(p.T0.Add(-61 * time.Minute)) {
			t.Fatalf("graded at %s; the paper's items are at %s", t0, p.T0.Add(-61*time.Minute))
		}
	}
	rows, err := w.rt.Query(ctx, `SELECT task_class, attempts, first_pass, job_id, core, tool_version, score::float8 FROM agentrt.capability_score
	  WHERE source = 'exam' ORDER BY task_class`)
	ok(t, err)
	var got []string
	for rows.Next() {
		var class, job, tool string
		var attempts, first int
		var core bool
		var score float64
		ok(t, rows.Scan(&class, &attempts, &first, &job, &core, &tool, &score))
		got = append(got, fmt.Sprintf("%s %d/%d %s %v %s %.4f", class, first, attempts, job, core, tool, score))
	}
	ok(t, rows.Err())
	want := []string{fmt.Sprintf("exam-alert 1/3 chief_of_staff false 2.1.0 (Claude Code) %.4f", store.Wilson(1, 3)),
		fmt.Sprintf("exam-brief 4/6 chief_of_staff false 2.1.0 (Claude Code) %.4f", store.Wilson(4, 6))}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	var body map[string]any
	var cause int64
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json, cause_event_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'capability_scored'`,
		store.ExamsStream).Scan(&body, &cause))
	results, _ := body["results"].([]any)
	if body["source"] != "exam" || body["run_id"] != p.RunID || body["questions"] != set.digest || body["host"] != "h1" || len(results) != 9 ||
		!strings.Contains(fmt.Sprint(results), "verdict:out_of_workspace") || !strings.Contains(fmt.Sprint(results), "failed:[says-pass]") ||
		!strings.Contains(fmt.Sprint(results), "conclusion:failed item:F2 round:1 run_no:3 verdict:not_done") {
		t.Fatalf("capability_scored %v", body)
	}
	var paperEvent int64
	ok(t, w.rt.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'artifact_put'
	  AND body_json->>'artifact_id' = $2`, store.ExamsStream, "sha256:"+p.RunID).Scan(&paperEvent))
	if cause != paperEvent {
		t.Fatalf("the record is caused by %d, the paper is %d", cause, paperEvent)
	}
	again, err := store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", second, set)
	ok(t, err)
	if again["duplicate"] != true || again["event_id"] != out["event_id"] || again["repeated"] != 5 || w.examEvents(t, "capability_scored") != 1 {
		t.Fatalf("the part again: %v", again)
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.capability_score WHERE source = 'exam'`).Scan(&n))
	if n != 2 || len(set.t0s) != 7 {
		t.Fatalf("handing a graded run in again recorded %d rows, graded %d runs", n, len(set.t0s))
	}
	if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS, said otherwise")), set); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("another answer to a graded run: %v", err)
	}
}

// What is refused: a second open paper of an executor, a job with no exam, a
// run unknown, another host's, lapsed or of another set, pins not held, an
// item not on the paper, an answer given twice in a part or changed, another
// model halfway, a local path.
func TestExamRefusals(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	p, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
	ok(t, err)
	if _, err = store.IssueExam(ctx, w.rt, examAsk("h2", "exec:claude"), set); !errors.Is(err, store.ErrState) {
		t.Fatalf("a second open paper of exec:claude: %v", err)
	}
	if _, err = store.IssueExam(ctx, w.rt, examAsk("h1", "exec:codex"), set); err != nil {
		t.Fatalf("a paper of another executor: %v", err)
	}
	for name, req := range map[string]store.ExamRequest{
		"another job":       {Host: "h1", Token: "agentd:h1", Job: "implementer", Executor: "exec:cursor"},
		"the deterministic": examAsk("h1", "exec:deterministic"),
		"an unknown tool":   examAsk("h1", "exec:nobody"),
		"no token":          {Host: "h1", Job: "chief_of_staff", Executor: "exec:cursor"},
	} {
		if _, err = store.IssueExam(ctx, w.rt, req, set); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	other := *p
	other.RunID = strings.Repeat("ab", 32)
	if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(&other, ans("F1", 1, "PASS")), set); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an unknown run: %v", err)
	}
	if _, err = store.RecordExamAnswers(ctx, w.rt, "h2", "agentd:h2", part(p, ans("F1", 1, "PASS")), set); !errors.Is(err, store.ErrOtherHost) {
		t.Fatalf("another host's run: %v", err)
	}
	for name, mutate := range map[string]func(*store.ExamSubmission){
		"pins not held":       func(s *store.ExamSubmission) { s.PinsOK = false },
		"another set":         func(s *store.ExamSubmission) { s.ExamSHA256 = strings.Repeat("0f", 32) },
		"another executor":    func(s *store.ExamSubmission) { s.Executor = "exec:codex" },
		"an item not on it":   func(s *store.ExamSubmission) { s.Answers[0].Item = "F9" },
		"a fourth run":        func(s *store.ExamSubmission) { s.Answers[0].RunNo = 4 },
		"a second round":      func(s *store.ExamSubmission) { s.Answers[0].Round = 2 },
		"twice in a part":     func(s *store.ExamSubmission) { s.Answers = append(s.Answers, s.Answers[0]) },
		"a conclusion":        func(s *store.ExamSubmission) { s.Answers[0].Conclusion = "maybe" },
		"no tool version":     func(s *store.ExamSubmission) { s.ToolVersion = "" },
		"a local path":        func(s *store.ExamSubmission) { s.Answers[0].Text = "PASS, see /home/bifrost-agentd-stg/x" },
		"a workspace":         func(s *store.ExamSubmission) { s.Answers[0].Text = "PASS, see tasks/t_01M5B2WKM1CYDFE4PWGE4D3D42/a1/x" },
		"no answers":          func(s *store.ExamSubmission) { s.Answers = nil },
		"a run id not a hash": func(s *store.ExamSubmission) { s.RunID = "run-0001" },
	} {
		s := part(p, ans("F1", 1, "PASS"))
		mutate(&s)
		if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", s, set); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A second round is the plan's: F3 takes it.
	f3 := ans("F3", 1, "PASS")
	f3.Round = 2
	out, err := store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS"), f3), set)
	ok(t, err)
	if out["taken"] != 2 {
		t.Fatalf("a part: %v", out)
	}
	out, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS"), ans("F1", 2, "PASS")), set)
	ok(t, err)
	if out["taken"] != 1 || out["repeated"] != 1 || out["answered"] != 3 {
		t.Fatalf("a part with one answer again: %v", out)
	}
	if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS, better")), set); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("another answer to a run answered: %v", err)
	}
	for name, mutate := range map[string]func(*store.ExamSubmission){
		"another model":        func(s *store.ExamSubmission) { s.Model = "sonnet" },
		"another provider":     func(s *store.ExamSubmission) { s.Provider = "cursor-mix" },
		"another tool version": func(s *store.ExamSubmission) { s.ToolVersion = "2.2.0 (Claude Code)" },
	} {
		halfway := part(p, ans("F1", 3, "PASS"))
		mutate(&halfway)
		if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", halfway, set); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("%s halfway: %v", name, err)
		}
	}
	moved := newFakeExam()
	moved.digest = strings.Repeat("e2", 32)
	if _, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 3, "PASS")), moved); !errors.Is(err, store.ErrState) {
		t.Fatalf("a run of a set the platform no longer has: %v", err)
	}
	if _, err = store.RecordExamAnswersLater(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 3, "PASS")), set, store.ExamPaperLife); !errors.Is(err, store.ErrState) {
		t.Fatalf("a lapsed run: %v", err)
	}
	if n := w.examEvents(t, "capability_scored"); n != 0 {
		t.Fatalf("%d runs graded", n)
	}
}

// A paper that lapses with answers is graded when a paper is next asked
// for, its runs without an answer not passed (the cut §6: 缺的记不过); one
// that lapses with none records nothing; then the executor may sit again.
func TestExamLapsedIsGraded(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	p, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
	ok(t, err)
	_, err = store.IssueExam(ctx, w.rt, examAsk("h2", "exec:codex"), set)
	ok(t, err)
	_, err = store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 1, "PASS"), ans("F3", 2, "no")), set)
	ok(t, err)
	next, err := store.IssueExamLater(ctx, w.rt, examAsk("h1", "exec:claude"), set, store.ExamPaperLife+time.Minute)
	ok(t, err)
	if next.RunID == p.RunID || w.examEvents(t, "capability_scored") != 1 {
		t.Fatalf("after the lapse: paper %s, %d graded", next.RunID, w.examEvents(t, "capability_scored"))
	}
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'capability_scored'`,
		store.ExamsStream).Scan(&body))
	results := fmt.Sprint(body["results"])
	if body["run_id"] != p.RunID || strings.Count(results, "verdict:missing") != 7 || strings.Count(results, "verdict:pass") != 1 {
		t.Fatalf("the lapsed run: %v", body)
	}
	var attempts, first int
	ok(t, w.rt.QueryRow(ctx, `SELECT attempts, first_pass FROM agentrt.capability_score WHERE source = 'exam' AND task_class = 'exam-brief'`).
		Scan(&attempts, &first))
	if attempts != 6 || first != 1 {
		t.Fatalf("exam-brief %d of %d", first, attempts)
	}
	if _, err = store.IssueExamLater(ctx, w.rt, examAsk("h1", "exec:claude"), set, store.ExamPaperLife+2*time.Minute); !errors.Is(err, store.ErrState) {
		t.Fatalf("the new paper is open: %v", err)
	}
	if _, err = store.RecordExamAnswersLater(ctx, w.rt, "h1", "agentd:h1", part(p, ans("F1", 2, "PASS")), set, store.ExamPaperLife+time.Minute); !errors.Is(err, store.ErrState) {
		t.Fatalf("an answer to the graded lapsed run: %v", err)
	}
}

// Routing does not read the exams' stream (the cut §6: the exam decides
// probation to active, which the Owner signs; competence.go reads in-job
// computations and benchmark runs): a record on it that would exclude codex
// if routing read it leaves codex routed, and the same record on the
// benchmarks' stream excludes it (the positive control).
func TestRoutingIgnoresExams(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, 30, 3))
	record := func(stream string) {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "capability_scored", Actor: "controller", Body: map[string]any{
				"source": "exam", "rows": 1, "run_id": "run-x-000001", "host": "h1", "executor": "exec:codex", "provider": "openai", "model": "gpt-x",
				"questions": strings.Repeat("0f", 32), "started_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
				"samples": []any{map[string]any{"executor": "exec:codex", "provider": "openai", "model": "gpt-x", "job": "implementer",
					"class": "go", "core": false, "attempts": 9, "first_pass": 0}}}})
			return err
		}))
	}
	record(store.ExamsStream)
	task := w.ready(t, store.Task{})
	if body := w.lastRoute(t, task); !strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") {
		t.Fatalf("an exam record excluded codex: %v", body["excluded"])
	}
	record(store.BenchmarksStream)
	n, err := store.Reroute(ctx, w.rt)
	ok(t, err)
	if body := w.lastRoute(t, task); n == 0 || strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") {
		t.Fatalf("the same record on the benchmarks' stream left codex routed (%d rerouted): %v", n, body["candidates"])
	}
}

// all answers every run of every item of p with "PASS".
func all(p *store.ExamPaper) []store.ExamAnswer {
	var out []store.ExamAnswer
	for _, it := range p.Items {
		for _, r := range it.Runs {
			out = append(out, ans(it.Item, r, "PASS"))
		}
	}
	return out
}

// Review 406 B2: the same executor and model sat twice in a day: the second
// run is graded and recorded beside the first, not over it.
func TestExamSameDayRetake(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	for i := 1; i <= 2; i++ {
		p, err := store.IssueExam(ctx, w.rt, examAsk("h1", "exec:claude"), set)
		ok(t, err)
		out, err := store.RecordExamAnswers(ctx, w.rt, "h1", "agentd:h1", part(p, all(p)...), set)
		ok(t, err)
		if out["graded"] != true {
			t.Fatalf("run %d: %v", i, out)
		}
	}
	var rows, ends int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*), count(DISTINCT window_end) FROM agentrt.capability_score WHERE source = 'exam'`).Scan(&rows, &ends))
	if rows != 4 || ends != 2 || w.examEvents(t, "capability_scored") != 2 {
		t.Fatalf("%d rows over %d window ends, %d records; want 4 over 2, 2", rows, ends, w.examEvents(t, "capability_scored"))
	}
}

// Review 406 B2: what the exams' lock orders, raced: two papers asked for
// one executor at once give one paper and one refusal; the two halves of a
// run handed in at once are graded once. The test holds the lock while both
// calls wait on it.
func TestExamRaces(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	set := newFakeExam()
	pool := w.db.Pool(t, w.db.Runtime)
	race := func(what string, calls ...func() error) []error {
		t.Helper()
		// On a connection of its own: the waits are read on w.rt, outside
		// any transaction (pg_stat_activity is a snapshot inside one).
		holder, err := pool.Begin(ctx)
		ok(t, err)
		// Released on every path, a failed wait included, so the pool can
		// close (review 409 B3); a rollback after the first is a no-op.
		defer func() { _ = holder.Rollback(context.Background()) }()
		_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock(742113)`)
		ok(t, err)
		errs := make([]error, len(calls))
		done := make(chan struct{}, len(calls))
		for i, c := range calls {
			go func() { errs[i] = c(); done <- struct{}{} }()
		}
		// A call that never waits on the lock leaves the wait to fail; the
		// holder is rolled back by the defer and the calls end.
		w.waitForN(t, "pg_advisory_xact_lock", len(calls), time.Now().Add(20*time.Second))
		ok(t, holder.Rollback(ctx))
		for range calls {
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatalf("%s: a call never returned", what)
			}
		}
		return errs
	}
	var papers [2]*store.ExamPaper
	errs := race("two papers", func() (err error) {
		papers[0], err = store.IssueExam(ctx, pool, examAsk("h1", "exec:claude"), set)
		return
	},
		func() (err error) {
			papers[1], err = store.IssueExam(ctx, pool, examAsk("h2", "exec:claude"), set)
			return
		})
	issued := 0
	var p *store.ExamPaper
	for i, err := range errs {
		switch {
		case err == nil:
			issued, p = issued+1, papers[i]
		case !errors.Is(err, store.ErrState):
			t.Fatalf("a paper raced: %v", err)
		}
	}
	if issued != 1 {
		t.Fatalf("%d papers for one executor at once", issued)
	}
	answers := all(p)
	half := len(answers) / 2
	var outs [2]map[string]any
	errs = race("two halves", func() (err error) {
		outs[0], err = store.RecordExamAnswers(ctx, pool, p.Host, "agentd:"+p.Host, part(p, answers[:half]...), set)
		return
	}, func() (err error) {
		outs[1], err = store.RecordExamAnswers(ctx, pool, p.Host, "agentd:"+p.Host, part(p, answers[half:]...), set)
		return
	})
	for _, err := range errs {
		ok(t, err)
	}
	if (outs[0]["graded"] == true) == (outs[1]["graded"] == true) || w.examEvents(t, "capability_scored") != 1 {
		t.Fatalf("the halves: %v / %v, %d records", outs[0], outs[1], w.examEvents(t, "capability_scored"))
	}

	// The last answer of a run against a paper asked for after the run's
	// paper lapsed (review 406 item 5): whichever takes the lock first, the
	// run is graded once, with or without that answer, and the answer is
	// either kept and graded or refused.
	q, err := store.IssueExam(ctx, pool, examAsk("h3", "exec:cursor"), set)
	ok(t, err)
	rest := all(q)
	_, err = store.RecordExamAnswers(ctx, pool, "h3", "agentd:h3", part(q, rest[:len(rest)-1]...), set)
	ok(t, err)
	before := w.examEvents(t, "capability_scored")
	var last map[string]any
	errs = race("the last answer and a lapse", func() (err error) {
		last, err = store.RecordExamAnswers(ctx, pool, "h3", "agentd:h3", part(q, rest[len(rest)-1]), set)
		return
	}, func() error {
		_, err := store.IssueExamLater(ctx, pool, examAsk("h3", "exec:cursor"), set, store.ExamPaperLife+time.Minute)
		return err
	})
	ok(t, errs[1])
	var answered int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'artifact_put'
	  AND cause_event_id = (SELECT event_id FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'artifact_put'
	    AND body_json->>'artifact_id' = $2)`, store.ExamsStream, "sha256:"+q.RunID).Scan(&answered))
	switch {
	case w.examEvents(t, "capability_scored") != before+1:
		t.Fatalf("the run was graded %d times", w.examEvents(t, "capability_scored")-before)
	case errs[0] == nil && (last["graded"] != true || answered != len(rest)):
		t.Fatalf("the last answer taken first: %v, %d answers kept", last, answered)
	case errs[0] != nil && (!errors.Is(errs[0], store.ErrState) || answered != len(rest)-1):
		t.Fatalf("the last answer after the lapse: %v, %d answers kept", errs[0], answered)
	}
}
