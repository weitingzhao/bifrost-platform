package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// The commit reader a pinned task's result is checked with (W-31 round 4,
// core block C-2; DESIGN-coordinator-landing-v3 §2.7): its parents and
// message, and the paths and lines of its diff, as Gitea has them in the
// organization agent branches go to (AGENTRT_GIT_ORG). Without it a pinned
// result waits (store.ErrNoHead). A diff it cannot read whole, or a path it
// cannot read for sure (a quoted name, a header it does not know), is an
// error: the result waits rather than be judged on part of it.

// maxCommitDiff bounds the diff read (a pinned result is one reviewed
// commit).
const maxCommitDiff = 32 << 20

func (s *Server) agentRuntimeCommits() store.CommitReader {
	org := strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))
	if org == "" || s.delivery == nil {
		return nil
	}
	return giteaCommits(s.delivery.GiteaAccess, org, &http.Client{Timeout: 20 * time.Second})
}

func giteaCommits(access func(context.Context) (delivery.GiteaAccess, error), org string, client *http.Client) store.CommitReader {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &c
	return func(ctx context.Context, repo, sha string) (store.CommitFacts, error) {
		if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(sha) {
			return store.CommitFacts{}, fmt.Errorf("%w: %q of %q is not a commit of a repository", store.ErrState, sha, repo)
		}
		acc, err := access(ctx)
		if err != nil {
			return store.CommitFacts{}, fmt.Errorf("gitea access: %w", err)
		}
		base := strings.TrimRight(acc.Base, "/") + "/api/v1/repos/" + url.PathEscape(org) + "/" + url.PathEscape(repo) + "/git/commits/" + sha
		get := func(u string, limit int64) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return nil, err
			}
			if acc.User != "" || acc.Pass != "" {
				req.SetBasicAuth(acc.User, acc.Pass)
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("gitea: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("gitea: %s/%s commit %s: HTTP %d", org, repo, sha, resp.StatusCode)
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
			if err != nil {
				return nil, fmt.Errorf("gitea: %s/%s commit %s: %w", org, repo, sha, err)
			}
			if int64(len(body)) > limit {
				return nil, fmt.Errorf("gitea: %s/%s commit %s: more than %d bytes", org, repo, sha, limit)
			}
			return body, nil
		}
		meta, err := get(base+"?stat=false&verification=false&files=false", 1<<20)
		if err != nil {
			return store.CommitFacts{}, err
		}
		var doc struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		if err = json.Unmarshal(meta, &doc); err != nil || doc.SHA != sha {
			return store.CommitFacts{}, fmt.Errorf("gitea: %s/%s commit %s: not that commit (%v)", org, repo, sha, err)
		}
		facts := store.CommitFacts{Message: doc.Commit.Message}
		for _, p := range doc.Parents {
			facts.Parents = append(facts.Parents, p.SHA)
		}
		diff, err := get(base+".diff", maxCommitDiff)
		if err != nil {
			return store.CommitFacts{}, err
		}
		if facts.Paths, facts.Lines, err = diffFacts(diff); err != nil {
			return store.CommitFacts{}, fmt.Errorf("gitea: %s/%s commit %s: %w", org, repo, sha, err)
		}
		return facts, nil
	}
}

// diffFacts reads a git diff: every path it names (both sides of a rename
// or a copy) and the lines its hunks add or delete. A header it cannot read
// for sure is an error.
func diffFacts(diff []byte) ([]string, int, error) {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) error {
		if p == "" || strings.HasPrefix(p, "\"") {
			return fmt.Errorf("a path it cannot read for sure: %q", p)
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
		return nil
	}
	lines := 0
	inHunk := false
	var pending string // a header's path when both sides are the same name
	var named bool     // the current file's paths were taken
	flush := func() error {
		if pending == "" && !named {
			return nil
		}
		if !named {
			return errors.New("a diff header it cannot read")
		}
		return nil
	}
	sc := bufio.NewScanner(bytes.NewReader(diff))
	sc.Buffer(make([]byte, 1<<20), maxCommitDiff)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "diff --git "):
			if err := flush(); err != nil {
				return nil, 0, err
			}
			inHunk, pending, named = false, "", false
			rest := strings.TrimPrefix(l, "diff --git ")
			if strings.HasPrefix(rest, "\"") {
				return nil, 0, fmt.Errorf("a quoted path: %q", l)
			}
			// a/P b/P: both sides the same name, the line split in its middle.
			if n := len(rest); n%2 == 1 && strings.HasPrefix(rest, "a/") {
				half := rest[:n/2]
				if " "+strings.Replace(half, "a/", "b/", 1) == rest[n/2:] {
					pending = strings.TrimPrefix(half, "a/")
				}
			}
			if pending == "" {
				pending = "?" // named only by rename or copy lines
			} else {
				if err := add(pending); err != nil {
					return nil, 0, err
				}
				named = true
			}
		case !inHunk && (strings.HasPrefix(l, "rename from ") || strings.HasPrefix(l, "copy from ")):
			if err := add(strings.TrimPrefix(strings.TrimPrefix(l, "rename from "), "copy from ")); err != nil {
				return nil, 0, err
			}
		case !inHunk && (strings.HasPrefix(l, "rename to ") || strings.HasPrefix(l, "copy to ")):
			if err := add(strings.TrimPrefix(strings.TrimPrefix(l, "rename to "), "copy to ")); err != nil {
				return nil, 0, err
			}
			named = true
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case inHunk && (strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-")):
			lines++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	if err := flush(); err != nil {
		return nil, 0, err
	}
	return paths, lines, nil
}
