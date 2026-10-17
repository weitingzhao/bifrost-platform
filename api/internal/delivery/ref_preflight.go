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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var shaRefPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// repoRefStatus probes Gitea for the given ref in a single repo, checking
// full lowercase SHAs as commits; other refs use branches, tags and the legacy
// abbreviated-SHA lookup.
// Kind "error" means the probe could not reach Gitea (network/auth) — the ref's
// presence is unknown and must NOT be treated as a genuine "missing".
func (s *Service) repoRefStatus(ctx context.Context, repo, ref, user, pass string) RepoRefStatus {
	fullSHA := callerParamSHA.MatchString(ref)
	if fullSHA {
		if sha, exists, err := s.giteaCommitExists(ctx, repo, ref, user, pass); exists {
			return RepoRefStatus{Repo: repo, Exists: true, Kind: "commit", Commit: sha}
		} else if err != nil {
			return RepoRefStatus{Repo: repo, Kind: "error", Detail: err.Error()}
		}
	}
	branches, branchErr := s.fetchGiteaBranches(ctx, repo, user, pass)
	if branchErr == nil {
		for _, b := range branches {
			if !fullSHA && b.Name == ref {
				return RepoRefStatus{Repo: repo, Exists: true, Kind: "branch", Commit: b.Commit}
			}
		}
	}

	tags, tagErr := s.fetchGiteaTags(ctx, repo, user, pass)
	if tagErr == nil {
		for _, t := range tags {
			if !fullSHA && t.Name == ref {
				return RepoRefStatus{Repo: repo, Exists: true, Kind: "tag", Commit: t.Commit}
			}
		}
	}

	if !fullSHA && shaRefPattern.MatchString(ref) {
		if sha, ok, err := s.giteaCommitExists(ctx, repo, ref, user, pass); ok {
			return RepoRefStatus{Repo: repo, Exists: true, Kind: "commit", Commit: sha}
		} else if err != nil {
			return RepoRefStatus{Repo: repo, Kind: "error", Detail: err.Error()}
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

// Preserve main 26a10b5's listing order and commit-error semantics outside
// platform delivery, including CI. In particular, a commit lookup error alone is missing.
func (s *Service) legacyRepoRefStatus(ctx context.Context, repo, ref, user, pass string) RepoRefStatus {
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
		if sha, ok, _ := s.giteaCommitExists(ctx, repo, ref, user, pass); ok {
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
func (s *Service) giteaCommitExists(ctx context.Context, repo, sha, user, pass string) (string, bool, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/git/commits/%s", giteaBaseURL(), giteaOrg, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false, err
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return sha, true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	return "", false, fmt.Errorf("commit lookup status %d", resp.StatusCode)
}

// RefPreflight checks whether the chosen revision exists in every repo the
// pipeline clones. It is the gate that prevents multi-repo deploys from failing
// halfway at a clone-* task.
func (s *Service) RefPreflight(ctx context.Context, pipelineName, revision string) RefPreflightResponse {
	if pipelineName == "bifrost-ci-python" {
		return RefPreflightResponse{ClusterID: s.clusterID(), Pipeline: pipelineName, Revision: revision,
			Repos: []RepoRefStatus{}, Missing: []string{}, GeneratedAt: time.Now().UTC(),
			Reachability: probe.ReachFail, Detail: "python-ci requires param repo to select its repository"}
	}
	return s.refPreflightForRepos(ctx, pipelineName, revision, reposForPipeline(pipelineName))
}

func (s *Service) refPreflightForRepos(ctx context.Context, pipelineName, revision string, repos []string) RefPreflightResponse {
	refs := map[string]string{}
	for _, repo := range repos {
		refs[repo] = revision
	}
	return s.refPreflightAtRefs(ctx, pipelineName, revision, repos, refs)
}

func (s *Service) refPreflightAtRefs(ctx context.Context, pipelineName, revision string, repos []string, refs map[string]string) RefPreflightResponse {
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
		ref := refs[repo]
		if ref == "" {
			ref = rev
		}
		var st RepoRefStatus
		if isPlatformDelivery(pipelineName) {
			st = s.repoRefStatus(ctx, repo, ref, user, pass)
		} else {
			st = s.legacyRepoRefStatus(ctx, repo, ref, user, pass)
		}
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

// Read the already merged run values, then declaration defaults for parameters
// omitted from the run. Never re-merge caller input or change the built object.
func runRepoRefs(name, revision string, spec map[string]any, pipe *unstructured.Unstructured, repos []string) map[string]string {
	values := map[string]string{}
	if params, ok := spec["params"].([]map[string]any); ok {
		for _, param := range params {
			key, _ := param["name"].(string)
			value, _ := param["value"].(string)
			values[key] = value
		}
	}
	defaults, _, _ := unstructured.NestedSlice(pipe.Object, "spec", "params")
	for _, raw := range defaults {
		param, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := param["name"].(string)
		if _, supplied := values[key]; !supplied {
			if value, ok := param["default"].(string); ok {
				values[key] = value
			}
		}
	}
	refs := map[string]string{}
	for _, repo := range repos {
		ref := revision
		if key := pipelineRepoRefParams[name][repo]; key != "" {
			if value, exists := values[key]; exists {
				ref = value
			}
		}
		refs[repo] = ref
	}
	return refs
}

func missingRunRefs(pf RefPreflightResponse, refs map[string]string) string {
	parts := make([]string, 0, len(pf.Missing))
	for _, repo := range pf.Missing {
		parts = append(parts, fmt.Sprintf("%s (ref %q)", repo, refs[repo]))
	}
	return "revision is missing in: " + strings.Join(parts, ", ")
}

// Legacy delivery checks intentionally ignore run params and never sync.
func (s *Service) legacyRunRefPreflight(ctx context.Context, name, revision string) string {
	pf := s.refPreflightForRepos(ctx, name, revision, reposForPipeline(name))
	if pf.Reachability == probe.ReachOK && len(pf.Missing) > 0 {
		return fmt.Sprintf("revision %q is missing in: %s — create the same branch/tag in those repos (or deploy a ref present in all)", revision, strings.Join(pf.Missing, ", "))
	}
	return ""
}

func platformRunInputs(revision string, extra map[string]string) error {
	if value, ok := extra["revision"]; ok && revision != "" && revision != value {
		return fmt.Errorf("revision %q and params.revision %q must match", revision, value)
	}
	refs := map[string]string{"revision": revision}
	for key, value := range extra {
		if callerMayOverride(strings.TrimSpace(key)) {
			refs[key] = value
		}
	}
	for _, key := range sortedKeysString(refs) {
		value := strings.TrimSpace(refs[key])
		if shaRefPattern.MatchString(value) && strings.ContainsAny(value, "abcdefABCDEF") && !callerParamSHA.MatchString(value) {
			return fmt.Errorf("%s %q: use the full 40-character lowercase SHA", key, value)
		}
	}
	return nil
}

func sortedKeysString(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Service) runRefPreflight(ctx context.Context, name, revision, ciRepo string, spec map[string]any, pipe *unstructured.Unstructured) string {
	repos := reposForPipeline(name)
	if ciRepo != "" {
		repos = []string{ciRepo}
	}
	refs := runRepoRefs(name, revision, spec, pipe, repos)
	// A platform commit cannot be the UI commit. Reject before any mirror request.
	if isPlatformDelivery(name) && callerParamSHA.MatchString(refs["bifrost-platform"]) && refs["bifrost-ui"] == refs["bifrost-platform"] {
		return fmt.Sprintf("bifrost-ui (ref %q) — pass uiRevision for bifrost-ui", refs["bifrost-ui"])
	}
	pf := s.refPreflightAtRefs(ctx, name, revision, repos, refs)
	if len(pf.Missing) == 0 {
		return ""
	}
	if ciRepo != "" {
		if pf.Reachability != probe.ReachOK {
			return ""
		}
		return fmt.Sprintf("revision %q is missing in: %s — sync this repository's mirror (or, where Gitea is its primary, push it there) before starting CI", revision, ciRepo)
	}
	missing := missingRunRefs(pf, refs)
	originalMissing := missing
	unseen := map[string]bool{}
	for _, repo := range pf.Missing {
		unseen[repo] = true
	}
	// Request one sync per repository, then give branches, tags and commits the
	// same bounded polling. SyncMirrors with no commits only enqueues the fetches.
	syncCtx, cancel := context.WithTimeout(ctx, mirrorSyncWait)
	defer cancel()
	synced, err := s.SyncMirrors(syncCtx, repos, nil)
	if err != nil || !synced.OK || syncCtx.Err() != nil {
		return missing + "; mirror sync did not complete"
	}
	for {
		if syncCtx.Err() != nil {
			return originalMissing + "; mirror was synced but the ref is still not present"
		}
		retry := s.refPreflightAtRefs(syncCtx, name, revision, repos, refs)
		if syncCtx.Err() != nil {
			return originalMissing + "; mirror was synced but the ref is still not present"
		}
		for _, status := range retry.Repos {
			if status.Exists {
				delete(unseen, status.Repo)
			}
		}
		if len(unseen) == 0 && len(retry.Missing) == 0 {
			return ""
		}
		select {
		case <-syncCtx.Done():
			return originalMissing + "; mirror was synced but the ref is still not present"
		case <-time.After(2 * time.Second):
		}
	}
}
