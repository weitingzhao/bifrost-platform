package landing

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type LandingRepo = rules.LandingRepository

// One closed registry, also available to the lower-level rule parser.
var LandingRepos = rules.LandingRepositories

func RepoSupported(repo string) bool { return rules.LandingRepoSupported(repo) }
