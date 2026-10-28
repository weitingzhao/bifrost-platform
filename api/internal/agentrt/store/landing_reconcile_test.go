package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 must 3 (CUT §10, §10.6): the main reconciler moves an operation stopped
// at main on, from what main shows. Before it, nothing did: Close held the
// mission for good.

// atMain puts a production operation of a fresh root at main: main_intent,
// then the given main_result outcome ("" stops at main_intent). Records are
// one minute apart from at; it returns the root and the intent's time.
func atMain(t *testing.T, w *world, op, outcome string, at time.Time) (string, time.Time) {
	t.Helper()
	root := w.ready(t, store.Task{OffPlanReason: "main " + op})
	prodChain(t, w, root, op, at, "main_intent")
	intent := at.Add(5 * time.Minute)
	if outcome != "" {
		store.FreezeEventClock(intent.Add(time.Minute))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "main_result", "outcome", outcome)))
	}
	return root, intent
}

func evidence(ev store.MainEvidence) store.MainEvidenceFunc {
	return func(context.Context, string, string) (store.MainEvidence, error) { return ev, nil }
}

func phaseOf(t *testing.T, w *world, op string) (string, string) {
	t.Helper()
	o := landingState(t, w, landingRepoStream).Operations[op]
	return o.Phase, o.Outcome
}

// Serial: FreezeEventClock. Every branch of §10.2 / §10.6, each run twice:
// the second pass writes nothing more.
func TestReconcileMainBranches(t *testing.T) {
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	in := store.MainEvidence{Head: commitB, HasR: true}
	unchanged := store.MainEvidence{Head: commitA}
	moved := store.MainEvidence{Head: "1111111111111111111111111111111111111111"}
	for _, c := range []struct {
		name, outcome string
		ev            store.MainEvidence
		after         time.Duration // since the intent, when the pass runs
		phase, result string
		did           string
	}{
		{"intent, R in main", "", in, time.Minute, "landed", "unknown", "landed"},
		{"intent, absent, inside the fence", "", unchanged, time.Minute, "main_intent", "", ""},
		{"intent, absent, main unchanged", "", unchanged, store.MainFence + time.Minute, "failed", "bound", "unsent"},
		{"intent, absent, main moved", "", moved, store.MainFence + time.Minute, "main_result", "non_ff", "non_ff"},
		{"unknown, R in main", "unknown", in, time.Minute, "landed", "unknown", "landed"},
		{"unknown, absent, inside the fence", "unknown", moved, time.Minute, "main_result", "unknown", ""},
		{"unknown, absent, main unchanged", "unknown", unchanged, store.MainFence + time.Minute, "failed", "bound", "unsent"},
		{"unknown, absent, main moved", "unknown", moved, store.MainFence + time.Minute, "main_result", "non_ff", "non_ff"},
		{"ok, R in main", "ok", in, time.Minute, "landed", "ok", "landed"},
		{"ok, R missing", "ok", moved, store.MainFence + time.Minute, "main_result", "ok", "ok_missing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newKernelWorld(t)
			defer store.FreezeEventClock(at)()
			op := "land:main:1"
			_, intent := atMain(t, w, op, c.outcome, at)
			store.FreezeEventClock(intent.Add(c.after))
			for pass := 0; pass < 2; pass++ {
				done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(c.ev))
				ok(t, err)
				want := c.did
				if pass == 1 {
					want = "" // nothing twice
				}
				if done[op] != want {
					t.Fatalf("pass %d: did %q, want %q", pass, done[op], want)
				}
				if p, o := phaseOf(t, w, op); p != c.phase || o != c.result {
					t.Fatalf("pass %d: %s{%s}, want %s{%s}", pass, p, o, c.phase, c.result)
				}
			}
		})
	}
}

// Serial: FreezeEventClock. denied ends failed{denied} with no Git read; the
// mission is no longer held for it.
func TestReconcileDeniedNeedsNoGit(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root, intent := atMain(t, w, "land:denied:1", "denied", at)
	store.FreezeEventClock(intent.Add(time.Hour))
	if s := landingState(t, w, landingRepoStream); !s.RootPending(root) {
		t.Fatal("fixture: denied should hold the root")
	}
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string) (store.MainEvidence, error) {
		t.Fatal("main read for a denied push")
		return store.MainEvidence{}, nil
	})
	ok(t, err)
	if p, o := phaseOf(t, w, "land:denied:1"); done["land:denied:1"] != "denied" || p != "failed" || o != "denied" {
		t.Fatalf("%v %s{%s}", done, p, o)
	}
	if landingState(t, w, landingRepoStream).RootPending(root) {
		t.Fatal("still held after failed{denied}")
	}
}

