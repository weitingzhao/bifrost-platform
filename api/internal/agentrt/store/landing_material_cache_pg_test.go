package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: c5Wait freezes the event clock. Exercise the real database reader
// and create check with a shared cache; the executor/final gate stay uncached.
func TestMaterialCacheRequestChecksStayLive(t *testing.T) {
	c := c5Wait(t, c5Soft, true, true)
	c.gsrc.Cache = &store.GrantMaterialCache{}
	result, patch := c.gsrc.Facts.Result, c.gsrc.Facts.GrantPatch
	reads := 0
	c.gsrc.Facts.Result = func(ctx context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		reads++
		return result(ctx, repo, task, attempt, base, a)
	}
	c.gsrc.Facts.GrantPatch = func(ctx context.Context, a, m0, r string) (store.GrantPatch, error) {
		reads++
		return patch(ctx, a, m0, r)
	}
	p := c.params(t)
	if _, err := store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionGuardrailLanding, p); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatalf("second material read repeated Git: %d", reads)
	}
	c.main = commitC
	if _, err := store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionGuardrailLanding, p); err == nil || !strings.Contains(err.Error(), "main_changed") {
		t.Fatalf("cached main: %v", err)
	}
	c.main = commitA
	c.ci.UID = "replacement-ci-object"
	if _, err := store.CheckLandingGrantRequest(ctx, c.w.rt, c.gsrc, store.ActionGuardrailLanding, p); err == nil || !strings.Contains(err.Error(), "material_mismatch") {
		t.Fatalf("cached material digest: %v", err)
	}
	if reads != 2 {
		t.Fatalf("live checks repeated Git: %d", reads)
	}
}
