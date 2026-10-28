package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 live orchestration (CUT §11.5 item 3): on PROD with Live set, a green
// CI ends in the second gate and one push of main from this process; with
// Live unset PROD claims and pushes nothing.

// livePusher records the pushes a live landing asks for.
type livePusher struct {
	outcome landinggit.PushOutcome
	pushes  []string
	deadln  time.Time
	closed  int
}

func (p *livePusher) PushMain(_ context.Context, r, m0 string, deadline time.Time) landinggit.PushOutcome {
	p.pushes = append(p.pushes, r+"<-"+m0)
	p.deadln = deadline
	return p.outcome
}
func (p *livePusher) Close() error { p.closed++; return nil }

// liveSources: main at M0 (commitA), CI on R green after prepared, no window.
func liveSources(src store.LandingSource, prepared time.Time) controller.DrillSources {
	return controller.DrillSources{Facts: src,
		Main:          func(context.Context) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context) (bool, error) { return false, nil },
		CI: func(_ context.Context, r string, _ time.Time) (landing.CIRun, error) {
			return landing.CIRun{Run: "ci-land", R: r, Status: "Succeeded", CreatedAt: prepared.Add(time.Minute)}, nil
		},
		EnsureCI: func(context.Context, string, string) error { return nil },
	}
}

// Serial: FreezeEventClock. Green CI on PROD: the gate writes reserved and
// main_intent, the push runs once against the intent's deadline, and its
// answer is recorded; the next pass pushes nothing more.
func TestLiveLandingPushesMainOnce(t *testing.T) {
	for _, c := range []struct {
		outcome landinggit.PushOutcome
		phase   string
	}{{landinggit.PushOK, "main_result"}, {landinggit.PushDenied, "main_result"}, {landinggit.PushUnknown, "main_result"}} {
		t.Run(string(c.outcome), func(t *testing.T) {
			w, task, op, src, prepared := liveReady(t)
			at := prepared.Add(10 * time.Minute)
			defer store.FreezeEventClock(at)()
			p := &livePusher{outcome: c.outcome}
			ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{},
				OpenLive: func(_ context.Context, r string) (controller.LiveWorkspace, error) {
					if r != commitB {
						t.Fatal("workspace for", r)
					}
					return p, nil
				}}
			_, err := ctl.LandingDrillCI(ctx, liveSources(src, prepared), op, at)
			ok(t, err)
			o := landingState(t, w, landingRepoStream).Operations[op]
			if len(p.pushes) != 1 || p.pushes[0] != commitB+"<-"+commitA || p.closed != 1 {
				t.Fatal("pushes", p.pushes, p.closed)
			}
			if o.Phase != c.phase || o.Outcome != string(c.outcome) || !p.deadln.Equal(at.Add(store.PushTimeout)) {
				t.Fatalf("%s %s deadline %v", o.Phase, o.Outcome, p.deadln)
			}
			if q := landingState(t, w, "landing-quota:dev:"+at.UTC().Format("2006-01-02")); q.Reservations[task] != op {
				t.Fatal("no reservation", q.Reservations)
			}
			if _, err = ctl.LandingDrillCI(ctx, liveSources(src, prepared), op, at); err == nil || len(p.pushes) != 1 {
				t.Fatal("a second pass pushed again", err, p.pushes)
			}
		})
	}
}

// Serial: FreezeEventClock. The gate refusing (main moved under it) opens a
// workspace but pushes nothing and writes no intent.
func TestLiveLandingGateRefusalPushesNothing(t *testing.T) {
	w, _, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(at)()
	p := &livePusher{outcome: landinggit.PushOK}
	reads := 0
	s := liveSources(src, prepared)
	s.Main = func(context.Context) (string, error) {
		reads++
		if reads > 1 { // the gate's own read: main moved after "ready"
			return strings.Repeat("e", 40), nil
		}
		return commitA, nil
	}
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{},
		OpenLive: func(context.Context, string) (controller.LiveWorkspace, error) { return p, nil }}
	_, err := ctl.LandingDrillCI(ctx, s, op, at)
	ok(t, err)
	if len(p.pushes) != 0 || p.closed != 1 {
		t.Fatal("pushed on a refusal", p.pushes, p.closed)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase == "main_intent" || o.Phase == "main_result" {
		t.Fatal("intent on a refusal", o.Phase)
	}
}

// Serial: FreezeEventClock. Without Live, PROD neither scans, claims nor
// pushes, whatever its sources; a workspace that cannot open writes no
// intent.
func TestLiveLandingOffOnProd(t *testing.T) {
	w, _, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(at)()
	opened := 0
	open := func(context.Context, string) (controller.LiveWorkspace, error) {
		opened++
		return nil, errors.New("offline")
	}
	s := liveSources(src, prepared)
	off := controller.Controller{DB: w.rt, Env: "prod", DrillGit: &branchObserved{}, Drill: &s, OpenLive: open}
	ok(t, off.LandingDrillPass(ctx))
	if _, err := off.LandingDrillCI(ctx, s, op, at); err == nil {
		t.Fatal("PROD without live ran the CI step")
	}
	if err := off.LandingDrillBranch(ctx, &branchObserved{}, op, at); err == nil {
		t.Fatal("PROD without live ran the branch step")
	}
	on := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{}, OpenLive: open}
	if _, err := on.LandingDrillCI(ctx, s, op, at); err == nil || opened != 1 {
		t.Fatal("an unopened workspace", err, opened)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "branch_pushed" {
		t.Fatal("written without a workspace", o.Phase)
	}
}

// Serial: SetHook, FreezeEventClock. The reconciler moves the operation on
// between the gate's commit and the push: nothing is pushed or recorded
// (review 663 R-01).
func TestLiveLandingRereadsTheIntentBeforePushing(t *testing.T) {
	w, _, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(at)()
	defer store.SetHook(func(point, arg string) {
		if point == "landing live: before push check" && arg == op {
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "main_result", "outcome", "unknown")))
		}
	})()
	p := &livePusher{outcome: landinggit.PushOK}
	ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: &branchObserved{},
		OpenLive: func(context.Context, string) (controller.LiveWorkspace, error) { return p, nil }}
	_, err := ctl.LandingDrillCI(ctx, liveSources(src, prepared), op, at)
	ok(t, err)
	if len(p.pushes) != 0 {
		t.Fatal("pushed an intent the reconciler had moved on", p.pushes)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "main_result" || o.Outcome != "unknown" {
		t.Fatal(o.Phase, o.Outcome)
	}
}

// Serial: FreezeEventClock. Past the intent's deadline at the re-read:
// nothing is pushed.
func TestLiveLandingPushesNothingPastTheDeadline(t *testing.T) {
	w, task, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(at)()
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed {
		t.Fatal(d)
	}
	if cur, err := store.AuthorizationCurrent(ctx, w.rt, auth); err != nil || !cur {
		t.Fatal("a fresh authorization is not current", cur, err)
	}
	store.FreezeEventClock(auth.Deadline)
	if cur, err := store.AuthorizationCurrent(ctx, w.rt, auth); err != nil || cur {
		t.Fatal("current at its deadline", cur, err)
	}
	other := auth
	other.IntentSeq++
	store.FreezeEventClock(at)
	if cur, err := store.AuthorizationCurrent(ctx, w.rt, other); err != nil || cur {
		t.Fatal("another intent's licence", cur, err, task)
	}
}
