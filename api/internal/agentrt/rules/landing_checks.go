package rules

import "slices"

// LandingChecksVersion identifies the coordinator v3 checks, including §12.
// It is audit evidence, not a substitute for recomputing the refusal set.
const LandingChecksVersion = 3

type LandingRefusal string

const (
	LandingFloorSoft    LandingRefusal = "floor_soft"
	LandingRulePaths    LandingRefusal = "rule_paths"
	LandingPresentation LandingRefusal = "presentation"
	LandingCore         LandingRefusal = "core"
	// LandingFloorGrant: a floor-grantable path (FloorEntry: infra's k8s/**,
	// scripts/**, ...; platform's floor paths neither hard nor open). Only
	// approve_floor_landing waives it, with rule_paths.
	LandingFloorGrant LandingRefusal = "floor_grant"
)

// landingCategories is every refusal category, in report order.
var landingCategories = []LandingRefusal{LandingCore, LandingFloorSoft, LandingFloorGrant, LandingRulePaths, LandingPresentation}

// Paths returns the existing endpoints of the trusted diff. Additions have
// only a new endpoint; deletions only an old one; rename/copy check both.
// Modification endpoints are identical and need only one path check.
func (c LandingChange) Paths() []string {
	var paths []string
	if c.OldPath != "" {
		paths = append(paths, c.OldPath)
	}
	if c.NewPath != "" && c.NewPath != c.OldPath {
		paths = append(paths, c.NewPath)
	}
	return paths
}

// StructuralViolation is independent of rules and grants. The trusted diff
// reader encodes change kind through its existing endpoints and modes.
func StructuralViolation(changes []LandingChange) (string, bool) {
	for _, c := range changes {
		if len(c.Paths()) == 0 {
			return "empty diff path", true
		}
		for _, p := range c.Paths() {
			if !landingPath(p) {
				return "invalid diff path: " + p, true
			}
		}
		for _, side := range [][2]string{{c.OldPath, c.OldMode}, {c.NewPath, c.NewMode}} {
			if side[0] == "" && side[1] != "" || side[0] != "" && side[1] != "100644" && side[1] != "100755" {
				return "non-regular file", true
			}
		}
		if c.Binary || c.Added < 0 || c.Deleted < 0 || (c.OldPath != "" && c.NewPath != "" && c.OldMode != c.NewMode && c.Added == 0 && c.Deleted == 0) {
			return "unsupported diff", true
		}
	}
	return "", false
}

// LandingAbsoluteViolation runs before policy validity or grant support.
func LandingAbsoluteViolation(repo string, changes []LandingChange) (string, bool) {
	for _, c := range changes {
		for _, p := range c.Paths() {
			if LandingHardAbsolute(repo, p) {
				return "hard_absolute: " + p, true
			}
		}
	}
	return "", false
}

// LandingSoftRefusals is the single predicate used at request and consumption
// time. §12 extends its four policy categories with core. Core paths can
// independently fail paths or display. Floor-grantable paths (FloorEntry)
// count as floor_grant. floor_soft is no longer produced: every floor path
// that is neither hard nor open is floor-grantable, and infra's floor paths
// are all hard (TestLandingSoftRefusalsNeverFloorSoft); grants and recorded
// refusals may still name it.
// An open path (LandingOpen) adds no core, floor_soft or presentation refusal
// and needs no allow match; deny still refuses it.
// Structural/absolute checks and rule validity must precede this function.
// A missing repo fails closed; it is never an empty (waiver-free) set.
func LandingSoftRefusals(l *Landing, repo string, changes []LandingChange) []LandingRefusal {
	r, ok := l.Repo(repo)
	if !ok {
		return []LandingRefusal{LandingRulePaths}
	}
	failed := map[LandingRefusal]bool{}
	for _, c := range changes {
		for _, p := range c.Paths() {
			open := LandingOpen(repo, p)
			if repo == "bifrost-trade-infra" && InfraFloorEntry(p) != "" {
				failed[LandingFloorGrant] = true
			} else if repo == "bifrost-platform" && FloorEntry(repo, p) != "" {
				failed[LandingFloorGrant] = true
			} else if LandingHardCore(repo, p) {
				failed[LandingCore] = true
			}
			if !open && !landingMatches(r.Allow, p) || landingMatches(r.Deny, p) || repo == "bifrost-trade-infra" && !landingMatches(InfraLandingAllow, p) {
				failed[LandingRulePaths] = true
			}
		}
		// Display checks apply only to changes entirely inside allow. A path
		// refusal is not mislabelled as an additional presentation refusal.
		// Open endpoints are not displayed: only the others are checked, and
		// a change all of whose endpoints are open has no display check.
		shown := c
		if LandingOpen(repo, c.OldPath) {
			shown.OldPath = ""
		}
		if LandingOpen(repo, c.NewPath) {
			shown.NewPath = ""
		}
		allowed := len(shown.Paths()) > 0
		for _, p := range shown.Paths() {
			allowed = allowed && landingMatches(r.Allow, p)
		}
		if repo != "bifrost-trade-infra" && allowed && !LandingPresentationOnly(r.Allow, []LandingChange{shown}) {
			failed[LandingPresentation] = true
		}
	}
	var out []LandingRefusal
	for _, category := range landingCategories {
		if failed[category] {
			out = append(out, category)
		}
	}
	// Infra: a path refusal outside the floor-grantable paths is final on
	// its own.
	if repo == "bifrost-trade-infra" && slices.Contains(out, LandingRulePaths) && !slices.Contains(out, LandingFloorGrant) {
		return []LandingRefusal{LandingRulePaths}
	}
	return out
}

// LandingWaivesCover implements soft_now ⊆ waives without accepting unknown
// categories. Grant kind/identity/validity are checked by the grant consumer.
func LandingWaivesCover(now, waives []LandingRefusal) bool {
	valid := landingCategories
	for _, set := range [][]LandingRefusal{now, waives} {
		for _, item := range set {
			if !slices.Contains(valid, item) {
				return false
			}
		}
	}
	for _, item := range now {
		if !slices.Contains(waives, item) {
			return false
		}
	}
	return true
}

// GrantSupported is intentionally independent of path hard checks. S6 owns
// infra's automatic lane; it does not gain grant support by adding that lane.
func GrantSupported(repo string) bool { return LandingRepositories[repo].GrantSupported }

// FloorGrantSupported: repo's floor-grantable paths take approve_floor_landing.
func FloorGrantSupported(repo string) bool { return LandingRepositories[repo].FloorGrantSupported }
