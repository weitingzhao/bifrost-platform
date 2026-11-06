package lineage

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	// pickedFrom is the line git cherry-pick -x writes, which the merges of
	// accepted results carry (scripts/agentrt/merge_accepted.py, and the hand
	// merges before it).
	pickedFrom = regexp.MustCompile(`(?m)^\(cherry picked from commit ([0-9a-f]{40})\)\s*$`)
	// revertsLine is git revert's own line in a revert's body.
	revertsLine = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-f]{40})\.\s*$`)
)

// MainResults reads repo's main back to since (at most maxPages pages of
// Gitea's commit list) and returns, for each agent runtime task an
// Agentrt-Task trailer names, the result commits main holds: the commit
// itself (a fast-forward) and the commit its "(cherry picked from commit …)"
// line names, each to the newest main commit that still stands as it. A
// commit a git revert on main undoes, and that no revert of the revert
// restores, does not stand (TD-383; review 436 A1, A2). Only git revert's own
// message counts as a revert (a subject starting `Revert "` or `Reapply "`,
// one "This reverts commit <sha>." line), as merge_accepted.py reads it. A
// history cut off by maxPages is read as far as it goes.
func MainResults(ctx context.Context, acc Access, client *http.Client, repo string, since time.Time, maxPages int) (map[string]map[string]string, error) {
	g := &gitea{acc: acc, client: client}
	commits, err := g.commits(ctx, repo, "main", since, maxPages, nil)
	if err != nil {
		return nil, err
	}
	return resultsForCommits(standingCommits(commits)), nil
}

// standingCommits applies the same revert rule to every main reader.
func standingCommits(commits []giteaCommit) []giteaCommit {
	reverts := map[string]string{} // the revert → the commit it reverts
	for _, c := range commits {
		subject, _ := parseMessage(c.Msg)
		if t := revertsLine.FindAllStringSubmatch(c.Msg, -1); len(t) == 1 && (strings.HasPrefix(subject, `Revert "`) || strings.HasPrefix(subject, `Reapply "`)) {
			reverts[c.SHA] = t[0][1]
		}
	}
	var undone func(sha string, depth int) bool
	undone = func(sha string, depth int) bool {
		if depth >= 20 {
			return false
		}
		for r, target := range reverts {
			if target == sha && !undone(r, depth+1) {
				return true
			}
		}
		return false
	}
	var standing []giteaCommit
	for _, c := range commits {
		if !undone(c.SHA, 0) {
			standing = append(standing, c)
		}
	}
	return standing
}

func resultsForCommits(commits []giteaCommit) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, c := range commits { // newest first
		_, t := parseMessage(c.Msg)
		if t.task == "" {
			continue
		}
		results := out[t.task]
		if results == nil {
			results = map[string]string{}
			out[t.task] = results
		}
		for _, from := range append([]string{c.SHA}, picked(c.Msg)...) {
			if _, seen := results[from]; !seen {
				results[from] = c.SHA
			}
		}
	}
	for _, c := range commits {
		for _, task := range tasksAnywhere(c.Msg) {
			if out[task] == nil {
				out[task] = map[string]string{}
			}
			if _, ok := out[task][c.SHA]; !ok {
				out[task][c.SHA] = c.SHA
			}
		}
	}
	return out
}

// picked is the commits a message's cherry-pick lines name.
func picked(msg string) []string {
	var out []string
	for _, m := range pickedFrom.FindAllStringSubmatch(strings.ReplaceAll(msg, "\r\n", "\n"), -1) {
		out = append(out, m[1])
	}
	return out
}

// tasksAnywhere reads folded task lines, retaining the trailer's task validation.
func tasksAnywhere(msg string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		m := trailerLine.FindStringSubmatch(line)
		if m != nil && strings.EqualFold(m[1], keyTask) && taskID.MatchString(m[2]) {
			out = append(out, m[2])
		}
	}
	return out
}
