package delivery

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var shaRefPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// repoRefStatus probes Gitea for the given ref in a single repo, checking
// branches, then tags, then (for SHA-looking refs) a direct commit lookup.
// Kind "error" means the probe could not reach Gitea (network/auth) — the ref's
// presence is unknown and must NOT be treated as a genuine "missing".
func (s *Service) repoRefStatus(ctx context.Context, repo, ref, user, pass string) RepoRefStatus {
	branches, branchErr := s.fetchGiteaBranches(ctx, repo, user, pass)
	if branchErr == nil {
		for _, b := range branches {
			if b.Name == ref {
				return RepoRefStatus{Repo: repo, Exists: true, Kind: "branch", Commit: b.Commit}
			}
		}
	}

	tags, tagErr := s.fetchGiteaTags(ctx, repo, user, pass)
	if tagErr == nil {
		for _, t := range tags {
			if t.Name == ref {
				return RepoRefStatus{Repo: repo, Exists: true, Kind: "tag", Commit: t.Commit}
			}
		}
	}

	if shaRefPattern.MatchString(ref) {
		if sha, ok := s.giteaCommitExists(ctx, repo, ref, user, pass); ok {
			return RepoRefStatus{Repo: repo, Exists: true, Kind: "commit", Commit: sha}
		}
	}

	// Could not confirm presence. If either listing failed we don't have a full
	// view, so report "error" (unknown) rather than a false "missing".
	if branchErr != nil || tagErr != nil {
		detail := ""
		if branchErr != nil {
			detail = "branches: " + branchErr.Error()
		} else if tagErr != nil {
			detail = "tags: " + tagErr.Error()
		}
		return RepoRefStatus{Repo: repo, Exists: false, Kind: "error", Detail: detail}
	}

	return RepoRefStatus{Repo: repo, Exists: false, Kind: "missing"}
}

// giteaCommitExists checks whether a commit SHA resolves in the repo.
func (s *Service) giteaCommitExists(ctx context.Context, repo, sha, user, pass string) (string, bool) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/git/commits/%s", giteaBaseURL(), giteaOrg, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return sha, true
	}
	return "", false
}

// RefPreflight checks whether the chosen revision exists in every repo the
// pipeline clones. It is the gate that prevents multi-repo deploys from failing
// halfway at a clone-* task.
func (s *Service) RefPreflight(ctx context.Context, pipelineName, revision string) RefPreflightResponse {
	now := time.Now().UTC()
	rev := strings.TrimSpace(revision)
	if rev == "" {
		rev = "main"
	}
	out := RefPreflightResponse{
		ClusterID:    s.clusterID(),
		Pipeline:     pipelineName,
		Revision:     rev,
		Repos:        []RepoRefStatus{},
		Missing:      []string{},
		Ready:        false,
		Reachability: probe.ReachFail,
		GeneratedAt:  now,
	}

	if err := validateRevision(rev); err != nil {
		out.Detail = err.Error()
		return out
	}

	repos := reposForPipeline(pipelineName)
	ns := s.PipelinesNamespace()

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		out.Detail = "cluster client: " + err.Error()
		return out
	}
	user, pass := s.giteaCredentials(ctx, clientset, ns)

	missing := []string{}
	errored := []string{}
	statuses := make([]RepoRefStatus, 0, len(repos))
	for _, repo := range repos {
		st := s.repoRefStatus(ctx, repo, rev, user, pass)
		statuses = append(statuses, st)
		switch {
		case st.Kind == "error":
			errored = append(errored, repo)
		case !st.Exists:
			missing = append(missing, repo)
		}
	}
	sort.Strings(missing)
	sort.Strings(errored)

	out.Repos = statuses
	out.Missing = missing

	// Reachability reflects probe completeness, NOT ref presence:
	//   ok       — every repo checked successfully (missing list is authoritative)
	//   degraded — some repos could not be probed
	//   fail     — no repo could be probed (do not hard-block on this)
	switch {
	case len(errored) == len(repos):
		out.Reachability = probe.ReachFail
	case len(errored) > 0:
		out.Reachability = probe.ReachDegraded
	default:
		out.Reachability = probe.ReachOK
	}

	// Ready only when we have full visibility and nothing is missing.
	out.Ready = out.Reachability == probe.ReachOK && len(missing) == 0

	switch {
	case len(errored) == len(repos):
		out.Detail = fmt.Sprintf("could not probe any repo: %s", strings.Join(errored, ", "))
	case len(missing) > 0 && len(errored) > 0:
		out.Detail = fmt.Sprintf("%q missing in: %s; unverified: %s",
			rev, strings.Join(missing, ", "), strings.Join(errored, ", "))
	case len(missing) > 0:
		out.Detail = fmt.Sprintf("%q missing in: %s", rev, strings.Join(missing, ", "))
	case len(errored) > 0:
		out.Detail = fmt.Sprintf("%q present where checked; unverified: %s", rev, strings.Join(errored, ", "))
	default:
		out.Detail = fmt.Sprintf("%q present in all %d repo(s)", rev, len(repos))
	}
	return out
}
