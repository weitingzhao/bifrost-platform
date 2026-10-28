package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
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
	return func(context.Context, string, string, string, string) (store.MainEvidence, error) { return ev, nil }
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
				// One evidence record per terminal verdict (review 658 R4).
				var evidence int
				ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json->'landing_main'->>'op' = $2`, landingRepoStream, op).Scan(&evidence))
				if want := map[bool]int{true: 1, false: 0}[c.did == "landed" || c.did == "unsent" || c.did == "non_ff"]; evidence != want {
					t.Fatalf("pass %d: %d evidence records, want %d", pass, evidence, want)
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
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
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
	did, err := store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", cands[0], store.MainEvidence{Head: commitA, ReadAt: intent.Add(store.MainFence + time.Minute)})
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
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
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
}

// Serial: FreezeEventClock. Absence read by a pass that began inside the
// fence proves nothing, however late it is written (review 658 B2); a read
// begun past the fence does.
func TestReconcileAbsenceReadInsideTheFenceProvesNothing(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	_, intent := atMain(t, w, "land:fence:1", "unknown", at)
	fence := intent.Add(store.MainFence)
	store.FreezeEventClock(fence.Add(-time.Second))
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
		store.FreezeEventClock(fence.Add(time.Second)) // the read ends past the fence
		return store.MainEvidence{Head: commitA}, nil
	})
	ok(t, err)
	if p, o := phaseOf(t, w, "land:fence:1"); len(done) != 0 || p != "main_result" || o != "unknown" {
		t.Fatalf("absence read inside the fence was used: %v %s{%s}", done, p, o)
	}
	done, err = store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitA}))
	ok(t, err)
	if p, o := phaseOf(t, w, "land:fence:1"); done["land:fence:1"] != "unsent" || p != "failed" || o != "bound" {
		t.Fatalf("a read past the fence: %v %s{%s}", done, p, o)
	}
}

// Serial: FreezeEventClock. Two passes holding the same late candidate
// record it once (review 658 R1); a late landing is still watched after the
// Owner declined the unsent operation (R2).
func TestReconcileLateLandingOnceAndAfterDecline(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root, intent := atMain(t, w, "land:late:2", "", at)
	store.FreezeEventClock(intent.Add(store.MainFence + time.Minute))
	_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitA}))
	ok(t, err)
	later := intent.Add(time.Hour)
	store.FreezeEventClock(later)
	res, err := store.HandleLandingAnswer(ctx, w.rt, answerCard("q-late-decline", "do_not_merge", later), root, "land:late:2", later)
	ok(t, err)
	if p, o := phaseOf(t, w, "land:late:2"); res.Outcome != store.LandingAnswerApplied || p != "failed" || o != "declined" {
		t.Fatalf("fixture: %+v %s{%s}", res, p, o)
	}
	cands, err := store.MainCandidates(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if len(cands) != 1 || !cands[0].Late {
		t.Fatalf("a declined unsent operation is not watched: %+v", cands)
	}
	found := store.MainEvidence{Head: commitB, HasR: true, ReadAt: later}
	for range 2 {
		_, err := store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", cands[0], found)
		ok(t, err)
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json->'landing_anomaly'->>'op' = 'land:late:2'`, landingRepoStream).Scan(&n))
	if n != 1 {
		t.Fatalf("%d late-landing records", n)
	}
}

// Serial: FreezeEventClock. ok with R missing, recorded; R shows later: it
// lands.
func TestReconcileOkMissingThenFoundLands(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	_, intent := atMain(t, w, "land:found:1", "ok", at)
	store.FreezeEventClock(intent.Add(time.Hour))
	_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitA}))
	ok(t, err)
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true}))
	ok(t, err)
	if p, _ := phaseOf(t, w, "land:found:1"); done["land:found:1"] != "landed" || p != "landed" {
		t.Fatalf("%v %s", done, p)
	}
}

// Serial: FreezeEventClock. An S2 drill never has a main_intent: it is no
// candidate and main is never read for it (review 658).
func TestReconcileIgnoresDrills(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	root := w.ready(t, store.Task{OffPlanReason: "drill"})
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("drill:1", "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("drill:1", "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("drill:1", "failed", "why", "bound")))
	done, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
		t.Fatal("main read for a drill")
		return store.MainEvidence{}, nil
	})
	ok(t, err)
	if len(done) != 0 {
		t.Fatal(done)
	}
	_ = landing.Version
}

