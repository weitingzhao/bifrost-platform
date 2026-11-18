package patrol

import (
	"context"
	"testing"
)

// Origin: 2026-11-18, the list of problem channels (L11): what the patrol fixed
// or failed to fix stayed in its own run records.
func TestFixesAreSaidWhenTheOutcomeChanges(t *testing.T) {
	var said []string
	OnFix(func(_ context.Context, item string, ok bool, _ string) {
		s := item + "=failed"
		if ok {
			s = item + "=ok"
		}
		said = append(said, s)
	})
	t.Cleanup(func() { OnFix(nil) })
	for _, r := range []fixResult{
		{ItemID: "redis", Ok: false, Action: "restart"},
		{ItemID: "redis", Ok: false, Action: "restart"},
		{ItemID: "redis", Skipped: true, Action: "report-only"},
		{ItemID: "redis", Ok: true, Action: "restart"},
	} {
		sayFix(t.Context(), r)
	}
	if len(said) != 2 || said[0] != "redis=failed" || said[1] != "redis=ok" {
		t.Fatalf("said %v", said)
	}
}
