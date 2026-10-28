package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock. CUT §11.8 v3 item 1: PROD claims only after its
// live mode is recorded; the fence then is the mode, so work accepted
// between enabled and the mode is not taken (and not held by Close), while
// work after it is. The mode is recorded once.
func TestLiveModeFencesTheProdClaim(t *testing.T) {
	w, before, src := goalDrill(t, "M-3")
	op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", before, time.Now())
	ok(t, err)
	if op != "" || d.Reason != "mode" {
		t.Fatalf("a PROD claim before the live mode: %q %+v", op, d)
	}
	at, err := store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	again, err := store.EnsureLandingMode(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if !again.Equal(at) {
		t.Fatal("the mode recorded twice", at, again)
	}
	op, d, err = store.ClaimLanding(ctx, w.rt, src, "prod", before, time.Now())
	ok(t, err)
	if op != "" || d.Reason != "before_enable" {
		t.Fatalf("work accepted before the mode was claimed: %q %+v", op, d)
	}
	w.goalMission(t, "M-4")
	after := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "after the mode", BaseCommit: commitB})
	w.passRound(t, after, "h1", "h2", "h3")
	op, d, err = store.ClaimLanding(ctx, w.rt, src, "prod", after, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatalf("work after the mode not claimed: %+v", d)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; !o.Live {
		t.Fatal("the claim after the mode is not live")
	}
}
