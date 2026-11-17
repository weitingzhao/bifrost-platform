package approvals

import (
	"regexp"
	"strings"
)

// What an execution card must show the Owner, and the mistakes that cost an
// approval before, checked at the moment the card is built (Owner and main
// coordinator 2026-11-15, from the three-questions review of the gates: the
// Owner decides in about a minute and cannot read a script, so the card
// carries two plain lines and the known traps are refused where the
// requester still sees them, not found later by a second reader).
//
// Only owner_run_command cards are checked here; platform actions and
// releases have their own cards.

const (
	changedLabel  = "改了什么"
	newPowerLabel = "批了以后谁因此能做什么新事"
)

// labelled reports whether some line of reason starts with label followed by
// a colon (full width or ASCII) and some text.
func labelled(reason, label string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(reason, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*• \t")
		if !strings.HasPrefix(line, label) {
			continue
		}
		rest := strings.TrimPrefix(line, label)
		rest = strings.TrimLeft(rest, " \t")
		for _, colon := range []string{"：", ":"} {
			if strings.HasPrefix(rest, colon) {
				if strings.TrimSpace(strings.TrimPrefix(rest, colon)) != "" {
					return true
				}
			}
		}
	}
	return false
}

var (
	rawFlag       = regexp.MustCompile(`--raw(\s|=|$)`)
	dryRunAny     = regexp.MustCompile(`--dry-run(=[^\s'"]*)?`)
	connectionArg = regexp.MustCompile(`--(kubeconfig|server|token|certificate-authority|client-key|client-certificate)(\s|=|$)`)
	// Words that destroy data in a database; each must be named in the reason.
	destructive = []string{"DROP", "TRUNCATE", "DELETE FROM"}
)

// ownerRunProblem returns a plain-language reason the card cannot be built,
// or "" when it can.
func ownerRunProblem(command, reason string) string {
	var missing []string
	if !labelled(reason, changedLabel) {
		missing = append(missing, "「"+changedLabel+"：」")
	}
	if !labelled(reason, newPowerLabel) {
		missing = append(missing, "「"+newPowerLabel+"：」")
	}
	if len(missing) > 0 {
		return "the reason must have a line starting with each of " + strings.Join(missing, " and ") +
			" followed by one plain sentence: what changes, and who can do what new thing once it is approved (the Owner decides in about a minute and cannot read the script)"
	}
	if rawFlag.MatchString(command) {
		return "the command uses --raw, which the execution port does not take (origin #550: a trial run with --raw really deleted); use the object API with an explicit path or a plan_manifest"
	}
	for _, m := range dryRunAny.FindAllString(command, -1) {
		if m != "--dry-run=server" {
			return "the command uses " + m + "; a trial run on the execution port must be --dry-run=server (origin #550: a trial run really deleted)"
		}
	}
	if m := connectionArg.FindString(command); m != "" {
		return "the command names its own connection (" + strings.TrimSpace(strings.TrimRight(m, " =")) + "); the execution port supplies its own identity, so leave the connection flags out"
	}
	upper := strings.ToUpper(command)
	upperReason := strings.ToUpper(reason)
	for _, word := range destructive {
		if strings.Contains(upper, word) && !strings.Contains(upperReason, word) {
			return "the command contains " + word + " and the reason does not say so: name what is dropped or deleted, and what holds the data, in the reason (origin #558/#559)"
		}
	}
	return ""
}
