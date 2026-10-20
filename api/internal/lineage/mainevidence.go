package lineage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// What an assessment of a card or an approval reads of the repositories
// (TD-391): a main's commits with the trailers that tie them to work, and a
// file of the governance repository (its ledger). Both read the Gitea mirror
// and write nothing.

// MainCommit is one commit of a main with what ties it to work.
type MainCommit struct {
	SHA, Subject string
	At           time.Time
	ChangeID     string
	Task         string   // Agentrt-Task: the runtime task it is a result of
	Work         []string // Work: the ledger items it names
}

// MainCommits reads repo's main back to since (at most maxPages pages of
// Gitea's commit list), newest first, each with its subject and trailers.
func MainCommits(ctx context.Context, acc Access, client *http.Client, repo string, since time.Time, maxPages int) ([]MainCommit, error) {
	g := &gitea{acc: acc, client: client}
	commits, err := g.commits(ctx, repo, "main", since, maxPages, nil)
	out := make([]MainCommit, 0, len(commits))
	for _, c := range commits {
		subject, t := parseMessage(c.Msg)
		out = append(out, MainCommit{SHA: c.SHA, Subject: subject, At: c.At, ChangeID: t.changeID, Task: t.task, Work: t.work})
	}
	return out, err
}

// maxRawFile bounds a file RawFile reads.
const maxRawFile = 4 << 20

// RawFile reads path in repo at ref through Gitea's raw API, at most 4 MiB.
func RawFile(ctx context.Context, acc Access, client *http.Client, repo, ref, path string) ([]byte, error) {
	g := &gitea{acc: acc, client: client}
	var parts []string
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		parts = append(parts, url.PathEscape(p))
	}
	u := fmt.Sprintf("%s/api/v1%s/raw/%s?ref=%s", acc.Base, g.repoPath(repo), strings.Join(parts, "/"), url.QueryEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if acc.User != "" && acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s/%s: status %d", repo, path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxRawFile+1))
	if err == nil && len(b) > maxRawFile {
		return nil, fmt.Errorf("%s/%s is larger than %d bytes", repo, path, maxRawFile)
	}
	return b, err
}
