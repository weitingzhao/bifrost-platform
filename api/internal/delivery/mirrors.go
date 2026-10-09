package delivery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

const mirrorSyncWait = 90 * time.Second

// MirrorRepoResult is one repository after a sync request.
type MirrorRepoResult struct {
	Repo          string `json:"repo"`
	SyncRequested bool   `json:"sync_requested"`
	Commit        string `json:"commit,omitempty"`
	Present       bool   `json:"present"`
	Error         string `json:"error,omitempty"`
}

// MirrorSyncResult is the body of POST /api/v1/delivery/mirrors/sync.
type MirrorSyncResult struct {
	OK    bool               `json:"ok"`
	Repos []MirrorRepoResult `json:"repos"`
}

// SetPolicy installs the allow-list. A nil policy rejects every sync.
func (s *Service) SetPolicy(p *actuationpolicy.Policy) { s.policy = p }

// SetPolicy installs the allow-list used by mirror sync.
func (h *Handler) SetPolicy(p *actuationpolicy.Policy) { h.svc.SetPolicy(p) }

// SyncOne fetches one repository and waits for commit. Handler method so the
// plan path and the HTTP path share one implementation.
func (h *Handler) SyncOne(ctx context.Context, repo, commit string) (MirrorRepoResult, error) {
	res, err := h.svc.SyncMirrors(ctx, []string{repo}, map[string]string{repo: commit})
	if err != nil {
		return MirrorRepoResult{}, err
	}
	if len(res.Repos) == 0 {
		return MirrorRepoResult{}, fmt.Errorf("mirror sync returned no repo")
	}
	return res.Repos[0], nil
}

// SyncMirrors asks Gitea to fetch each repo, then polls git/commits/<sha>
// until the commit is present or 90 seconds pass.
func (s *Service) SyncMirrors(ctx context.Context, repos []string, commits map[string]string) (MirrorSyncResult, error) {
	var out MirrorSyncResult
	if len(repos) == 0 {
		return out, fmt.Errorf("repos is required")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(repos))
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		if s.policy == nil {
			return out, fmt.Errorf("repo is not in the mirror allow-list")
		}
		if err := s.policy.MirrorAllowed(repo); err != nil {
			return out, err
		}
		clean = append(clean, repo)
	}
	if len(clean) == 0 {
		return out, fmt.Errorf("repos is required")
	}
	for name := range commits {
		if !seen[strings.TrimSpace(name)] {
			return out, fmt.Errorf("commit for %s has no matching repo", name)
		}
	}
	user, pass, err := s.giteaAuth(ctx)
	if err != nil {
		return out, err
	}
	deadline := time.Now().Add(mirrorSyncWait)
	out.OK = true
	for _, repo := range clean {
		item := s.syncOneRepo(ctx, repo, strings.TrimSpace(commits[repo]), user, pass, deadline)
		if !item.Present {
			out.OK = false
		}
		out.Repos = append(out.Repos, item)
	}
	return out, nil
}

func (s *Service) giteaAuth(ctx context.Context) (string, string, error) {
	if s.mirrorCreds != nil {
		return s.mirrorCreds(ctx)
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return "", "", err
	}
	user, pass := s.giteaCredentials(ctx, clientset, s.PipelinesNamespace())
	return user, pass, nil
}

func (s *Service) giteaRoot() string {
	if strings.TrimSpace(s.giteaBase) != "" {
		return strings.TrimRight(s.giteaBase, "/")
	}
	return giteaBaseURL()
}

func (s *Service) syncOneRepo(ctx context.Context, repo, commit, user, pass string, deadline time.Time) MirrorRepoResult {
	item := MirrorRepoResult{Repo: repo, Commit: commit}
	if err := s.postMirrorSync(ctx, repo, user, pass); err != nil {
		item.Error = err.Error()
		return item
	}
	item.SyncRequested = true
	if commit == "" {
		item.Present = true
		return item
	}
	if !isFullGitSHA(commit) {
		item.Error = "commit must be a 40-character hex sha"
		return item
	}
	for {
		if s.commitPresent(ctx, repo, commit, user, pass) {
			item.Present = true
			return item
		}
		if !time.Now().Before(deadline) {
			item.Error = "commit was not present within 90s"
			return item
		}
		select {
		case <-ctx.Done():
			item.Error = ctx.Err().Error()
			return item
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Service) postMirrorSync(ctx context.Context, repo, user, pass string) error {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/mirror-sync", s.giteaRoot(), giteaOrg, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("mirror-sync status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *Service) commitPresent(ctx context.Context, repo, sha, user, pass string) bool {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/git/commits/%s", s.giteaRoot(), giteaOrg, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.StatusCode == http.StatusOK
}
