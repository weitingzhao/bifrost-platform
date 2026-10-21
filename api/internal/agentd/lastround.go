package agentd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// What an implementation in a round after the first is told of the rounds
// before it (TD-405). Each round starts from the task's base commit with the
// same spec; on PROD M-23 codex, told nothing of the check that sent the last
// round back, handed in the same compile error three rounds running. The
// claim names the checks the task's earlier rounds failed (the platform's
// store/claimfailed.go: its last deterministic check and its last review,
// each when it failed); agentd reads their reports and Prompt puts them after
// the spec: for a deterministic check its first failed command and the end of
// that command's output, for a review the reviewer's report.

// PriorCheck is a check an earlier round failed, with its report: agentd's
// verify.json for a deterministic check, the reviewer's report for a review.
type PriorCheck struct {
	FailedCheck
	Report []byte
}

// priorReviewBytes is the most of a failed review's report a prompt quotes:
// a prompt is one argument of the executor's command, which Linux holds to
// 128 KiB.
const priorReviewBytes = 16 << 10

// priorChecks reads the reports of the checks an implementation's claim
// names; a check's attempt is told none.
func (d *Daemon) priorChecks(ctx context.Context, a *Attempt) ([]PriorCheck, error) {
	if a.Spec.Check != "" || a.Claim.Role != "implement" || len(a.Claim.FailedChecks) == 0 {
		return nil, nil
	}
	out := make([]PriorCheck, 0, len(a.Claim.FailedChecks))
	for _, f := range a.Claim.FailedChecks {
		body, err := d.Client.GetArtifact(ctx, f.Artifact)
		if err != nil {
			return nil, fmt.Errorf("agentd: the report of the %s check of attempt %d: %w", f.Method, f.AttemptNo, err)
		}
		out = append(out, PriorCheck{FailedCheck: f, Report: body})
	}
	d.logf("agentd: %s: told of %d failed check(s) of the task's earlier rounds", a.Claim.AttemptID, len(out))
	return out, nil
}

// writePriorChecks writes, after the spec, what the checks of the task's
// earlier rounds failed on.
func writePriorChecks(b *strings.Builder, checks []PriorCheck) {
	if len(checks) == 0 {
		return
	}
	b.WriteString("\n\n---\nThe last round failed its check; fix that first. This round starts again from the same commit, " +
		"without that round's changes: make sure yours do not fail the same way.\n")
	for _, c := range checks {
		switch c.Method {
		case "deterministic":
			fmt.Fprintf(b, "\nThe deterministic check of attempt %d failed. %s", c.AttemptNo, failedCommand(c.Report))
		case "review":
			fmt.Fprintf(b, "\nThe review of attempt %d failed it. Its report:\n\n%s", c.AttemptNo, fenced(cutStart(c.Report, priorReviewBytes)))
		}
	}
}

// failedCommand is what a deterministic check's verify.json says failed: the
// first command that exited non-zero or timed out and the end of its output
// (the check stops at the first failure); with none, its reason; a report
// that is not verify.json, its start.
func failedCommand(report []byte) string {
	var v Verification
	if json.Unmarshal(report, &v) != nil {
		return "Its report:\n\n" + fenced(cutStart(report, priorReviewBytes))
	}
	for _, c := range v.Commands {
		if c.ExitCode == 0 && !c.TimedOut {
			continue
		}
		how := fmt.Sprintf("exited %d", c.ExitCode)
		if c.TimedOut {
			how += ", timed out"
		}
		head := "First failed command: " + oneLine(strings.Join(c.Argv, " "), 300) + " " + how
		if tail := strings.TrimRight(strings.ToValidUTF8(c.Tail, "?"), " \t\r\n"); tail != "" {
			return head + ". Its output ends:\n\n" + fenced(tail)
		}
		return head + "; it wrote nothing.\n"
	}
	return "No command failed; its reason: " + oneLine(v.Reason, 300) + "\n"
}

// fenced is s in a markdown code block whose fence is longer than any run of
// backticks in it.
func fenced(s string) string {
	fence := "```"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence + "\n" + strings.TrimRight(s, "\n") + "\n" + fence + "\n"
}

// cutStart is the start of b as text, at most max bytes without splitting a
// character, saying so when it cut.
func cutStart(b []byte, max int) string {
	s := strings.ToValidUTF8(string(b), "?")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n(cut: the report is %d bytes; this is its start)", len(s))
}
