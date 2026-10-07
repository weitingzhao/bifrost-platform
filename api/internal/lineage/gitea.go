package lineage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Access is the Gitea base URL, org and basic auth (delivery.GiteaAccess, adapted by the server).
type Access struct {
	Base, Org, User, Pass string
}

// AccessFunc resolves Access per refresh, so rotated credentials are picked up.
type AccessFunc func(ctx context.Context) (Access, error)

type giteaCommit struct {
	SHA     string
	Msg     string
	At      time.Time
	Parents []string
}

type giteaRepo struct {
	Name, Default string
	// Mirror is a pull mirror; MirrorUpdated is when Gitea last fetched it.
	Mirror        bool
	MirrorUpdated time.Time
}

type giteaBranch struct {
	Name string
	SHA  string
	At   time.Time
}

type gitea struct {
	acc    Access
	client *http.Client
}

const pageLimit = 50

func (g *gitea) get(ctx context.Context, path string, q url.Values, out any) error {
	u := fmt.Sprintf("%s/api/v1%s", g.acc.Base, path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if g.acc.User != "" && g.acc.Pass != "" {
		req.SetBasicAuth(g.acc.User, g.acc.Pass)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// mirrorSync asks Gitea to fetch a pull mirror from its upstream now. Gitea
// queues the fetch and answers at once; MirrorUpdated moves when it is done.
func (g *gitea) mirrorSync(ctx context.Context, repo string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1%s/mirror-sync", g.acc.Base, g.repoPath(repo)), nil)
	if err != nil {
		return err
	}
	if g.acc.User != "" && g.acc.Pass != "" {
		req.SetBasicAuth(g.acc.User, g.acc.Pass)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("mirror-sync %s: status %d: %s", repo, resp.StatusCode, string(body))
	}
	return nil
}

// repos lists the org's non-archived, non-empty repos with their default branch.
func (g *gitea) repos(ctx context.Context) ([]giteaRepo, error) {
	var out []giteaRepo
	for page := 1; page <= 10; page++ {
		var raw []struct {
			Name          string    `json:"name"`
			DefaultBranch string    `json:"default_branch"`
			Archived      bool      `json:"archived"`
			Empty         bool      `json:"empty"`
			Mirror        bool      `json:"mirror"`
			MirrorUpdated time.Time `json:"mirror_updated"`
		}
		q := url.Values{"limit": {fmt.Sprint(pageLimit)}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, "/orgs/"+url.PathEscape(g.acc.Org)+"/repos", q, &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if !r.Archived && !r.Empty {
				def := r.DefaultBranch
				if def == "" {
					def = "main"
				}
				out = append(out, giteaRepo{Name: r.Name, Default: def, Mirror: r.Mirror, MirrorUpdated: r.MirrorUpdated})
			}
		}
		if len(raw) < pageLimit {
			break
		}
	}
	return out, nil
}

// branches lists a repo's branches with their head commit time.
func (g *gitea) branches(ctx context.Context, repo string) ([]giteaBranch, error) {
	var out []giteaBranch
	for page := 1; page <= 10; page++ {
		var raw []struct {
			Name   string `json:"name"`
			Commit struct {
				ID        string    `json:"id"`
				Timestamp time.Time `json:"timestamp"`
			} `json:"commit"`
		}
		q := url.Values{"limit": {fmt.Sprint(pageLimit)}, "page": {fmt.Sprint(page)}}
		if err := g.get(ctx, g.repoPath(repo)+"/branches", q, &raw); err != nil {
			return nil, err
		}
		for _, b := range raw {
			out = append(out, giteaBranch{Name: b.Name, SHA: b.Commit.ID, At: b.Commit.Timestamp})
		}
		if len(raw) < pageLimit {
			break
		}
	}
	return out, nil
}

// commits walks ref's history newest first and stops at the first commit older
// than since, at a commit for which stop returns true, or after maxPages pages.
func (g *gitea) commits(ctx context.Context, repo, ref string, since time.Time, maxPages int, stop func(sha string) bool) ([]giteaCommit, error) {
	var out []giteaCommit
	for page := 1; page <= maxPages; page++ {
		var raw []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message   string `json:"message"`
				Committer struct {
					Date time.Time `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		q := url.Values{
			"sha": {ref}, "limit": {fmt.Sprint(pageLimit)}, "page": {fmt.Sprint(page)},
			"stat": {"false"}, "verification": {"false"}, "files": {"false"},
		}
		if err := g.get(ctx, g.repoPath(repo)+"/commits", q, &raw); err != nil {
			return out, err
		}
		for _, c := range raw {
			if c.Commit.Committer.Date.Before(since) || (stop != nil && stop(c.SHA)) {
				return out, nil
			}
			gc := giteaCommit{SHA: c.SHA, Msg: c.Commit.Message, At: c.Commit.Committer.Date}
			for _, p := range c.Parents {
				gc.Parents = append(gc.Parents, p.SHA)
			}
			out = append(out, gc)
		}
		if len(raw) < pageLimit {
			break
		}
	}
	return out, nil
}

func (g *gitea) repoPath(repo string) string {
	return "/repos/" + url.PathEscape(g.acc.Org) + "/" + url.PathEscape(repo)
}
