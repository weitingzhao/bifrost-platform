package lineage

import (
	"context"
	"net/http"
	"time"
)

// MainTasks reads repo's main back to since (at most maxPages pages of
// Gitea's commit list) and returns each agent runtime task an Agentrt-Task
// trailer names, to the newest commit naming it: the agent runtime records
// when it first sees an accepted result there (TD-383). A history cut off
// by maxPages is read as far as it goes.
func MainTasks(ctx context.Context, acc Access, client *http.Client, repo string, since time.Time, maxPages int) (map[string]string, error) {
	g := &gitea{acc: acc, client: client}
	commits, err := g.commits(ctx, repo, "main", since, maxPages, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range commits { // newest first
		if _, t := parseMessage(c.Msg); t.task != "" {
			if _, seen := out[t.task]; !seen {
				out[t.task] = c.SHA
			}
		}
	}
	return out, nil
}
