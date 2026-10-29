package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock. Independent connections append snapshots in both
// orders; A's pending snapshot predates B's terminal snapshot, but arrives last.
func TestC4Review690RequestInterleaving(t *testing.T) {
	for _, status := range []string{"approved", "running", "unknown", "expired", "failed", "executed", "rejected"} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/late=%v", status, late), func(t *testing.T) {
				w, _, op, _, at := c4Wait(t)
				a, b := w.db.Connect(t, w.db.Runtime), w.db.Connect(t, w.db.Runtime)
				pending, advanced := c4Request(op, "pending", at), c4Request(op, status, at)
				appendRequest := func(db *pgx.Conn, r store.LandingGrantRequest) {
					t.Helper()
					wrote, err := store.RecordLandingGrantRequest(ctx, db, r)
					ok(t, err)
					if !wrote {
						t.Fatal("distinct snapshot lost from audit")
					}
				}
				if late {
					appendRequest(b, advanced)
					appendRequest(a, pending)
				} else {
					appendRequest(a, pending)
					appendRequest(b, advanced)
				}
				before := w.lastSeq(t, landingRepoStream)
				wrote, err := store.RecordLandingGrantRequest(ctx, b, advanced)
				ok(t, err)
				if wrote || before != w.lastSeq(t, landingRepoStream) {
					t.Fatal("repeat changed audit")
				}
				shown, err := store.ReadLandingWait(ctx, a, op)
				ok(t, err)
				shown.ObserveMain(commitA)
				if shown.ApprovalStatus != status || shown.OwnerTodo {
					t.Fatalf("after repeated terminal: %+v", shown)
				}
				var n int
				ok(t, a.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE body_json->'landing_grant_request'->>'op'=$1`, op).Scan(&n))
				if n != 2 {
					t.Fatal("observations not retained", n)
				}
			})
		}
	}
}

// Serial: FreezeEventClock. B is in CI with an old branch_pushed snapshot;
// A commits awaiting_grant at E=5h59m using a separate connection. B returns
// at wall age 6h01m. With no A, the same entry must really settle bound.
func TestC4Review690CITimerInterleaving(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(fmt.Sprint(wait), func(t *testing.T) {
			w, root, op, src, at := c4Ready(t, "api/internal/agentrt/store/c4.go")
			other := w.db.Connect(t, w.db.Runtime)
			start := at.Add(5*time.Hour + 29*time.Minute)
			finish := start.Add(2 * time.Minute)
			store.FreezeEventClock(start)
			ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true}
			a, err := ctl.LandingDrillCI(ctx, controller.DrillSources{
				Facts:         src,
				Main:          func(context.Context) (string, error) { return commitA, nil },
				WindowBlocked: func(context.Context) (bool, error) { return false, nil },
				CI: func(context.Context, string, time.Time) (landing.CIRun, error) {
					if wait {
						_, d, err := store.AuthorizeLandingPush(ctx, other, src, op, observed(at.Add(-29*time.Minute)))
						ok(t, err)
						if d.Reason != "awaiting_grant" {
							t.Fatal(d)
						}
					}
					store.FreezeEventClock(finish)
					return landing.CIRun{Run: "ci-review", R: commitB, Status: "Succeeded", CreatedAt: at.Add(-28 * time.Minute)}, nil
				},
			}, op, start)
			ok(t, err)
			phase, why := phaseOf(t, w, op)
			if !wait {
				if phase != "failed" || why != "bound" || a.Why != "bound" {
					t.Fatal(phase, why, a)
				}
				return
			}
			clock := landing.RootTiming(landingState(t, w, landingRepoStream), root, finish)
			if phase != "awaiting_grant" || a.Kind != "wait" || clock.Execution != 5*time.Hour+59*time.Minute || clock.Waiting != 2*time.Minute {
				t.Fatal(phase, why, a, clock)
			}
		})
	}
}

// Serial: FreezeEventClock. The locked store entry checks the latest state
// even if a controller submits an already-expired snapshot.
func TestC4Review690LockedTimerGuards(t *testing.T) {
	w, _, op, _, at := c4Wait(t)
	observed := landingState(t, w, landingRepoStream).Operations[op]
	store.FreezeEventClock(at.Add(7 * time.Hour))
	before := w.lastSeq(t, landingRepoStream)
	why, err := store.SettleLandingOperationBound(ctx, w.rt, op, observed)
	ok(t, err)
	if why != "" || before != w.lastSeq(t, landingRepoStream) {
		t.Fatal("paused E ended", why)
	}
	store.FreezeEventClock(at.Add(72 * time.Hour))
	why, err = store.SettleLandingOperationBound(ctx, w.rt, op, observed)
	ok(t, err)
	if why != "approval_expired" {
		t.Fatal("current W did not end", why)
	}
	before = w.lastSeq(t, landingRepoStream)
	why, err = store.SettleLandingOperationBound(ctx, w.rt, op, observed)
	ok(t, err)
	if why != "" || before != w.lastSeq(t, landingRepoStream) {
		t.Fatal("old phase wrote", why)
	}
}
