package ownernotice

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// Origin: 2026-11-18, the notifier's channels (L17, N03, N04): the topics were
// registered and nothing wrote them, so the notifier had nothing to take over.
// A notice must name only what its push was about.
func TestTheNoticesKeepTheirContract(t *testing.T) {
	live, ok := Liveness("thread", "local_abc:1")
	if !ok || live.Change != ChangeSilent {
		t.Fatalf("thread: %+v %v", live, ok)
	}
	if _, ok := Liveness("look", "x"); ok {
		t.Fatal("a kind that is not a liveness notice was taken")
	}
	host, _ := Liveness("host", "mac-1")
	for _, n := range []Notice{live, host, Policy("rp-1", true), Policy("", false)} {
		if err := eventlog.CheckBody(n.Type, Body(n)); err != nil {
			t.Errorf("%+v: %v", n, err)
		}
		if err := registry.Agree(n.Type); err != nil {
			t.Errorf("%s: %v", n.Type, err)
		}
	}
	if got := Policy("", false); got.Subject != "none" || got.Change != ChangeExpired {
		t.Errorf("no policy: %+v", got)
	}
	bad := Body(Policy("rp-1", true))
	bad["change"] = ChangeLost
	if eventlog.CheckBody(TypePolicy, bad) == nil {
		t.Error("a policy notice with a liveness change was taken")
	}
	if Valid(Notice{TypeLiveness, "has space", ChangeLost}) {
		t.Error("a subject the bus cannot carry was taken")
	}
}
