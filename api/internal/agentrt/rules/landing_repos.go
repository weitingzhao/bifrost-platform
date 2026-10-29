package rules

import (
	"regexp"
	"slices"
)

// LandingRepository is the closed transport/policy registry. It lives below
// landing so rule parsing and landing eligibility share it without a cycle.
type LandingRepository struct {
	Name, Remote, CIPipeline, Short string
	CINamePattern                   *regexp.Regexp
	CIResults                       map[string]string
	Siblings                        []string
	GrantSupported                  bool
}

var LandingRepositories = map[string]LandingRepository{
	"bifrost-platform": {
		Name: "bifrost-platform", Short: "platform",
		Remote:     "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git",
		CIPipeline: "bifrost-ci-platform", CINamePattern: regexp.MustCompile(`^landing-ci-[0-9a-f]{24}-[12]$`),
		CIResults: map[string]string{"revision": "platform-commit", "ui": "ui-commit", "infra": "infra-commit"},
		Siblings:  []string{"bifrost-ui"}, GrantSupported: true,
	},
	"bifrost-trade-infra": {
		Name: "bifrost-trade-infra", Short: "infra",
		Remote:     "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-trade-infra.git",
		CIPipeline: "bifrost-ci-infra", CINamePattern: regexp.MustCompile(`^landing-ci-infra-[0-9a-f]{24}-[1-9][0-9]*$`),
		CIResults: map[string]string{"revision": "infra-commit", "base": "base-commit", "platform": "platform-commit", "tools": "tools-from", "policy": "doc-policy", "purpose": "purpose"},
		Siblings:  []string{"bifrost-platform"}, GrantSupported: false,
	},
}

func LandingRepoNames() []string {
	names := make([]string, 0, len(LandingRepositories))
	for name := range LandingRepositories {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func LandingRepoSupported(repo string) bool { _, ok := LandingRepositories[repo]; return ok }
