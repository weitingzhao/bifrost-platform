package store

import (
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// floorPathsOf are a floor grant's pinned files from Git's M0..R, and
// whether that tree can be granted at all: R one commit on M0, every file
// added or modified, floor-grantable, and no Secret manifest.
func floorPathsOf(t landing.FloorTree) ([]GrantFloorPath, bool) {
	out := []GrantFloorPath{}
	ok := t.OnM0 && len(t.Files) > 0
	for _, f := range t.Files {
		entry := rules.InfraFloorEntry(f.Path)
		if entry == "" || f.Secret || f.Kind != "A" && f.Kind != "M" || f.Mode != "100644" || !grantHex64.MatchString(f.SHA256) {
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
// created after preparation and no later than now; zero otherwise.
func floorCIOf(o landing.Operation, now time.Time) (GrantCI, bool) {
	created := o.CI.Time("created_at")
	ci := GrantCI{Pipeline: rules.LandingRepositories["bifrost-trade-infra"].CIPipeline, Run: o.CI.String("run"), UID: o.CI.String("uid"),
		CreatedAt: created.UTC().Format(time.RFC3339Nano), Status: o.CI.String("status"), SHA: o.CI.String("R")}
	green := ci.Status == "Succeeded" && ci.SHA == o.R && ci.Run != "" && ci.UID != "" && created.After(o.PreparedAt) && !created.After(now)
	return ci, green
}
