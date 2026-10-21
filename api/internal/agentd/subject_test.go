package agentd

import (
	"strings"
	"testing"
)

// TD-409: a review's prompt quotes the report the attempt it checks handed
// in right after that task's spec, before its check's verify.json, and for
// a task of effect none says the checkout holds none of the work; a check
// told of no report, or of a task that changes files, gets no such line, and
// every other prompt is what it was.
func TestAReviewIsToldWhatTheAttemptReported(t *testing.T) {
	review := func(report string, effect string) *Attempt {
		return &Attempt{Claim: &Claimed{Role: "review", SubjectEffect: effect}, SubjectReport: []byte(report),
			Spec:    TaskSpec{Text: "# Review it", Check: "review", SubjectTask: "t_1", Attempt: "a_1", Round: 1},
			Subject: &TaskSpec{Text: "# Sleep 2700 s"}, VerifyReport: []byte(`{"pass":true,"commands":[]}`)}
	}
	p := Prompt(review("# done\n\nslept 2700 s\n", "none"))
	spec, quoted := strings.Index(p, "# Sleep 2700 s"), strings.Index(p, "The report attempt a_1 handed in:\n\n```\n# done\n\nslept 2700 s\n```\n")
	verify := strings.Index(p, "Its deterministic check passed")
	if spec < 0 || quoted < spec || verify < quoted {
		t.Fatalf("the report is not between the spec and the verify.json:\n%s", p)
	}
	if !strings.Contains(p, "That task changes no files (effect none): this checkout holds none of its work. Judge it by the report above") {
		t.Fatalf("no line for effect none:\n%s", p)
	}
	if p := Prompt(review("", "none")); !strings.Contains(p, "its report did not reach you") || strings.Contains(p, "handed in:") {
		t.Fatalf("effect none, no report:\n%s", p)
	}
	if p := Prompt(review("# done", "branch")); strings.Contains(p, "effect none") || !strings.Contains(p, "handed in:") {
		t.Fatalf("a task that changes files:\n%s", p)
	}
	// Told of nothing (an older platform): the prompt is what it was.
	plain := review("", "")
	plain.Claim.SubjectEffect = ""
	if got := Prompt(plain); strings.Contains(got, "handed in:") || strings.Contains(got, "effect none") {
		t.Fatalf("a check told of nothing:\n%s", got)
	}
	// A long report is cut, and the prompt still fits one argument.
	big := review(strings.Repeat("output line\n", 4<<10), "none")
	big.Subject.Text = strings.Repeat("spec line\n", 118<<10/10)
	if p := Prompt(big); len(p) > promptMax || !strings.Contains(p, "effect none") {
		t.Fatalf("a large spec and report: %d bytes, over %d, or no effect line", len(p), promptMax)
	}
	if p := Prompt(review(strings.Repeat("output line\n", 4<<10), "none")); !strings.Contains(p, "(cut: the report is ") {
		t.Fatal("a report over subjectReportBytes is not cut")
	}
}

// Review td409-1 B1: a review prompt that fit its one argument before the
// report and the line for effect none were added still fits with them: at
// the last share neither is in it.
func TestAReviewPromptThatFitBeforeStillFits(t *testing.T) {
	for _, report := range []string{"# done", ""} {
		a := &Attempt{Claim: &Claimed{Role: "review"}, Spec: TaskSpec{Text: "# Review it", Check: "review", SubjectTask: "t_1", Attempt: "a_1", Round: 1},
			Subject: &TaskSpec{}, VerifyReport: []byte(`{"pass":true,"commands":[]}`)}
		a.Subject.Text = strings.Repeat("s", promptMax-len(Prompt(a)))
		before := Prompt(a)
		if len(before) != promptMax {
			t.Fatalf("the fixture's prompt is %d bytes, not %d", len(before), promptMax)
		}
		a.SubjectReport, a.Claim.SubjectEffect = []byte(report), "none"
		if got := Prompt(a); len(got) > promptMax || got != before {
			t.Errorf("report %q: a prompt of %d bytes (over %d: %v), or not the one that fit", report, len(got), promptMax, len(got) > promptMax)
		}
	}
}
