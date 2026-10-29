package rules

import (
	"fmt"
	"slices"
)

// LandingChecksVersion identifies the coordinator v3 checks, including §12.
// It is audit evidence, not a substitute for recomputing the refusal set.
const LandingChecksVersion = 1

type LandingRefusal string

const (
	LandingFloorSoft    LandingRefusal = "floor_soft"
	LandingRulePaths    LandingRefusal = "rule_paths"
	LandingPresentation LandingRefusal = "presentation"
	LandingSize         LandingRefusal = "size"
	LandingCore         LandingRefusal = "core"
)

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
// time. §12 extends its four policy categories with core. Core paths do not
// also count as floor_soft, but can independently fail paths, size or display.
// Structural/absolute checks and rule validity must precede this function.
// A missing repo fails closed; it is never an empty (waiver-free) set.
func LandingSoftRefusals(l *Landing, repo string, changes []LandingChange) []LandingRefusal {
	r, ok := l.Repo(repo)
	if !ok {
		return []LandingRefusal{LandingRulePaths}
	}
	failed := map[LandingRefusal]bool{}
	if len(changes) == 0 || len(changes) > l.Limits.MaxFiles {
		failed[LandingSize] = true
	}
	// Subtract instead of adding untrusted int counts: no overflow can make
	// an oversized change pass, even when two counts each fit into an int.
	remaining := l.Limits.MaxChangedLines
	for _, c := range changes {
		for _, p := range c.Paths() {
			if LandingHardCore(repo, p) {
				failed[LandingCore] = true
			} else if LandingFloor(repo, p) && !LandingHard(repo, p) {
				failed[LandingFloorSoft] = true
			}
			if !landingMatches(r.Allow, p) || landingMatches(r.Deny, p) {
				failed[LandingRulePaths] = true
			}
		}
		// Display checks apply only to changes entirely inside allow. A path
		// refusal is not mislabelled as an additional presentation refusal.
		allowed := len(c.Paths()) > 0
		for _, p := range c.Paths() {
			allowed = allowed && landingMatches(r.Allow, p)
		}
		if allowed && !LandingPresentationOnly(r.Allow, []LandingChange{c}) {
			failed[LandingPresentation] = true
		}
		for _, n := range []int{c.Added, c.Deleted} {
			if n < 0 || n > remaining {
				failed[LandingSize] = true
			} else {
				remaining -= n
			}
		}
	}
	var out []LandingRefusal
	for _, category := range []LandingRefusal{LandingCore, LandingFloorSoft, LandingRulePaths, LandingPresentation, LandingSize} {
		if failed[category] {
			out = append(out, category)
		}
	}
	return out
}

// LandingWaivesCover implements soft_now ⊆ waives without accepting unknown
// categories. Grant kind/identity/validity are checked by the grant consumer.
func LandingWaivesCover(now, waives []LandingRefusal) bool {
	valid := []LandingRefusal{LandingCore, LandingFloorSoft, LandingRulePaths, LandingPresentation, LandingSize}
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
func GrantSupported(repo string) bool { return repo == "bifrost-platform" }

// legacyCheckChanges preserves the pre-grant gate's closed behavior. Grant
// callers use the layers directly; no signed rule alone authorizes a waiver.
func legacyCheckChanges(l *Landing, repo string, changes []LandingChange) error {
	if _, ok := l.Repo(repo); !ok {
		return fmt.Errorf("rule")
	}
	if why, bad := FloorViolation(repo, changes); bad {
		return fmt.Errorf("%s", why)
	}
	for _, refusal := range LandingSoftRefusals(l, repo, changes) {
		switch refusal {
		case LandingSize:
			return fmt.Errorf("limit")
		case LandingPresentation:
			return fmt.Errorf("not presentation-only")
		default:
			return fmt.Errorf("rule: %s", refusal)
		}
	}
	return nil
}