// Serial: FreezeEventClock. ok with R missing is recorded once and held;
// main showing R later lands it; or the Owner confirms it lost on a card
// (1A), and then the root no longer holds its mission.
func TestReconcileOkMissingEndsOnlyByTheOwner(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root, intent := atMain(t, w, "land:lost:1", "ok", at)
	store.FreezeEventClock(intent.Add(time.Hour))
	missing := evidence(store.MainEvidence{Head: commitA})
	for range 2 {
		_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", missing)
		ok(t, err)
	}
	var anomalies int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json->'landing_anomaly'->>'op' = 'land:lost:1'`, landingRepoStream).Scan(&anomalies))
	if anomalies != 1 || !landingState(t, w, landingRepoStream).RootPending(root) {
		t.Fatalf("anomalies=%d, or no longer held", anomalies)
	}
	// try_again and do_not_merge cannot end it; confirm_main_lost does.
	for i, choice := range []string{"try_again", "do_not_merge"} {
		res, err := store.HandleLandingAnswer(ctx, w.rt, answerCard(fmt.Sprintf("q-lost-%d", i), choice, intent.Add(time.Hour)), root, "land:lost:1", intent.Add(time.Hour))
		ok(t, err)
		if res.Outcome != store.LandingAnswerRefused {
			t.Fatal(choice, res)
		}
	}
	res, err := store.HandleLandingAnswer(ctx, w.rt, answerCard("q-lost", "confirm_main_lost", intent.Add(time.Hour)), root, "land:lost:1", intent.Add(time.Hour))
	ok(t, err)
	if res.Outcome != store.LandingAnswerApplied || res.Why != "main_lost" {
		t.Fatal(res)
	}
	s := landingState(t, w, landingRepoStream)
	if o := s.Operations["land:lost:1"]; o.Phase != "failed" || o.Outcome != "main_lost" || s.RootPending(root) {
		t.Fatalf("%+v pending=%v", o, s.RootPending(root))
	}
	// Anywhere else it is refused.
	other, _ := atMain(t, w, "land:lost:2", "unknown", at)
	res, err = store.HandleLandingAnswer(ctx, w.rt, answerCard("q-lost-2", "confirm_main_lost", intent.Add(time.Hour)), other, "land:lost:2", intent.Add(time.Hour))
	ok(t, err)
	if res.Outcome != store.LandingAnswerRefused || res.Why != "not_main_lost" {
		t.Fatal(res)
	}
}

// Serial: FreezeEventClock. Evidence read for a state that moved before the
// locked write is stale: nothing is written (§10.2).
func TestReconcileStaleEvidenceWritesNothing(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	_, intent := atMain(t, w, "land:moved:1", "", at)
	store.FreezeEventClock(intent.Add(store.MainFence + time.Minute))
	cands, err := store.MainCandidates(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if len(cands) != 1 {
		t.Fatal(cands)
	}
	// The pusher records its result meanwhile.
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:moved:1", "main_result", "outcome", "ok")))
	did, err := store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", cands[0], store.MainEvidence{Head: commitA})
	ok(t, err)
	if p, o := phaseOf(t, w, "land:moved:1"); did != "" || p != "main_result" || o != "ok" {
		t.Fatalf("stale evidence written: %q %s{%s}", did, p, o)
	}
}

// Serial: FreezeEventClock. A read that fails skips that operation only; it
// is never taken as R absent.
func TestReconcileReadFailureSkipsOnlyThatOperation(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	_, intent := atMain(t, w, "land:fail:1", "", at)
	atMain(t, w, "land:fail:2", "", at)
	store.FreezeEventClock(intent.Add(store.MainFence + time.Minute))
	calls := 0
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string) (store.MainEvidence, error) {
		calls++
		if calls == 1 {
			return store.MainEvidence{}, errors.New("fetch failed")
		}
		return store.MainEvidence{Head: commitA}, nil
	})
	if err == nil || calls != 2 || len(done) != 1 {
		t.Fatalf("err=%v calls=%d done=%v", err, calls, done)
	}
	failedOp := "land:fail:1"
	if _, okd := done[failedOp]; okd {
		failedOp = "land:fail:2"
	}
	if p, _ := phaseOf(t, w, failedOp); p != "main_intent" {
		t.Fatalf("a failed read wrote %s", p)
	}
}

// Serial: FreezeEventClock. An operation ended as never sent whose R main
// later shows is recorded once for the Owner (2A).
func TestReconcileLateLandingIsRecordedOnce(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	_, intent := atMain(t, w, "land:late:1", "", at)
	store.FreezeEventClock(intent.Add(store.MainFence + time.Minute))
	_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitA}))
	ok(t, err)
	if p, o := phaseOf(t, w, "land:late:1"); p != "failed" || o != "bound" {
		t.Fatalf("fixture: %s{%s}", p, o)
	}
	store.FreezeEventClock(intent.Add(time.Hour))
	for pass := 0; pass < 2; pass++ {
		done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true}))
		ok(t, err)
		want := "late_landing"
		if pass == 1 {
			want = ""
		}
		if done["land:late:1"] != want {
			t.Fatalf("pass %d: %v", pass, done)
		}
	}
	if p, o := phaseOf(t, w, "land:late:1"); p != "failed" || o != "bound" {
		t.Fatalf("a late landing changed the protocol state: %s{%s}", p, o)
	}
	_ = landing.Version
}
