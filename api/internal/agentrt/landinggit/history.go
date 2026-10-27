package landinggit

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// HistoryCommit is one commit of main's history; Parents (first parent first)
// let a revert of a merge undo what the merge brought in.
type HistoryCommit struct {
	SHA, Message string
	Parents      []string
}

// mergeRevert is git's second line of a revert of a merge ("git revert -m"):
// the parent it kept, the changes of the other parents being undone.
var mergeRevert = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-f]{40}),\s+reversing\s+changes\s+made\s+to\s+([0-9a-f]{40})\.`)

// EvaluateHistory is shared by Git and the read-only Gitea adapter. Work is
// never identity. A commit on main counts as the result only with exact
// provenance: it is the result, or a cherry-pick of it, and it carries the
// result's task or Change-Id. A Change-Id stays the same when an attempt is
// amended into the next one, so a commit sharing it without that provenance
// may be an older attempt: Unknown, not Effective (PV2 review 2026-10-26).
func EvaluateHistory(task, result, message string, commits []HistoryCommit, complete bool) (landing.MainHistory, error) {
	unknown := landing.MainHistory{State: landing.Unknown}
	if !complete || !fullSHA.MatchString(result) {
		return unknown, errors.New("incomplete history")
	}
	ids := changeTrailer.FindAllStringSubmatch(historyTrailers(message), -1)
	tasks := taskTrailer.FindAllStringSubmatch(historyTrailers(message), -1)
	if len(ids) > 1 || len(tasks) > 1 || (task != "" && (len(tasks) != 1 || tasks[0][1] != task)) || len(ids) == 0 && task == "" {
		return unknown, errors.New("ambiguous result lineage")
	}
	parents := map[string][]string{}
	for _, c := range commits {
		if !fullSHA.MatchString(c.SHA) {
			return unknown, errors.New("invalid history SHA")
		}
		parents[c.SHA] = c.Parents
	}
	// reachable is every commit reachable from sha, sha included.
	reachable := func(sha string) map[string]bool {
		out := map[string]bool{}
		queue := []string{sha}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			if out[x] {
				continue
			}
			out[x] = true
			queue = append(queue, parents[x]...)
		}
		return out
	}
	// Every "This reverts commit X" line counts, however many one commit has
	// and whatever its title: a revert ignored would leave a result Effective.
	reversals := map[string][]string{}
	reversalCommits := map[string]bool{}
	for _, c := range commits {
		title, _, _ := strings.Cut(c.Message, "\n")
		rs := revertLine.FindAllStringSubmatch(c.Message, -1)
		if len(rs) == 0 {
			continue
		}
		if strings.HasPrefix(title, `Revert "`) || strings.HasPrefix(title, `Reapply "`) {
			reversalCommits[c.SHA] = true
		}
		kept := map[string]string{}
		for _, m := range mergeRevert.FindAllStringSubmatch(c.Message, -1) {
			kept[m[1]] = m[2]
		}
		for _, r := range rs {
			undone := r[1]
			reversals[undone] = append(reversals[undone], c.SHA)
			ps := parents[undone]
			if len(ps) < 2 {
				continue
			}
			// A merge reverted: what it brought in is what is reachable from
			// it but not from the parent the revert kept.
			mainline := ps[0]
			if k, ok := kept[undone]; ok {
				// The parent a revert says it kept must be one of the merge's:
				// any other would mark the whole history before it reverted.
				if !slices.Contains(ps, k) {
					return unknown, errors.New("a merge revert names a parent the merge does not have")
				}
				mainline = k
			}
			base := reachable(mainline)
			for x := range reachable(undone) {
				if x != undone && !base[x] {
					reversals[x] = append(reversals[x], c.SHA)
				}
			}
		}
	}
	var undone func(string, map[string]bool) (bool, error)
	undone = func(sha string, path map[string]bool) (bool, error) {
		if path[sha] || len(path) > 1000 {
			return false, errors.New("cyclic revert history")
		}
		path[sha] = true
		defer delete(path, sha)
		for _, r := range reversals[sha] {
			u, e := undone(r, path)
			if e != nil {
				return false, e
			}
			if !u {
				return true, nil
			}
		}
		return false, nil
	}
	found := landing.MainHistory{State: landing.Never}
	unproven := false
	for _, c := range commits {
		if reversalCommits[c.SHA] {
			continue
		}
		ci := changeTrailer.FindAllStringSubmatch(historyTrailers(c.Message), -1)
		ct := taskTrailer.FindAllStringSubmatch(historyTrailers(c.Message), -1)
		provenance := c.SHA == result || strings.Contains(c.Message, "(cherry picked from commit "+result+")")
		idMatch := len(ids) == 1 && len(ci) == 1 && ci[0][1] == ids[0][1]
		if len(ci) > 1 || len(ct) > 1 {
			if provenance || idMatch {
				return unknown, errors.New("ambiguous main lineage")
			}
			continue
		}
		taskMatch := task != "" && len(ct) == 1 && ct[0][1] == task
		if !provenance || !(c.SHA == result || idMatch || taskMatch) {
			if idMatch {
				unproven = true
			}
			continue
		}
		u, e := undone(c.SHA, map[string]bool{})
		if e != nil {
			return unknown, e
		}
		if !u {
			return landing.MainHistory{State: landing.Effective, Commit: c.SHA}, nil
		}
		found = landing.MainHistory{State: landing.Reverted, Commit: c.SHA}
	}
	if unproven {
		return unknown, errors.New("a commit shares the result's Change-Id without its provenance")
	}
	return found, nil
}

// Only the terminal trailer block supplies identity; quoted trailer examples
// in a commit body do not bind a result to a task or Change-Id.
func historyTrailers(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	lines := []string{}
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "(cherry picked from commit ") && strings.HasSuffix(line, ")") {
			continue
		}
		lines = append(lines, line)
	}
	message = strings.TrimSpace(strings.Join(lines, "\n"))
	_, block, ok := strings.Cut(message, "\n\n")
	if !ok {
		return ""
	}
	if i := strings.LastIndex(block, "\n\n"); i >= 0 {
		block = block[i+2:]
	}
	for i, line := range strings.Split(block, "\n") {
		if i > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue // a folded continuation of the trailer above
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" || strings.ContainsAny(key, " \t") {
			return ""
		}
	}
	return block
}
