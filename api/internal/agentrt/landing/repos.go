package landing

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type LandingRepo = rules.LandingRepository

// One closed registry, also available to the lower-level rule parser.
var LandingRepos = rules.LandingRepositories

func LandingRepoNames() []string     { return rules.LandingRepoNames() }
func RepoSupported(repo string) bool { return rules.LandingRepoSupported(repo) }

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
