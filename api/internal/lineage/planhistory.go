package lineage

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
)

// PlanHistory pins the head before pagination and refuses truncated history.
// It preserves reverted results, unlike the cleanup-only MainResults index.
func PlanHistory(ctx context.Context, acc Access, client *http.Client, repo, task, result, snapshot string, maxPages int) (landing.MainHistory, error) {
	unknown := landing.MainHistory{State: landing.Unknown}
	if maxPages < 1 {
		return unknown, errors.New("history page budget missing")
	}
	g := &gitea{acc: acc, client: client}
	target, err := g.commits(ctx, repo, result, time.Time{}, 1, nil)
	if err != nil {
		return unknown, err
	}
	if len(target) == 0 || target[0].SHA != result {
		return unknown, errors.New("result unavailable")
	}
	if snapshot == "" {
		var head struct {
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		if err = g.get(ctx, g.repoPath(repo)+"/branches/main", nil, &head); err != nil {
			return unknown, err
		}
		snapshot = head.Commit.ID
		if snapshot == "" {
			return unknown, errors.New("main unavailable")
		}
	}
	commits, err := g.commits(ctx, repo, snapshot, time.Time{}, maxPages, nil)
	if err != nil {
		return unknown, err
	}
	if len(commits) == 0 || commits[0].SHA != snapshot {
		return unknown, errors.New("snapshot mismatch")
	}
	seen := map[string]bool{}
	history := make([]landinggit.HistoryCommit, 0, len(commits))
	for _, c := range commits {
		seen[c.SHA] = true
		history = append(history, landinggit.HistoryCommit{SHA: c.SHA, Message: c.Msg})
	}
	complete := len(commits) < maxPages*pageLimit
	for _, c := range commits {
		for _, p := range c.Parents {
			complete = complete && seen[p]
		}
	}
	return landinggit.EvaluateHistory(task, result, target[0].Msg, history, complete)
}
