package rules_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// 2026-11-15 relay A: retired rule rows remain append-only history, but
// their removed parsers must not accept a new rule of the same kind.
func TestRemovedKindsRemainRecognizableHistory(t *testing.T) {
	for _, id := range []string{"intake-gates", "debt-projects", "routing:dev", "concurrency", "job:test", "capability-facts"} {
		t.Run(id, func(t *testing.T) {
			if !rules.Retired(id) {
				t.Fatal("historical rule kind is no longer recognized")
			}
			if _, _, err := rules.KindOf(id); err == nil {
				t.Fatal("removed rule kind is still accepted")
			}
			if _, err := rules.Parse([]byte(text(id, ""))); err == nil {
				t.Fatal("removed rule kind still parses")
			}
		})
	}
}
