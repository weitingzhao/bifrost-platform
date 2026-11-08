package store

import (
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// floorFileKindModeAllowed mirrors the infra structural check for existing
// executable scripts. Both material and the final Git check use the same rule.
func floorFileKindModeAllowed(repo string, f landing.FloorFile) bool {
	if f.Kind != "A" && f.Kind != "M" {
		return false
	}
	if f.Mode == "100644" {
		return true
	}
	return repo == "bifrost-trade-infra" && f.Kind == "M" &&
		strings.HasPrefix(f.Path, "scripts/") && f.OldMode == "100755" && f.Mode == "100755"
}

// floorPathsOf are a floor grant's pinned files from Git's M0..R in repo,
// and whether that tree can be granted at all: R one commit on M0, every
// file added or modified, floor-grantable in repo, and no Secret manifest
// (infra's k8s files).
func floorPathsOf(repo string, t landing.FloorTree) ([]GrantFloorPath, bool) {
	out := []GrantFloorPath{}
	ok := t.OnM0 && len(t.Files) > 0
	for _, f := range t.Files {
		entry := rules.FloorEntry(repo, f.Path)
		if entry == "" || f.Secret || !floorFileKindModeAllowed(repo, f) || !grantHex64.MatchString(f.SHA256) {
			ok = false
			continue
		}
		out = append(out, GrantFloorPath{Path: f.Path, Entry: entry, BlobSHA256: f.SHA256})
	}
	slices.SortFunc(out, func(a, b GrantFloorPath) int { return strings.Compare(a.Path, b.Path) })
	for i := 1; i < len(out); i++ {
		if out[i].Path == out[i-1].Path {
			ok = false
		}
	}
	return out, ok
}

// floorCIOf is the infra CI success the operation is waiting on: R's run,
// created after preparation and no later than now; zero otherwise. A
// platform floor grant names its pinned CI run instead, as the other
// platform grants do.
func floorCIOf(o landing.Operation, now time.Time) (GrantCI, bool) {
	created := o.CI.Time("created_at")
	ci := GrantCI{Pipeline: rules.LandingRepositories["bifrost-trade-infra"].CIPipeline, Run: o.CI.String("run"), UID: o.CI.String("uid"),
		CreatedAt: created.UTC().Format(time.RFC3339Nano), Status: o.CI.String("status"), SHA: o.CI.String("R")}
	green := ci.Status == "Succeeded" && ci.SHA == o.R && ci.Run != "" && ci.UID != "" && created.After(o.PreparedAt) && !created.After(now)
	return ci, green
}
