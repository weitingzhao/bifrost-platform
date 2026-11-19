//bifrost:owner verification

package server

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// The switch: only UNIT_SCOPE_SHADOW=on gives the records pass a reader, so
// with anything else no scope is computed for any run. Origin: main
// coordinator 2026-11-14: backend skip by package goes shadow first; a
// failing package the scope would skip is the miss this looks for.
func TestUnitScopeShadowSwitch(t *testing.T) {
	s := &Server{delivery: &delivery.Handler{}}
	for _, v := range []string{"", "off", "ON", "1", "true"} {
		t.Setenv(cirunview.ScopeShadowEnv, v)
		if s.unitScopeShadowSource() != nil {
			t.Errorf("%s=%q switched the shadow on", cirunview.ScopeShadowEnv, v)
		}
	}
	// Off, the pass has no reader to tell: nothing is wired to be asked.
	if tell := s.unitScopeShadowSource(); tell != nil {
		t.Error("off, the pass has a reader")
	}
	t.Setenv(cirunview.ScopeShadowEnv, "on")
	if s.unitScopeShadowSource() == nil {
		t.Errorf("%s=on left the shadow off", cirunview.ScopeShadowEnv)
	}
}
