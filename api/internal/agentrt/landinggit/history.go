package landinggit

import (
	"errors"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

type HistoryCommit struct{ SHA, Message string }

// EvaluateHistory is shared by Git and the read-only Gitea adapter. Work is
// never identity. A task trailer alone cannot make an old attempt count as a
// newer result: it must carry that exact SHA/cherry-pick provenance or Change-Id.
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
	reversals := map[string][]string{}
	reversalCommits := map[string]bool{}
	for _, c := range commits {
		if !fullSHA.MatchString(c.SHA) {
			return unknown, errors.New("invalid history SHA")
		}
		title, _, _ := strings.Cut(c.Message, "\n")
		rs := revertLine.FindAllStringSubmatch(c.Message, -1)
		if len(rs) == 1 && (strings.HasPrefix(title, `Revert "`) || strings.HasPrefix(title, `Reapply "`)) {
			reversals[rs[0][1]] = append(reversals[rs[0][1]], c.SHA)
			reversalCommits[c.SHA] = true
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
	for _, c := range commits {
		if reversalCommits[c.SHA] {
			continue
		}
		ci := changeTrailer.FindAllStringSubmatch(historyTrailers(c.Message), -1)
		ct := taskTrailer.FindAllStringSubmatch(historyTrailers(c.Message), -1)
		match := len(ids) == 1 && len(ci) == 1 && ci[0][1] == ids[0][1]
		if len(ci) > 1 || len(ct) > 1 {
			if strings.Contains(c.Message, result) || match {
				return unknown, errors.New("ambiguous main lineage")
			}
			continue
		}
		if task != "" && len(ct) == 1 && ct[0][1] == task && (c.SHA == result || strings.Contains(c.Message, "(cherry picked from commit "+result+")")) {
			match = true
		}
		if !match {
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
	for _, line := range strings.Split(block, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" || strings.ContainsAny(key, " \t") {
			return ""
		}
	}
	return block
}
