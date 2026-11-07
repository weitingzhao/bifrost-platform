package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// deploy_agentd's reach into the runtime: a host set aside carries the
// controller's drain control until it is set back (twice is once); an
// unknown host is refused; nothing runs on a fresh host, and a host whose
// claims reported no build reads as "".
func TestAgentdDeployHostAsideAndBack(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	if _, err := store.SetHostAside(ctx, w.rt, "h9", "deploy"); err == nil {
		t.Fatal("an unknown host was set aside")
	}
	id, err := store.SetHostAside(ctx, w.rt, "h1", "deploy_agentd: install agentd 0123456789ab")
	ok(t, err)
	var kind, by string
	var cleared *string
	read := func() {
		ok(t, w.rt.QueryRow(ctx, `SELECT control_kind, set_by_actor_id, cleared_by_actor_id FROM agentrt.control
		  WHERE control_id = $1 AND scope = 'host' AND scope_ref = 'h1'`, id).Scan(&kind, &by, &cleared))
	}
	if read(); kind != "drain" || by != "controller" || cleared != nil {
		t.Fatalf("control %s: %s by %s, cleared %v", id, kind, by, cleared)
	}
	n, err := store.HostLiveAttempts(ctx, w.rt, "h1")
	if ok(t, err); n != 0 {
		t.Fatalf("%d live attempts", n)
	}
	build, err := store.HostAgentdCommit(ctx, w.rt, "h1")
	if ok(t, err); build != "" {
		t.Fatalf("build %q before any claim reported one", build)
	}
	ok(t, store.SetHostBack(ctx, w.rt, id))
	if read(); cleared == nil || *cleared != "controller" {
		t.Fatalf("control %s not cleared by the controller: %v", id, cleared)
	}
	ok(t, store.SetHostBack(ctx, w.rt, id))
}
