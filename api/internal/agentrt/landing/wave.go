package landing

import (
	"path"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// WaveExcluded returns the reason a change must keep its individual landing
// path, or empty when it may join a wave. Both endpoints of a rename count.
func WaveExcluded(repo string, changes []rules.LandingChange, urgent bool) string {
	if urgent {
		return "urgent"
	}
	if why, bad := rules.LandingAbsoluteViolation(repo, changes); bad {
		return "absolute: " + why
	}
	for _, c := range changes {
		for _, p := range c.Paths() {
			if rules.FloorEntry(repo, p) != "" {
				return "floor_grant: " + p
			}
			if rules.S5Candidate(repo, p) {
				return "s5: " + p
			}
			for _, dir := range strings.Split(path.Dir(p), "/") {
				switch strings.ToLower(dir) {
				case "secret", "secrets", "permission", "permissions", "rbac", "schema", "schemas", "migration", "migrations", "migrate":
					return "sensitive: " + p
				}
			}
		}
	}
	return ""
}
