package rules

import "slices"

// S5Paths is the closed first-batch registry, not an extensible rule field.
// A signed allow and the deployed checker profile must both cover a path.
func S5Paths() []string {
	return []string{
		"console/src/lib/patrol/cronSchedule.ts",
		"console/src/lib/research/signalHealthAgeMeters.ts",
		"console/src/lib/research/researchHealthCopy.ts",
		"console/src/lib/market-data/queueHistoryModel.ts",
	}
}

func S5Candidate(repo, path string) bool {
	return repo == "bifrost-platform" && slices.Contains(S5Paths(), path)
}

func S5Allowed(l *Landing, repo, path string) bool {
	r, ok := l.Repo(repo)
	return ok && S5Candidate(repo, path) && !LandingHard(repo, path) && landingMatches(r.Allow, path) && !landingMatches(r.Deny, path)
}

// S5Enrolled lists the S5 paths a landing rule's allow covers (after deny
// and the hard prefixes). A rule that enrolls one turns every platform
// TS/TSX change into an S5 lane; store.SignRule therefore takes it only once
// the deployment's trusted S5 contract is usable (design §9 order).
func S5Enrolled(r Rule) []string {
	if r.Kind != KindLanding || r.Landing == nil {
		return nil
	}
	var out []string
	for _, path := range S5Paths() {
		if S5Allowed(r.Landing, "bifrost-platform", path) {
			out = append(out, path)
		}
	}
	return out
}
