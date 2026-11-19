package deliveryview_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; only a whole number of GiB above 0, within range, sets the floor.
func TestDiskFloorFromEnv(t *testing.T) {
	for value, want := range map[string]int64{
		"": 0, " 20 ": 20 << 30, "1": 1 << 30, "0": 0, "-1": 0, "1.5": 0, "much": 0, "1048576": 1 << 50, "1048577": 0,
	} {
		t.Setenv("AGENTRT_DISK_FLOOR_GIB", value)
		if got := deliveryview.DiskFloorFromEnv(); got != want {
			t.Errorf("%q: %d, want %d", value, got, want)
		}
	}
}
