package landing

import (
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type LandingRepo = rules.LandingRepository

// One closed registry, also available to the lower-level rule parser.
var LandingRepos = rules.LandingRepositories

func LandingRepoNames() []string     { return rules.LandingRepoNames() }
func RepoSupported(repo string) bool { return rules.LandingRepoSupported(repo) }
func RepoStream(stream string) bool {
	repo, ok := strings.CutPrefix(stream, "landing:")
	return ok && RepoSupported(repo)
}

// LiveRepos respects presence, including an explicitly empty new setting.
// The legacy switch opts in only platform and only when the new one is absent.
func LiveRepos(lookup func(string) (string, bool)) []string {
	value, set := lookup("AGENTRT_LANDING_LIVE_REPOS")
	if !set {
		if old, _ := lookup("AGENTRT_LANDING_LIVE"); old == "on" {
			return []string{"bifrost-platform"}
		}
		return nil
	}
	wanted := map[string]bool{}
	for _, repo := range strings.Split(value, ",") {
		wanted[strings.TrimSpace(repo)] = true
	}
	var out []string
	for _, repo := range LandingRepoNames() {
		if wanted[repo] {
			out = append(out, repo)
		}
	}
	return out
}

// Repository keeps the original platform-only callers source-compatible.
// Production multi-repository callers always pass a registry key explicitly.
func Repository(repos ...string) string {
	if len(repos) == 0 {
		return "bifrost-platform"
	}
	if len(repos) != 1 || !RepoSupported(repos[0]) {
		return ""
	}
	return repos[0]
}
