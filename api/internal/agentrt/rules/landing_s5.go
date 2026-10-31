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

func S5Active(l *Landing, repo string) bool {
	for _, path := range S5Paths() {
		if S5Allowed(l, repo, path) {
			return true
		}
	}
	return false
}

// S5Refusals removes floor/display ONLY for each enrolled ordinary modification.
// All other changes keep the original predicate, including mixed display diffs.
// Size is checked over the original whole diff, never separate subsets.
func S5Refusals(l *Landing, repo string, changes []LandingChange) []LandingRefusal {
	failed := map[LandingRefusal]bool{}
	for _, r := range LandingSoftRefusals(l, repo, changes) {
		if r != LandingFloorSoft && r != LandingPresentation {
			failed[r] = true
		}
	}
	for _, c := range changes {
		if c.Kind == "M" && c.OldPath == c.NewPath && c.OldMode == "100644" && c.NewMode == "100644" && !c.Binary && S5Allowed(l, repo, c.NewPath) {
			continue
		}
		for _, r := range LandingSoftRefusals(l, repo, []LandingChange{c}) {
			if r == LandingFloorSoft || r == LandingPresentation {
				failed[r] = true
			}
		}
	}
	var out []LandingRefusal
	for _, r := range []LandingRefusal{LandingCore, LandingFloorSoft, LandingRulePaths, LandingPresentation, LandingSize} {
		if failed[r] {
			out = append(out, r)
		}
	}
	return out
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
