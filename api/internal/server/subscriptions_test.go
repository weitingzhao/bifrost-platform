package server

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// 2026-11-16: the subscription tables compiled into the platform pass the
// registration checks against the real registers and action catalog.
func TestSubscriptionTablesAreValid(t *testing.T) {
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs["rocket"].Rows) == 0 {
		t.Fatal("rocket has no subscription rows")
	}
	tier := func(id string) (string, bool) {
		a, ok := actions.ByID(id)
		return string(a.Tier), ok
	}
	if err := registry.CheckSubscriptions(tenants, subs, tier); err != nil {
		t.Fatal(err)
	}
}