// Serial: FreezeEventClock. The reconciler's own loop runs without the drill
// (any environment): with no main reader it still settles denied (PROD
// today).
func TestReconcileLoopSettlesDeniedWithoutTheDrill(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	atMain(t, w, "land:loop:denied", "denied", at)
	store.FreezeEventClock(at.Add(time.Hour))
	ctl := controller.Controller{DB: w.rt, Env: "prod", Reconcile: &controller.LandingReconcile{}}
	ok(t, ctl.LandingReconcilePass(ctx))
	if p, o := phaseOf(t, w, "land:loop:denied"); p != "failed" || o != "denied" {
		t.Fatalf("%s{%s}", p, o)
	}
}

// atMainR is atMain with a given R, so a reader can tell operations apart.
func atMainR(t *testing.T, w *world, op, r string, at time.Time) time.Time {
	t.Helper()
	root := w.ready(t, store.Task{OffPlanReason: "main " + op})
	ref := "refs/heads/land/prod/" + root + "/1"
	for i, rec := range []map[string]any{
		newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1),
		newLandingRecord(op, "prepared", "R", r, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", false),
		newLandingRecord(op, "branch_intent", "ref", ref, "R", r),
		newLandingRecord(op, "branch_pushed", "ref", ref, "R", r),
		newLandingRecord(op, "ci_seen", "run", "ci-"+op, "status", "Succeeded", "R", r, "created_at", at.Add(3*time.Minute).Format(time.RFC3339Nano)),
		newLandingRecord(op, "main_intent", "R", r, "M0", commitA),
	} {
		store.FreezeEventClock(at.Add(time.Duration(i) * time.Minute))
		ok(t, appendLanding(w, landingRepoStream, rec))
	}
	return at.Add(5 * time.Minute)
}

// Serial: FreezeEventClock. A read that fails holds back that operation only
// (review 658 R3, 659 R659-4): the other is settled in the same pass; the
// failed one is not read again until its wait is over, then is.
func TestReconcileLoopBacksOffPerOperation(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	badR, goodR := strings.Repeat("c", 40), strings.Repeat("d", 40)
	intent := atMainR(t, w, "land:loop:bad", badR, at)
	atMainR(t, w, "land:loop:good", goodR, at)
	now := intent.Add(store.MainFence + time.Minute)
	store.FreezeEventClock(now)
	reads := map[string]int{}
	failing := true
	ctl := controller.Controller{DB: w.rt, Env: "stg", Now: func() time.Time { return now }}
	ctl.Reconcile = &controller.LandingReconcile{MainEvidence: func(_ context.Context, _, _, _ string, r string) (store.MainEvidence, error) {
		reads[r]++
		if r == badR && failing {
			return store.MainEvidence{}, errors.New("fetch failed")
		}
		return store.MainEvidence{Head: commitA}, nil
	}}
	if err := ctl.LandingReconcilePass(ctx); err == nil {
		t.Fatal("a failed read was not reported")
	}
	if p, _ := phaseOf(t, w, "land:loop:good"); p != "failed" {
		t.Fatalf("the healthy operation was not settled: %s", p)
	}
	if p, _ := phaseOf(t, w, "land:loop:bad"); p != "main_intent" {
		t.Fatalf("a failed read wrote %s", p)
	}
	ok(t, ctl.LandingReconcilePass(ctx))
	if reads[badR] != 1 {
		t.Fatalf("read again before its wait was over: %d reads", reads[badR])
	}
	failing = false
	now = now.Add(2 * time.Minute)
	store.FreezeEventClock(now)
	ok(t, ctl.LandingReconcilePass(ctx))
	if p, _ := phaseOf(t, w, "land:loop:bad"); reads[badR] != 2 || p != "failed" {
		t.Fatalf("after its wait: %d reads, %s", reads[badR], p)
	}
}

