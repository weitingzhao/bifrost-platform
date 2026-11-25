package privilege

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/bus/registry" // declares the body
)

// Origin: 2026-11-21, the metrics line's owner_privilege_uses counts
// owner.privilege.used in the ledger, and the uses were only in the
// coordinator's note (duty/privilege-log-2026-11-21.md), which is no system
// archive (LAW-8). A use's body passes the register; a cause outside the
// three kinds, an empty what or a two-line what is refused.
func TestAUseIsABodyTheRegisterAccepts(t *testing.T) {
	ok := Body(Use{What: "pushed the bus/observe hand-over", StuckBecause: "two lines waited on each other"})
	if ok["who"] != "owner" || ok["subject"] != "owner" {
		t.Fatalf("who %v, want owner by default", ok["who"])
	}
	if err := eventlog.CheckBody(Type, ok); err != nil {
		t.Fatalf("a plain use refused: %v", err)
	}
	if err := eventlog.CheckBody(Type, Body(Use{What: "x", StuckBecause: "y", Cause: "contract"})); err != nil {
		t.Fatalf("a use with a cause refused: %v", err)
	}
	for name, u := range map[string]Use{
		"cause":    {What: "x", StuckBecause: "y", Cause: "laziness"},
		"empty":    {What: " ", StuckBecause: "y"},
		"twolines": {What: "a\nb", StuckBecause: "y"},
	} {
		if err := eventlog.CheckBody(Type, Body(u)); err == nil {
			t.Errorf("%s: accepted, want refused", name)
		}
	}
}
