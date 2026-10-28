package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 B1 / B6 (CUT §2.5 steps 6–7, §11.4): the live second gate writes
// reserved and main_intent only when every check passes; the push result is
// recorded under the stream lock; a denied push stops the repository.

// liveReady is a live operation of goalDrill's root at branch_pushed, R on
// M0, prepared at the returned time.
func liveReady(t *testing.T) (*world, string, string, store.LandingSource, time.Time) {
	t.Helper()
	w, task, src := goalDrill(t, "M-3")
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("fixture claim", d)
	}
	prepared := time.Now().UTC().Truncate(time.Second)
	store.FreezeEventClock(prepared)
	ref := "refs/heads/land/prod/" + task + "/1"
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", prepared.Format(time.RFC3339Nano), "dry_run", false)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)))
	return w, task, op, src, prepared
}

func observed(prepared time.Time) func(context.Context, landing.Operation) (store.LiveObservation, error) {
	return func(context.Context, landing.Operation) (store.LiveObservation, error) {
		return store.LiveObservation{Main: commitA, CI: landing.CIRun{Run: "green", R: commitB, Status: "Succeeded", CreatedAt: prepared.Add(time.Minute)}}, nil
	}
}

// Serial: FreezeEventClock. Every check passes: reserved on the day's quota
// stream and main_intent on the repository stream, an Authorization with
// the intent's absolute deadline.
func TestLiveGateAuthorizesOnce(t *testing.T) {
	w, task, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(at)()
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed || auth.R != commitB || auth.M0 != commitA || auth.Root != task || !auth.Deadline.Equal(auth.IntentAt.Add(store.PushTimeout)) {
		t.Fatalf("%+v %+v", d, auth)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "main_intent" {
		t.Fatal(o.Phase)
	}
	q := landingState(t, w, "landing-quota:dev:"+at.UTC().Format("2006-01-02"))
	if q.Reservations[task] != op {
		t.Fatal("no reservation", q.Reservations)
	}
	// A second gate on the same operation authorizes nothing more.
	_, d, err = store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if d.Allowed {
		t.Fatal("authorized twice")
	}
}

// Serial: FreezeEventClock. Each refusal writes nothing.
func TestLiveGateRefusesWithoutWriting(t *testing.T) {
	for _, c := range []struct {
		name, reason string
		obs          func(time.Time) store.LiveObservation
	}{
		{"main moved", "main_changed", func(p time.Time) store.LiveObservation {
			return store.LiveObservation{Main: strings.Repeat("e", 40), CI: landing.CIRun{R: commitB, Status: "Succeeded", CreatedAt: p.Add(time.Minute)}}
		}},
		{"window", "window", func(p time.Time) store.LiveObservation {
			return store.LiveObservation{Main: commitA, WindowBlocked: true, CI: landing.CIRun{R: commitB, Status: "Succeeded", CreatedAt: p.Add(time.Minute)}}
		}},
		{"old CI", "ci", func(p time.Time) store.LiveObservation {
			return store.LiveObservation{Main: commitA, CI: landing.CIRun{R: commitB, Status: "Succeeded", CreatedAt: p.Add(-time.Minute)}}
		}},
		{"red CI", "ci", func(p time.Time) store.LiveObservation {
			return store.LiveObservation{Main: commitA, CI: landing.CIRun{R: commitB, Status: "Failed", CreatedAt: p.Add(time.Minute)}}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, task, op, src, prepared := liveReady(t)
			at := prepared.Add(10 * time.Minute)
			defer store.FreezeEventClock(at)()
			_, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, func(context.Context, landing.Operation) (store.LiveObservation, error) {
				return c.obs(prepared), nil
			})
			ok(t, err)
			if d.Allowed || d.Reason != c.reason {
				t.Fatalf("%+v", d)
			}
			if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "branch_pushed" {
				t.Fatal("written on a refusal", o.Phase)
			}
			if q := landingState(t, w, "landing-quota:dev:"+at.UTC().Format("2006-01-02")); len(q.Reservations) != 0 {
				t.Fatal("reserved on a refusal", q.Reservations, task)
			}
		})
	}
}

// Serial: FreezeEventClock. A version that takes effect during the gate's
// reads refuses it (CUT §9.7); the UTC day turning during them rolls the
// whole gate back (review 660 R-03).
func TestLiveGateDecidesAfterItsReads(t *testing.T) {
	w, _, op, src, prepared := liveReady(t)
	start := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(start)()
	body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
	_, err := store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", start.Add(time.Minute).Format(time.RFC3339), until, body)))
	ok(t, err)
	_, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, func(c context.Context, o landing.Operation) (store.LiveObservation, error) {
		store.FreezeEventClock(start.Add(2 * time.Minute))
		return observed(prepared)(c, o)
	})
	ok(t, err)
	if d.Allowed || d.Reason != "rule" {
		t.Fatalf("a rule that took effect during the reads: %+v", d)
	}

	w2, _, op2, src2, prepared2 := liveReady(t)
	midnight := time.Date(prepared2.Year(), prepared2.Month(), prepared2.Day()+1, 0, 0, 0, 0, time.UTC)
	store.FreezeEventClock(midnight.Add(-time.Second))
	_, _, err = store.AuthorizeLandingPush(ctx, w2.rt, src2, op2, func(c context.Context, o landing.Operation) (store.LiveObservation, error) {
		store.FreezeEventClock(midnight.Add(time.Second))
		return store.LiveObservation{Main: commitA, CI: landing.CIRun{R: commitB, Status: "Succeeded", CreatedAt: prepared2.Add(time.Minute)}}, nil
	})
	if !errors.Is(err, store.ErrLandingDayChanged) {
		t.Fatal("the day turned during the gate", err)
	}
	if o := landingState(t, w2, landingRepoStream).Operations[op2]; o.Phase != "branch_pushed" {
		t.Fatal("written across midnight", o.Phase)
	}
}

// Serial: FreezeEventClock. The result is recorded under the stream lock;
// what the reconciler wrote first stands. A denied push ends failed{denied}
// with the repository stopped in the same transaction (B6); claims stop.
func TestLivePushResultAndDeniedStopsTheRepository(t *testing.T) {
	w, task, op, src, prepared := liveReady(t)
	defer store.FreezeEventClock(prepared.Add(10 * time.Minute))()
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed {
		t.Fatal(d)
	}
	wrote, err := store.RecordPushResult(ctx, w.rt, auth, "denied")
	ok(t, err)
	if !wrote {
		t.Fatal("result not recorded")
	}
	if again, err := store.RecordPushResult(ctx, w.rt, auth, "ok"); err != nil || again {
		t.Fatal("a second result over the first", again, err)
	}
	ended, err := store.ReconcileDenied(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if len(ended) != 1 {
		t.Fatal(ended)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "failed" || o.Outcome != "denied" {
		t.Fatal(o.Phase, o.Outcome)
	}
	var stops int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_stop'`, landingRepoStream).Scan(&stops))
	if stops != 1 {
		t.Fatal("stops", stops)
	}
	// Another root of the repository is not claimed while it is stopped.
	other := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "another drill", BaseCommit: commitB})
	w.passRound(t, other, "h1", "h2", "h3")
	opOther, dOther, err := store.ClaimLandingDrill(ctx, w.rt, src, other, time.Now())
	ok(t, err)
	if opOther != "" || dOther.Reason != "stopped" {
		t.Fatalf("claimed while stopped: %q %+v", opOther, dOther)
	}
	_ = task
}
