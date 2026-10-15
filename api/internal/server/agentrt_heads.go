package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// The result check's Gitea half (design v5 §5.2: a result is persisted once
// its branch is on Gitea and the platform has checked it). Agent branches go
// to their own organization on Gitea, AGENTRT_GIT_ORG: the organization the
// releases clone from mirrors GitHub, takes no push and drops branches GitHub
// does not have. Unset, there is no reader and a pushed branch answers 503
// (store.ErrNoHead), as before.

// agentRuntimeHeads is the branch reader of this process, or nil.
func (s *Server) agentRuntimeHeads() store.BranchHead {
	org := strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))
	if org == "" || s.delivery == nil {
		return nil
	}
	return giteaBranchHead(s.delivery.GiteaAccess, org, &http.Client{Timeout: 10 * time.Second})
}

var (
	giteaName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	commitID  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// giteaBranchHead reads the commit a branch of <org>/<repo> points at. A
// branch that is not there, in a repository that is, is a refused result
// (store.ErrState); a repository it cannot see (a wrong organization, a
// credential that does not reach it) or a Gitea it cannot reach is an error
// of the platform, which agentd retries later, so no result is lost to it.
func giteaBranchHead(access func(context.Context) (delivery.GiteaAccess, error), org string, client *http.Client) store.BranchHead {
	// A redirect would read another repository or branch than the one asked
	// for: it is an answer of its own, an error of the platform (review 194 A2).
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &c
	return func(ctx context.Context, repo, ref string) (string, error) {
		branch, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || branch == "" || !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") {
			return "", fmt.Errorf("%w: %q of %q is not a branch of a repository", store.ErrState, ref, repo)
		}
		segments := strings.Split(branch, "/")
		for i, seg := range segments {
			if seg == "" || seg == "." || seg == ".." {
				return "", fmt.Errorf("%w: %q is not a branch name", store.ErrState, ref)
			}
			segments[i] = url.PathEscape(seg)
		}
		acc, err := access(ctx)
		if err != nil {
			return "", fmt.Errorf("gitea access: %w", err)
		}
		repoURL := strings.TrimRight(acc.Base, "/") + "/api/v1/repos/" + url.PathEscape(org) + "/" + url.PathEscape(repo)
		var b struct {
			Name   string `json:"name"`
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		status, err := giteaGet(ctx, client, acc, repoURL+"/branches/"+strings.Join(segments, "/"), &b)
		if err != nil {
			return "", err
		}
		switch status {
		case http.StatusOK:
			if b.Name != branch || !commitID.MatchString(b.Commit.ID) {
				return "", fmt.Errorf("gitea: %s/%s %s: the answer is not that branch with a commit id", org, repo, ref)
			}
			return b.Commit.ID, nil
		case http.StatusNotFound:
			// Gitea answers 404 for a branch that is not there and for a
			// repository it does not show this credential: tell them apart.
			// Only that repository, read and named, makes the result refused
			// (review 194 A1).
			var r struct {
				FullName string `json:"full_name"`
			}
			rs, err := giteaGet(ctx, client, acc, repoURL, &r)
			if err != nil {
				return "", err
			}
			if rs != http.StatusOK || !strings.EqualFold(r.FullName, org+"/"+repo) {
				return "", fmt.Errorf("gitea: repository %s/%s: HTTP %d, %q", org, repo, rs, r.FullName)
			}
			return "", fmt.Errorf("%w: %s is not on %s/%s", store.ErrState, ref, org, repo)
		}
		return "", fmt.Errorf("gitea: %s/%s %s: HTTP %d", org, repo, ref, status)
	}
}

// giteaGet sends one GET and decodes a 200's body into out (nil: ignore it).
func giteaGet(ctx context.Context, client *http.Client, acc delivery.GiteaAccess, u string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	if acc.User != "" || acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gitea: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("gitea: %w", err)
	}
	if resp.StatusCode == http.StatusOK && out != nil {
		if err = json.Unmarshal(body, out); err != nil {
			return 0, fmt.Errorf("gitea: %w", errors.Join(errors.New("unreadable answer"), err))
		}
	}
	return resp.StatusCode, nil
}
