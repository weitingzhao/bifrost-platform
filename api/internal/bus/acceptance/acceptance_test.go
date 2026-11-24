package acceptance

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/bus/registry" // declares the body
)

func items(passes int) []string {
	out := make([]string, 10)
	for i := range out {
		out[i] = "fail"
		if i < passes {
			out[i] = "pass"
		}
	}
	return out
}

// Origin: 2026-11-21, the Owner wanted Foundation's state on the bus; the
// decoupling acceptance lived only in the Foundation seat's notes (no system
// archive, law 93), and its ruler changed three times that day, so a reading
// carries its version. A reading whose score matches its items passes the
// register; the controls: a score that does not match the items, a wrong
// item count, an unknown status and a short commit are refused.
func TestAReadingIsABodyTheRegisterAccepts(t *testing.T) {
	sha := strings.Repeat("a", 40)
	b, err := Body(Reading{Ruler: 3, Score: "7/10", Items: items(7), PlatformCommit: sha, ReadAt: "2026-11-21T18:28:25Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := eventlog.CheckBody(Type, b); err != nil {
		t.Fatalf("a plain reading refused by the register: %v", err)
	}
	if b["subject"] != "ruler-3@"+sha {
		t.Fatalf("subject %v", b["subject"])
	}
	for name, rd := range map[string]Reading{
		"score does not match": {Ruler: 3, Score: "8/10", Items: items(7), PlatformCommit: sha},
		"nine items":           {Ruler: 3, Score: "7/10", Items: items(7)[:9], PlatformCommit: sha},
		"unknown status":       {Ruler: 3, Score: "0/10", Items: append(items(0)[:9], "skipped"), PlatformCommit: sha},
		"short commit":         {Ruler: 3, Score: "7/10", Items: items(7), PlatformCommit: "abc"},
		"no ruler":             {Score: "7/10", Items: items(7), PlatformCommit: sha},
	} {
		if _, err := Body(rd); err == nil {
			t.Errorf("%s: accepted, want refused", name)
		}
	}
}
