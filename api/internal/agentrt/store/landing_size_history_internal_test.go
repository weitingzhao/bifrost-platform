package store

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// PROD 2026-11-11 10:35 (88b25c2): the size gate was removed, but grants and
// floor refusals recorded before it name "size"; they stay readable.
func TestRecordedSizeStaysReadable(t *testing.T) {
	t.Parallel()
	if !validCategories([]string{"core", "rule_paths", "size"}, false) {
		t.Fatal("a recorded grant's size category no longer reads")
	}
	if validCategories([]string{"size", "core"}, false) {
		t.Fatal("an unsorted category set reads")
	}
	floor := map[string]any{"soft": []any{"size"}, "grant_supported": rules.GrantSupported("bifrost-platform")}
	if !validLandingFloor("bifrost-platform", floor) {
		t.Fatal("a recorded floor refusal with size no longer reads")
	}
}
