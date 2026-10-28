package store_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: SetHook, FreezeEventClock (goalDrill).
// TD-447 3: one CloseMissions pass folds a repository's landing stream once
// for all its closable missions while the stream does not move, and again
// once it moves.
func TestClosePassFoldsTheLandingStreamOnce(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	w.goalMission(t, "M-4")
	other := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a second drill", BaseCommit: commitB})
	w.passRound(t, other, "h4", "h5", "h6")
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("claim refused", d)
	}
	var folds atomic.Int32
	defer store.SetHook(func(point, arg string) {
		if point == "landing_fold" && arg == landingRepoStream {
			folds.Add(1)
		}
	})()
	// Past both first-claim graces: M-3 holds by its claimed operation, M-4
	// (never claimed) closes; both read the same stream.
	restore := store.FreezeEventClock(time.Now().Add(3 * time.Minute))
	defer restore()
	closed, err := store.CloseMissions(ctx, w.rt)
	ok(t, err)
	if len(closed) != 1 || closed[0] != "M-4" || w.state(t, "mission", "M-3") != "active" {
		t.Fatalf("fixture: closed=%v M-3=%s", closed, w.state(t, "mission", "M-3"))
	}
	if n := folds.Load(); n != 1 {
		t.Fatalf("the stream was folded %d times in one pass, want 1", n)
	}
	// The stream moves: the next pass folds it again and sees the move.
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "stale")))
	folds.Store(0)
	closed, err = store.CloseMissions(ctx, w.rt)
	ok(t, err)
	if n := folds.Load(); n != 1 || len(closed) != 1 || closed[0] != "M-3" {
		t.Fatalf("after the stream moved: folds=%d closed=%v", n, closed)
	}
}
