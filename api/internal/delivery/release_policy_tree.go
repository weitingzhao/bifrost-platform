package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// The complete difference of two commits, as the release policy reads it
// when the commits' own file lists may be cut short (release_policy_diff.go
// unions what each commit says it touched, and the mirror caps that list a
// commit). The two whole trees are listed, every path with its object id,
// and compared: a path that is in one and not the other, or has another id
// or another kind, changed. That is complete whatever the number of files.
//
// Whatever cannot be established is an error, never a shorter list: a
// listing that does not add up to the count the mirror gives, a count or a
// tree that changes between pages, a page that does not read.
//
// The mirror's "truncated" is not read. Gitea 1.21 sets it on every page of
// a tree with more entries than one page holds, the last page included
// (agentrt_commit_tree.go reads the same route and says the same), so it
// tells nothing about whether the pages together are whole; the count does.
// Read as "cut short" it refused every tree of more than 1,000 entries, which
// is every tree of this repository (2026-11-14, 1c280b40..6bbcb657, 107
// files: the first large release after this reading went live).
//
// This file is on the policy's trust-anchor path list
// (api/internal/delivery/release_policy*.go), as the commit walk is.

const (
	treeListPageSize = 1000
	treeListMaxPages = 200 // 200,000 entries: far past any tree here
	treeListMaxBody  = 32 << 20
)

type giteaTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// treeListing is every entry of commit's tree by path: "<type> <object id>".
func (s *Service) treeListing(ctx context.Context, repo, commit, user, pass string) (map[string]string, error) {
	out := map[string]string{}
	total, root := -1, ""
	for page := 1; page <= treeListMaxPages; page++ {
		endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/git/trees/%s?recursive=true&per_page=%d&page=%d",
			giteaBaseURL(), giteaOrg, url.PathEscape(repo), url.PathEscape(commit), treeListPageSize, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		if user != "" && pass != "" {
			req.SetBasicAuth(user, pass)
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, treeListMaxBody+1))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("the tree of %s: status %d", shortRef(commit), resp.StatusCode)
		}
		if readErr != nil || len(body) > treeListMaxBody {
			return nil, fmt.Errorf("the tree of %s: unreadable or over %d bytes", shortRef(commit), treeListMaxBody)
		}
		var doc struct {
			SHA        string           `json:"sha"`
			Tree       []giteaTreeEntry `json:"tree"`
			TotalCount *int             `json:"total_count"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("the tree of %s: json: %w", shortRef(commit), err)
		}
		if doc.TotalCount == nil || *doc.TotalCount < 0 {
			return nil, fmt.Errorf("the tree of %s came without its entry count", shortRef(commit))
		}
		if total >= 0 && *doc.TotalCount != total {
			return nil, fmt.Errorf("the tree of %s changed its entry count between pages", shortRef(commit))
		}
		total = *doc.TotalCount
		if page == 1 {
			root = doc.SHA
		} else if doc.SHA != root {
			return nil, fmt.Errorf("the tree of %s changed between pages", shortRef(commit))
		}
		for _, e := range doc.Tree {
			path := strings.TrimSpace(e.Path)
			if path == "" || e.SHA == "" || e.Type == "" {
				return nil, fmt.Errorf("the tree of %s has an entry without a path, a kind or an id", shortRef(commit))
			}
			if _, twice := out[path]; twice {
				return nil, fmt.Errorf("the tree of %s lists %s twice", shortRef(commit), path)
			}
			out[path] = e.Type + " " + e.SHA
		}
		if len(out) >= total || len(doc.Tree) == 0 {
			break
		}
	}
	if len(out) != total {
		return nil, fmt.Errorf("the tree of %s has %d entries and %d were listed", shortRef(commit), total, len(out))
	}
	return out, nil
}

// treeFilesBetween lists every file path whose content differs between the
// trees of from and to, sorted: added, removed, or with another object id.
// Directories are not listed (their files are); a submodule is, by its path.
func (s *Service) treeFilesBetween(ctx context.Context, repo, from, to, user, pass string) ([]string, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if repo == "" || from == "" || to == "" {
		return nil, fmt.Errorf("repo, from and to are required")
	}
	if from == to {
		return []string{}, nil
	}
	old, err := s.treeListing(ctx, repo, from, user, pass)
	if err != nil {
		return nil, err
	}
	next, err := s.treeListing(ctx, repo, to, user, pass)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	for path, id := range next {
		if strings.HasPrefix(id, "tree ") {
			continue
		}
		if old[path] != id {
			changed[path] = true
		}
	}
	for path, id := range old {
		if strings.HasPrefix(id, "tree ") {
			continue
		}
		if next[path] != id {
			changed[path] = true
		}
	}
	files := make([]string, 0, len(changed))
	for path := range changed {
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}
