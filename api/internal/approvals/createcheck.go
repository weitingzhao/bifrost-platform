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
	rawFlag = regexp.MustCompile(`--raw(\s|=|$)`)
	// The value of --dry-run is read as a word, so "--dry-run=server}" in a
	// shell expansion and "--dry-run=server." at the end of a comment are the
	// server dry run they say (card #576 draft, 2026-11-17).
	dryRunAny     = regexp.MustCompile(`--dry-run(=[A-Za-z0-9_-]*)?`)
	connectionArg = regexp.MustCompile(`--(kubeconfig|server|token|certificate-authority|client-key|client-certificate)(\s|=|$)`)
	// Global kubectl flags that take their value as the next word; the
	// subcommand is the first word that is neither one of these nor its value.
	valueFlags = map[string]bool{"-n": true, "--namespace": true, "--context": true, "--cluster": true, "--user": true,
		"--as": true, "--as-group": true, "-v": true, "--v": true, "--request-timeout": true, "--cache-dir": true,
		"--profile": true, "--profile-output": true}
	// Words that destroy data in a database; each must be named in the reason.
	destructive = []string{"DROP", "TRUNCATE", "DELETE FROM"}
)

// kubectlSubcommand returns the subcommand of the kubectl command whose words
// are given (the words after "kubectl"), or "" when it cannot be told: the
// first word that is not a flag and not the value of a flag that takes one.
func kubectlSubcommand(words []string) string {
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case strings.HasPrefix(w, "-"):
			if valueFlags[w] {
				i++ // its value is the next word
			}
		default:
			return w
		}
	}
	return ""
}

// rawProblem allows --raw only as a read: on its line, the kubectl command it
// belongs to must have the subcommand get, read by position (not by finding
// the word get anywhere). Any other subcommand with --raw, one that cannot be
// told, or a --raw that is not part of a kubectl command, is refused. Origin:
// #550, a trial run with --raw really deleted (--raw ignores --dry-run);
// narrowed 2026-11-17 (main coordinator, Owner: the owner of the codebase
// decides) because a script that pages a list with "kubectl get --raw" was
// refused with it.
func rawProblem(command string) string {
	for _, line := range strings.Split(command, "\n") {
		for from := 0; ; {
			i := rawFlag.FindStringIndex(line[from:])
			if i == nil {
				break
			}
			at := from + i[0]
			from = at + len("--raw")
			head := line[:at]
			k := strings.LastIndex(head, "kubectl")
			if k < 0 {
				return "the command uses --raw outside a kubectl command, so it cannot be told to be a read; --raw is allowed only as \"kubectl get ... --raw\" (origin #550)"
			}
			words := strings.Fields(head[k:])
			sub := kubectlSubcommand(words[1:])
			if sub != "get" {
				if sub == "" {
					return "the command uses --raw and its kubectl subcommand cannot be told; --raw is allowed only as \"kubectl get ... --raw\" (origin #550)"
				}
				return "the command uses --raw with \"kubectl " + sub + "\"; --raw is allowed only with the subcommand get, because --raw ignores --dry-run (origin #550: a trial run with --raw really deleted)"
			}
		}
	}
	return ""
}

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
	if why := rawProblem(command); why != "" {
		return why
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
