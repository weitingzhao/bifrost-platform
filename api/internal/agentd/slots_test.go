package agentd_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// AGENTD_SLOTS: unset is the default, otherwise 1 to the per_host ceiling.
func TestSlots(t *testing.T) {
	for v, want := range map[string]int{"": agentd.DefaultSlots, " ": agentd.DefaultSlots, "1": 1, "4": 4, "32": rules.PerHostCeiling} {
		if n, err := agentd.Slots(v); err != nil || n != want {
			t.Errorf("%q: %d %v, want %d", v, n, err, want)
		}
	}
	for _, v := range []string{"0", "-1", "33", "three", "2.5"} {
		if n, err := agentd.Slots(v); err == nil {
			t.Errorf("%q: %d, want an error", v, n)
		}
	}
}
