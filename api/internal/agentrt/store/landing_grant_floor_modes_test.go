package store

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func TestInfraFloorPinsExistingExecutable(t *testing.T) {
	t.Parallel()
	const repo = "bifrost-trade-infra"
	file := landing.FloorFile{
		Path: "scripts/agent-host/agentd-update", Kind: "M", OldMode: "100755",
		Mode: "100755", SHA256: strings.Repeat("a", 64),
	}
	tree := landing.FloorTree{OnM0: true, Files: []landing.FloorFile{file}}
	pins, ok := floorPathsOf(repo, tree)
	if !ok || len(pins) != 1 || pins[0].BlobSHA256 != file.SHA256 {
		t.Fatalf("expected existing executable to be pinned: %v %v", pins, ok)
	}
	if reason := floorTreeStale(repo, pins, tree); reason != "" {
		t.Fatal(reason)
	}
	for _, changed := range []landing.FloorFile{
		{Path: file.Path, Kind: "A", OldMode: "000000", Mode: "100755", SHA256: file.SHA256},
		{Path: file.Path, Kind: "M", OldMode: "100644", Mode: "100755", SHA256: file.SHA256},
		{Path: file.Path, Kind: "M", OldMode: "100755", Mode: "120000", SHA256: file.SHA256},
	} {
		tree.Files[0] = changed
		if _, allowed := floorPathsOf(repo, tree); allowed || floorTreeStale(repo, pins, tree) == "" {
			t.Fatalf("invalid mode passed material or final check: %+v", changed)
		}
	}
}
