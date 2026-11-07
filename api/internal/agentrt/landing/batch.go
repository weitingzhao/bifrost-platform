package landing

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Batch landing (R5:5.7, goal 2): the tasks waiting to land on
// bifrost-platform are stacked into one batch that runs one landing CI. A
// batch is several operations, one per root, each claimed as today, all
// prepared with the same M0 (main when the batch was formed) and the same
// R, the tip of the members replayed one on another in batch order (member
// 1 on M0, member k on member k-1's commit). The first member is the lead:
// only it kicks, reruns or parks for CI. Green, R is pushed once to main;
// red, the members the failing tests point at go back for rework, and when
// nothing maps the batch is split in half and tested again. No protocol kind
// is new: the batch itself is a controller note beside the protocol.

// BatchMax is the most members a batch has (Owner 2026-11-06: 1000); the
// rule's limits.max_batch may set fewer, and the day's quota left bounds it.
const BatchMax = 1000

// BatchCandidate is a root that may join a batch, with the facts its claim
// would be judged on (read with no operation yet).
type BatchCandidate struct {
	Task  string
	Facts Facts
}

// BatchID names the batch led by lead.
func BatchID(lead string) string { return "batch:" + lead }

// FormBatch picks a batch's members from cands, in their order: at most
// the rule's limits.max_batch when set, and no more than the day's quota
// leaves (at most BatchMax); bifrost-platform
// roots of one tenant only (every other repository keeps the one-operation
// path); no root that S5 must check (S5 keeps its own path, alone); and no
// root its claim refuses on policy (a floor or rule refusal, or soft checks
// no grant may wait on): the controller claims that one alone, so its
// refusal is recorded. Any number of roots that may wait for a grant
// (PolicyChecks and MayAwaitGrant on their facts) join; the first of them
// leads the batch, since only the lead's CI is pinned and only the lead
// waits: its one grant covers every member's soft checks (the lead's
// material is the batch's, store/landing_batch_grant.go). A batch of one
// is today's single landing.
func FormBatch(cands []BatchCandidate, now time.Time) []BatchCandidate {
	var out []BatchCandidate
	grant := -1
	limit := BatchMax
	for _, c := range cands {
		f := c.Facts
		if f.Repo != "bifrost-platform" || f.Operation.S5.Required || S5AutomaticRoute(f) {
			continue
		}
		if len(out) > 0 && f.Tenant != out[0].Facts.Tenant {
			continue
		}
		if f.Rule != nil && f.Rule.Landing != nil {
			limit = min(limit, f.Rule.Landing.Limits.MaxLandingsPerDay-f.ReservedToday)
			if m := f.Rule.Landing.Limits.MaxBatch; m > 0 {
				limit = min(limit, m)
			}
		}
		if len(out) >= limit {
			break
		}
		reason, soft := PolicyChecks(f, now)
		if reason != "" || len(soft) > 0 && !MayAwaitGrant(f, soft) {
			continue
		}
		if len(soft) > 0 && grant < 0 {
			grant = len(out)
		}
		out = append(out, c)
	}
	if grant > 0 {
		lead := out[grant]
		out = append([]BatchCandidate{lead}, slices.Delete(out, grant, grant+1)...)
	}
	return out
}

// Split is the first and the second half of members, the first the larger.
func Split[T any](members []T) ([]T, []T) {
	h := (len(members) + 1) / 2
	return slices.Clone(members[:h]), slices.Clone(members[h:])
}

// BatchMember is one member as blame reads it: its operation, its root and
// the paths its result changes (rules.LandingChange paths).
type BatchMember struct {
	Op, Task string
	Paths    []string
}

var (
	// FAIL<tab><import path>, as go test ends a failed package (also
	// "[build failed]" and "[setup failed]").
	blameFailPkg = regexp.MustCompile(`(?m)^\s*FAIL\s+((?:[\w.\-]+/)+[\w.\-]+)`)
	// "# <import path>", the header of a package's build errors.
	blameBuildPkg = regexp.MustCompile(`(?m)^\s*#\s+((?:[\w.\-]+/)+[\w.\-]+)`)
	// <path>.go:<line>, a build error (path.go:12:3:) or a panic's frame.
	blameGoFile = regexp.MustCompile(`((?:[\w.\-]+/)*[\w.\-]+)\.go:\d+`)
	// <import path>.<function>, a panic's frame.
	blamePanicFrame = regexp.MustCompile(`(?m)^\s*((?:[\w.\-]+/)+[\w\-]+)\.[\w(*]`)
)

// BlameRed returns the members whose changed files lie in a package
// directory the failed TaskRuns' Message or LogTail names: go test's
// FAIL<tab><import path> line, a build error's "# <import path>" header
// or <path>.go:<line>:<col>: position, and after a panic its frames'
// files and packages. Empty when nothing maps (the batch is then split).
func BlameRed(failure CIFailure, members []BatchMember) []BatchMember {
	var named []string
	add := func(name string) {
		name = strings.TrimPrefix(path.Clean(name), "./")
		if name != "" && name != "." && name != "/" && !slices.Contains(named, name) {
			named = append(named, name)
		}
	}
	for _, tr := range failure.TaskRuns {
		for _, text := range []string{tr.Message, tr.LogTail} {
			for _, m := range blameFailPkg.FindAllStringSubmatch(text, -1) {
				add(m[1])
			}
			for _, m := range blameBuildPkg.FindAllStringSubmatch(text, -1) {
				add(m[1])
			}
			for _, m := range blameGoFile.FindAllStringSubmatch(text, -1) {
				add(path.Dir(m[1]))
			}
			if i := strings.Index(text, "panic:"); i >= 0 {
				for _, m := range blamePanicFrame.FindAllStringSubmatch(text[i:], -1) {
					add(m[1])
				}
			}
		}
	}
	var out []BatchMember
	for _, m := range members {
		if slices.ContainsFunc(m.Paths, func(p string) bool { return packageNamed(path.Dir(p), named) }) {
			out = append(out, m)
		}
	}
	return out
}

// packageNamed: the package directory dir (repository-relative) is one of
// named, given as an import path, an absolute path or a path relative to a
// module below the repository root.
func packageNamed(dir string, named []string) bool {
	if dir == "" || dir == "." {
		return false
	}
	for _, n := range named {
		if n == dir || strings.HasSuffix(n, "/"+dir) || strings.HasSuffix(dir, "/"+n) {
			return true
		}
	}
	return false
}
