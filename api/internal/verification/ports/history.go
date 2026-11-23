package ports

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/mainhistory"
)

// For lineage's main-history reader (lineage.HistoryRules, bound in
// server/agentrt_planfacts.go): lineage reads main's history from Gitea and
// asks verification's landing rules what it says about a result.

// HistoryCommit is one commit of main's history as the landing rules read it.
type HistoryCommit struct {
	SHA, Message string
	Parents      []string
}

// HistoryRules are the landing rules over main's history.
type HistoryRules struct {
	// Evaluate says whether result landed for task, was reverted, or is
	// unknown, from commits (newest first) and whether they are complete.
	Evaluate func(task, result, message string, commits []HistoryCommit, complete bool) (mainhistory.MainHistory, error)
	// CherryPickSource is the commit a message says it was cherry-picked
	// from, "" when it names none or more than one.
	CherryPickSource func(message string) string
	// HasTaskTrailer says message carries exactly one Agentrt-Task trailer
	// naming task.
	HasTaskTrailer func(message, task string) bool
}

// MainHistoryRules are landinggit's rules for lineage's history reader. The
// commits are copied field by field; the rules are landinggit's, unchanged.
func MainHistoryRules() HistoryRules {
	return HistoryRules{
		Evaluate: func(task, result, message string, commits []HistoryCommit, complete bool) (mainhistory.MainHistory, error) {
			in := make([]landinggit.HistoryCommit, len(commits))
			for i, c := range commits {
				in[i] = landinggit.HistoryCommit{SHA: c.SHA, Message: c.Message, Parents: c.Parents}
			}
			return landinggit.EvaluateHistory(task, result, message, in, complete)
		},
		CherryPickSource: landinggit.CherryPickSource,
		HasTaskTrailer:   landinggit.HasTaskTrailer,
	}
}
