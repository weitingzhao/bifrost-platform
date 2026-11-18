package schemaevents

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Origin: 2026-11-18, channel L25: the topic was registered and nothing wrote
// it, so a migration reaching a database was seen only by polling /health
// (PROD's 0033 that night was checked that way). The first event must not
// replay 33 old migrations as news.
func TestOnlyWhatIsNewIsSaid(t *testing.T) {
	all := []Migration{{ID: 33}, {ID: 31}, {ID: 32}}
	ids := func(ms []Migration) (out []int) {
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return
	}
	if got := ids(toSay(all, -1)); len(got) != 1 || got[0] != 33 {
		t.Fatalf("first time: %v", got)
	}
	if got := ids(toSay(all, 31)); len(got) != 2 || got[0] != 32 || got[1] != 33 {
		t.Fatalf("after 31: %v", got)
	}
	if got := toSay(all, 33); len(got) != 0 {
		t.Fatalf("nothing new: %v", got)
	}
}

func TestTheEventKeepsItsContract(t *testing.T) {
	b := Body(Migration{ID: 33, SHA256: strings.Repeat("a", 64)})
	if err := eventlog.CheckBody(Type, b); err != nil {
		t.Fatal(err)
	}
	b["subject"] = "agentrt:32"
	if eventlog.CheckBody(Type, b) == nil {
		t.Fatal("a subject that is not its migration was taken")
	}
	if err := registry.Agree(Type); err != nil {
		t.Fatal(err)
	}
}
