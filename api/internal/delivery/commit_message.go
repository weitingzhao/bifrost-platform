package delivery

import (
	"context"
	"net/url"
)

// CommitMessage is a commit's whole message in repo, read through Gitea; ok
// is false when the commit is not there. Read-only: the bus's subscription
// dispatcher reads the Claude-Session trailer from it to wake the seat that
// made the commit (2026-11-16).
func (f PolicyFacts) CommitMessage(ctx context.Context, repo, sha string) (string, bool, error) {
	var c struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	found, err := f.giteaGet(ctx, url.PathEscape(repo)+"/git/commits/"+url.PathEscape(sha), &c)
	if err != nil || !found {
		return "", found, err
	}
	return c.Commit.Message, true, nil
}
