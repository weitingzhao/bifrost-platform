package agentd

import (
	"context"
	"fmt"
	"strings"
)

// What a review is told of the attempt it checks (TD-409; STG M-93,
// 2026-10-20: an implementation of effect none slept its 2700 s and
// reported "slept 2700 s", and two cursor reviews, given only its spec and
// its check's verify.json, looked for that line in an empty checkout and
// failed it twice). The claim names the report the checked attempt handed in
// and its task's effect class (the platform's store/claimsubject.go); agentd
// reads the report for a review (a deterministic check runs commands and
// reads no prompt), and the prompt quotes it right after the subject's spec,
// at most subjectReportBytes, cut further when the prompt's one argument
// would not hold it (promptMax). For a task that changes no files it says
// the checkout holds none of the work: the review goes by the report and the
// check. The report was masked of local paths when agentd handed it in, and
// the platform takes none that still has one (H8).

// subjectReportBytes is the most of the checked attempt's report a review's
// prompt quotes.
const subjectReportBytes = 16 << 10

// subjectReport reads the report the checked attempt handed in, for a
// review whose claim names one.
func (d *Daemon) subjectReport(ctx context.Context, a *Attempt) ([]byte, error) {
	if a.Spec.Check != "review" || a.Claim == nil || a.Claim.SubjectReport == "" {
		return nil, nil
	}
	body, err := d.Client.GetArtifact(ctx, a.Claim.SubjectReport)
	if err != nil {
		return nil, fmt.Errorf("agentd: the report of attempt %s, which it checks: %w", a.Spec.Attempt, err)
	}
	return body, nil
}

// writeSubject writes, after the subject's spec, the report the checked
// attempt handed in (at most quota bytes of it, none at 0) and, for a task
// of effect none, what the review goes by.
func writeSubject(b *strings.Builder, a *Attempt, quota int) {
	quoted := len(a.SubjectReport) > 0 && quota > 0
	if quoted {
		fmt.Fprintf(b, "\nThe report attempt %s handed in:\n\n%s", a.Spec.Attempt, fenced(cutStart(a.SubjectReport, quota)))
	}
	if a.Claim == nil || a.Claim.SubjectEffect != "none" {
		return
	}
	if quoted {
		b.WriteString("\nThat task changes no files (effect none): this checkout holds none of its work. Judge it by the report above and its deterministic check, not by what is or is not in the checkout.\n")
		return
	}
	b.WriteString("\nThat task changes no files (effect none): this checkout holds none of its work, and its report did not reach you. Do not look for its work in the checkout; say in your report that you could not see what it did.\n")
}