// Serial: FreezeEventClock. ⑪ (S3 B-02): pushed, then reverted before the
// landing was recorded: landed and reverted, both, in that order, in one
// pass. Reverted after it was recorded: the landed operation, read again
// while its mission is open, gets reverted. Unknown validity records landed
// only.
func TestReconcileRecordsReversals(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	revert := strings.Repeat("f", 40)
	_, intent := atMain(t, w, "land:rev:1", "unknown", at)
	store.FreezeEventClock(intent.Add(time.Minute))
	_, err := store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: revert, HasR: true, Validity: "reverted", RevertedBy: revert}))
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations["land:rev:1"]; !o.EverLanded || !o.Reverted {
		t.Fatalf("reverted before it was recorded: %+v", o)
	}

	w2 := newKernelWorld(t)
	_, intent2 := atMain(t, w2, "land:rev:2", "ok", at)
	store.FreezeEventClock(intent2.Add(time.Minute))
	_, err = store.ReconcileMain(ctx, w2.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true, Validity: "effective"}))
	ok(t, err)
	if o := landingState(t, w2, landingRepoStream).Operations["land:rev:2"]; !o.EverLanded || o.Reverted {
		t.Fatalf("landed: %+v", o)
	}
	store.FreezeEventClock(intent2.Add(2 * time.Minute)) // a later read
	done, err := store.ReconcileMain(ctx, w2.rt, "bifrost-platform", evidence(store.MainEvidence{Head: revert, HasR: true, Validity: "reverted", RevertedBy: revert}))
	ok(t, err)
	if o := landingState(t, w2, landingRepoStream).Operations["land:rev:2"]; done["land:rev:2"] != "reverted" || !o.Reverted {
		t.Fatalf("reverted later: %v %+v", done, o)
	}

	w3 := newKernelWorld(t)
	_, intent3 := atMain(t, w3, "land:rev:3", "unknown", at)
	store.FreezeEventClock(intent3.Add(time.Minute))
	_, err = store.ReconcileMain(ctx, w3.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true}))
	ok(t, err)
	if o := landingState(t, w3, landingRepoStream).Operations["land:rev:3"]; !o.EverLanded || o.Reverted {
		t.Fatalf("unknown validity: %+v", o)
	}
	var validity string
	ok(t, w3.rt.QueryRow(ctx, `SELECT body_json->'landing_main'->>'validity' FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_main'`, landingRepoStream).Scan(&validity))
	if validity != "unknown" {
		t.Fatal(validity)
	}
	// Landed and not reversed, it is read while its mission is open, and
	// not once it is closed (review 662: asserted before any reversal).
	listed := func() bool {
		cands, err := store.MainCandidates(ctx, w3.rt, "bifrost-platform")
		ok(t, err)
		for _, c := range cands {
			if c.Op == "land:rev:3" {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("an open mission's landing not read")
	}
	ok(t, store.InTx(ctx, w3.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "done", "test") }))
	if !listed() {
		t.Fatal("a landing not read in the day after its mission closed (CUT §11.8 v3 item 2)")
	}
	store.FreezeEventClock(intent3.Add(time.Minute + store.LateRevertWithin + time.Hour))
	if listed() {
		t.Fatal("a closed mission's landing read again after a day")
	}
}

