package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

// What changed between two commits, as the release policy reads it.
//
// The mirror is Gitea 1.21. Its API has no compare route: GET
// /repos/{owner}/{repo}/compare/{base}...{head} answers the router's own
// plain-text "404 page not found", so the policy could never read a diff and
// covered no release that needed one (2026-11-12). The commit list is there,
// and it takes not= (git log <to> --not <from>) and files=: this reader walks
// the commits the new side has and the old side lacks and unions the files
// each one touched.
//
// The union is a superset of the net diff (a file changed and changed back is
// still listed; a merge commit lists what it brought in against its first
// parent), which errs toward asking the Owner. Whatever cannot be established
// is an error, never an empty list: the old side must be an ancestor of the
// new one, every commit must carry its file list, and the walk must see as
// many commits as the mirror says there are.
//
// This file is on the policy's trust-anchor path list
// (api/internal/delivery/release_policy*.go) on purpose: the policy's path
// rules are only as good as this list.

const (
	// changedFilesPageSize is Gitea's own cap on a page of commits
	// ([git] COMMITS_RANGE_SIZE, 50 unless configured).
	changedFilesPageSize = 50
	// defaultChangedFilesMaxCommits bounds one walk. A release that far from
	// the last one is the Owner's to look at.
	defaultChangedFilesMaxCommits = 500
	changedFilesMaxBody           = 16 << 20
)

// changedFilesMaxCommits is PLATFORM_POLICY_DIFF_MAX_COMMITS, or the default.
func changedFilesMaxCommits() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("PLATFORM_POLICY_DIFF_MAX_COMMITS"))); err == nil && n > 0 {
		return n
	}
	return defaultChangedFilesMaxCommits
}

type giteaListedCommit struct {
	SHA string `json:"sha"`
	// Files is a pointer so a commit listed without its files (files=false,
	// or a server that does not know the parameter) is told from one that
	// touched nothing.
	Files *[]struct {
		Filename string `json:"filename"`
	} `json:"files"`
}

// listGiteaCommits is one page of git log <sha> --not <not>, newest first,
// each commit with the files it touched. total is the mirror's count of the
// whole range, -1 when it sent none.
func (s *Service) listGiteaCommits(ctx context.Context, repo, sha, not string, page, limit int, user, pass string) (commits []giteaListedCommit, total int, err error) {
	q := url.Values{}
	q.Set("sha", sha)
	q.Set("not", not)
	q.Set("stat", "false")
	q.Set("verification", "false")
	q.Set("files", "true")
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", strconv.Itoa(page))
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/commits?%s", giteaBaseURL(), giteaOrg, url.PathEscape(repo), q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, -1, err
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, -1, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, -1, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, changedFilesMaxBody+1))
	if err != nil {
		return nil, -1, err
	}
	if len(body) > changedFilesMaxBody {
		return nil, -1, fmt.Errorf("the commit list is over %d bytes", changedFilesMaxBody)
	}
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, -1, fmt.Errorf("json: %w", err)
	}
	total = -1
	if v := strings.TrimSpace(resp.Header.Get("X-Total-Count")); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n >= 0 {
			total = n
		}
	}
	return commits, total, nil
}

// changedFilesBetween lists the paths touched by the commits to has and from
// lacks, sorted. from must be an ancestor of to.
func (s *Service) changedFilesBetween(ctx context.Context, repo, from, to, user, pass string) ([]string, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if repo == "" || from == "" || to == "" {
		return nil, fmt.Errorf("repo, from and to are required")
	}
	if from == to {
		return []string{}, nil
	}
	// The old side must have nothing the new side lacks. A rollback, or two
	// histories that diverged, has no forward list of commits to read.
	behind, behindTotal, err := s.listGiteaCommits(ctx, repo, from, to, 1, 1, user, pass)
	if err != nil {
		return nil, err
	}
	if len(behind) > 0 || behindTotal > 0 {
		return nil, fmt.Errorf("%s is not an ancestor of %s (a rollback or a diverged history): there is no forward diff to read", shortRef(from), shortRef(to))
	}

	maxCommits := changedFilesMaxCommits()
	seen := map[string]bool{}
	walked, want := 0, -1
	for page := 1; ; page++ {
		commits, total, err := s.listGiteaCommits(ctx, repo, to, from, page, changedFilesPageSize, user, pass)
		if err != nil {
			return nil, err
		}
		if page == 1 {
			want = total
		}
		if len(commits) == 0 {
			break
		}
		for _, c := range commits {
			sha := strings.TrimSpace(c.SHA)
			if sha == "" {
				return nil, fmt.Errorf("the mirror listed a commit without an id")
			}
			if sha == from || strings.HasPrefix(sha, from) && len(from) >= 7 {
				return nil, fmt.Errorf("the mirror listed %s itself: it did not apply not=", shortRef(from))
			}
			if c.Files == nil {
				return nil, fmt.Errorf("the mirror listed %s without its files", shortRef(sha))
			}
			for _, f := range *c.Files {
				if name := strings.TrimSpace(f.Filename); name != "" {
					seen[name] = true
				}
			}
			walked++
			if walked > maxCommits {
				return nil, fmt.Errorf("more than %d commits between %s and %s", maxCommits, shortRef(from), shortRef(to))
			}
		}
		if want >= 0 && walked >= want {
			break
		}
	}
	if walked == 0 {
		return nil, fmt.Errorf("the mirror lists no commits between %s and %s", shortRef(from), shortRef(to))
	}
	if want >= 0 && want != walked {
		return nil, fmt.Errorf("the mirror counts %d commits between %s and %s and listed %d", want, shortRef(from), shortRef(to), walked)
	}
	files := make([]string, 0, len(seen))
	for name := range seen {
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}
