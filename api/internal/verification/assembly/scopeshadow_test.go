// Moved from server on 2026-11-23

package assembly

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// The switch: only UNIT_SCOPE_SHADOW=on gives the records pass a reader, so
// with anything else no scope is computed for any run. Origin: main
// coordinator 2026-11-14: backend skip by package goes shadow first; a
// failing package the scope would skip is the miss this looks for.
func TestUnitScopeShadowSwitch(t *testing.T) {
	var l ScopeShadowLent
	for _, v := range []string{"", "off", "ON", "1", "true"} {
		t.Setenv(cirunview.ScopeShadowEnv, v)
		if ScopeShadow(l) != nil {
			t.Errorf("%s=%q switched the shadow on", cirunview.ScopeShadowEnv, v)
		}
	}
	// Off, the pass has no reader to tell: nothing is wired to be asked.
	if tell := ScopeShadow(l); tell != nil {
		t.Error("off, the pass has a reader")
	}
	t.Setenv(cirunview.ScopeShadowEnv, "on")
	if ScopeShadow(l) == nil {
		t.Errorf("%s=on left the shadow off", cirunview.ScopeShadowEnv)
	}
}