// mainEvidenceRows are the operation's landing_main records, oldest first:
// verdict/validity.
func mainEvidenceRows(t *testing.T, w *world, op string) []string {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT body_json->'landing_main'->>'verdict' || '/' || (body_json->'landing_main'->>'validity')
	  FROM agentrt.event WHERE event_stream_id = $1 AND body_json->'landing_main'->>'op' = $2 ORDER BY event_id`, landingRepoStream, op)
	ok(t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	return out
}

// Serial: FreezeEventClock. Review 662 B-04: a landing's evidence is read
// again and recorded when it says something new: unknown to effective,
// unreadable, R gone from main; the same again, or an older read, writes
// nothing.
func TestReconcileRefreshesALandingsEvidence(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	_, intent := atMain(t, w, "land:ev:1", "ok", at)
	pass := func(k int, ev store.MainEvidence, err error) map[string]string {
		t.Helper()
		store.FreezeEventClock(intent.Add(time.Duration(k) * time.Minute))
		done, e := store.ReconcileMain(ctx, w.rt, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) { return ev, err })
		if err == nil {
			ok(t, e)
		}
		return done
	}
	pass(1, store.MainEvidence{Head: commitB, HasR: true}, nil)
	pass(2, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective"}, nil)
	pass(3, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective"}, nil)
	pass(4, store.MainEvidence{}, errors.New("fetch failed"))
	pass(5, store.MainEvidence{}, errors.New("fetch failed"))
	pass(6, store.MainEvidence{Head: strings.Repeat("e", 40)}, nil)
	// Every newer read is recorded, the same answer again included: the
	// last record is the newest read (review 663 B-01).
	want := []string{"landed/unknown", "observed/effective", "observed/effective", "observed/unreadable", "observed/unreadable", "observed/absent"}
	if got := mainEvidenceRows(t, w, "land:ev:1"); !slices.Equal(got, want) {
		t.Fatalf("evidence %v, want %v", got, want)
	}
	if o := landingState(t, w, landingRepoStream).Operations["land:ev:1"]; o.Phase != "landed" || o.Reverted {
		t.Fatalf("an observation moved the operation: %+v", o)
	}
	// An older read committed late is not recorded over a newer one.
	c := store.MainCandidate{Op: "land:ev:1", Root: landingState(t, w, landingRepoStream).Operations["land:ev:1"].Task, Phase: "landed", Outcome: "ok", R: commitB, M0: commitA}
	did, err := store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", c, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective", ReadAt: intent.Add(5 * time.Minute)})
	ok(t, err)
	if did != "" || len(mainEvidenceRows(t, w, "land:ev:1")) != 6 {
		t.Fatal("a stale observation recorded", did)
	}
	// An older read that saw a reversal, committed after a newer read, does
	// not end the landing as reverted (review 663 B-01).
	revert := strings.Repeat("f", 40)
	did, err = store.ReconcileMainOne(ctx, w.rt, "bifrost-platform", c, store.MainEvidence{Head: revert, HasR: true, Validity: "reverted", RevertedBy: revert, ReadAt: intent.Add(5*time.Minute + time.Second)})
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations["land:ev:1"]; did != "" || o.Reverted {
		t.Fatal("an older reversal read recorded over a newer read", did)
	}
	// The same answer read later raises the mark: an older read with
	// another answer, committed after it, is not recorded.
	w3 := newKernelWorld(t)
	_, intent3 := atMain(t, w3, "land:ev:3", "ok", at)
	store.FreezeEventClock(intent3.Add(time.Minute))
	_, err = store.ReconcileMain(ctx, w3.rt, "bifrost-platform", evidence(store.MainEvidence{Head: commitB, HasR: true}))
	ok(t, err)
	c3 := store.MainCandidate{Op: "land:ev:3", Root: landingState(t, w3, landingRepoStream).Operations["land:ev:3"].Task, Phase: "landed", Outcome: "ok", R: commitB, M0: commitA}
	_, err = store.ReconcileMainOne(ctx, w3.rt, "bifrost-platform", c3, store.MainEvidence{Head: commitB, HasR: true, ReadAt: intent3.Add(3 * time.Minute)})
	ok(t, err)
	did, err = store.ReconcileMainOne(ctx, w3.rt, "bifrost-platform", c3, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective", ReadAt: intent3.Add(2 * time.Minute)})
	ok(t, err)
	if got := mainEvidenceRows(t, w3, "land:ev:3"); did != "" || !slices.Equal(got, []string{"landed/unknown", "observed/unknown"}) {
		t.Fatal("an older effective over a newer unknown", did, got)
	}
	// A failed read of an operation not yet landed writes nothing.
	w2 := newKernelWorld(t)
	_, intent2 := atMain(t, w2, "land:ev:2", "unknown", at)
	store.FreezeEventClock(intent2.Add(store.MainFence + time.Minute))
	_, err = store.ReconcileMain(ctx, w2.rt, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
		return store.MainEvidence{}, errors.New("fetch failed")
	})
	if err == nil || len(mainEvidenceRows(t, w2, "land:ev:2")) != 0 {
		t.Fatal("a failed read recorded for an unsettled push", err)
	}
}

// Review 662 N-01: versioned evidence is checked field by field; a record
// without a version is checked as before, its validity typed.
func TestMainEvidenceRecordsAreChecked(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	root := w.ready(t, store.Task{OffPlanReason: "evidence records"})
	base := func(kv ...any) map[string]any {
		m := map[string]any{"v": store.MainEvidenceVersion, "op": "land:x:1", "root": root, "verdict": "observed", "R": commitB, "M0": commitA,
			"head": commitB, "has_r": true, "validity": "effective", "reverted_by": "", "read_at": time.Now().UTC().Format(time.RFC3339Nano)}
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] == nil {
				delete(m, kv[i].(string))
				continue
			}
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	put := func(m map[string]any) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: root, Body: map[string]any{"text": "evidence", "landing_main": m}})
			return err
		})
	}
	revert := strings.Repeat("f", 40)
	for name, c := range map[string]struct {
		m  map[string]any
		ok bool
	}{
		"effective":                           {base(), true},
		"reverted":                            {base("validity", "reverted", "reverted_by", revert), true},
		"absent":                              {base("validity", "absent", "has_r", false, "head", commitA), true},
		"unreadable":                          {base("validity", "unreadable", "has_r", false, "head", ""), true},
		"validity a number":                   {base("validity", 5), false},
		"validity missing":                    {base("validity", nil), false},
		"reverted, no reversal":               {base("validity", "reverted"), false},
		"reversal, not reverted":              {base("reverted_by", revert), false},
		"reversal not a commit":               {base("validity", "reverted", "reverted_by", "HEAD~1"), false},
		"absent with R":                       {base("validity", "absent"), false},
		"effective without R":                 {base("has_r", false), false},
		"unreadable with head":                {base("validity", "unreadable", "has_r", false), false},
		"reverted_by missing":                 {base("reverted_by", nil), false},
		"another version":                     {base("v", "3"), false},
		"reversal not a commit, not reverted": {base("reverted_by", "not-a-sha"), false},
		"verdict reverted, effective":         {base("verdict", "reverted"), false},
		"unreadable as a verdict":             {base("verdict", "landed", "validity", "unreadable", "has_r", false, "head", ""), false},
		"unsent with R":                       {base("verdict", "unsent"), false},
		"landed without R":                    {base("verdict", "landed", "validity", "absent", "has_r", false, "head", commitA), false},
		"unsent":                              {base("verdict", "unsent", "validity", "absent", "has_r", false, "head", commitA), true},
		"landed reverted":                     {base("verdict", "landed", "validity", "reverted", "reverted_by", revert), true},
		"legacy":                              {base("v", nil, "verdict", "landed", "validity", "unknown", "reverted_by", nil), true},
		"legacy, number":                      {base("v", nil, "verdict", "landed", "validity", 1), false},
		"legacy, observed":                    {base("v", nil), false},
	} {
		if err := put(c.m); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Serial: FreezeEventClock. Review 662 R-04: a landing, from ok or from
// unknown, is read again every thirty minutes, not every minute past the
// fence.
func TestReconcileLoopRereadsALandingHalfHourly(t *testing.T) {
	for _, outcome := range []string{"ok", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			w := newKernelWorld(t)
			at := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
			defer store.FreezeEventClock(at)()
			r := strings.Repeat("d", 40)
			intent := atMainR(t, w, "land:half:1", r, at)
			if outcome == "ok" {
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:half:1", "main_result", "outcome", "ok")))
			}
			now := intent.Add(store.MainFence + time.Minute)
			store.FreezeEventClock(now)
			reads := 0
			ctl := controller.Controller{DB: w.rt, Env: "prod", Now: func() time.Time { return now }}
			ctl.Reconcile = &controller.LandingReconcile{MainEvidence: func(context.Context, string, string, string, string) (store.MainEvidence, error) {
				reads++
				return store.MainEvidence{Head: r, HasR: true, Validity: "effective"}, nil
			}}
			ok(t, ctl.LandingReconcilePass(ctx))
			if p, _ := phaseOf(t, w, "land:half:1"); p != "landed" || reads != 1 {
				t.Fatal(p, reads)
			}
			for i, step := range []time.Duration{time.Minute, 28 * time.Minute, time.Minute} {
				now = now.Add(step)
				store.FreezeEventClock(now)
				ok(t, ctl.LandingReconcilePass(ctx))
				if want := []int{1, 1, 2}[i]; reads != want {
					t.Fatalf("after step %d: %d reads, want %d", i, reads, want)
				}
			}
		})
	}
}
