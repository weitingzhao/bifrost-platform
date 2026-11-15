package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

// TestFixtureRule checks the still-parsed fixture rows against their bodies.
func TestFixtureRule(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	_, err := w.rt.Exec(ctx, agentrttest.CardAutoRule)
	ok(t, err)
	if c := inForce(t, w, "card-auto-answer", time.Now()); c == nil || c.CardAuto.AfterSeconds != 30*60 || len(c.CardAuto.Kinds) != 1 {
		t.Fatalf("the fixture card-auto-answer rule reads as %+v", c)
	}
}
