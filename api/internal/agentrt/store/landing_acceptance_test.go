package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// S3 cross-block acceptance (CUT §3 S3 cases, §11.11): what the per-block
// tests left to the whole.

// Serial: FreezeEventClock. Cases ②, ③ and B6 at the second gate: a change
// after the branch was pushed (the mission marked no_merge, the change
// merged by hand and reverted, the repository stopped) refuses the push and
// writes nothing.
func TestLiveGateRefusesLateChanges(t *testing.T) {
	for _, c := range []struct {
		name   string
		reason string
		late   func(t *testing.T, w *world, task, op string, src *store.LandingSource)
	}{
		{"no_merge", "", func(t *testing.T, w *world, _, _ string, _ *store.LandingSource) {
			tamperAdmin(t, w, `UPDATE agentrt.mission SET scope_json = scope_json || '{"no_merge":"owner"}'::jsonb WHERE mission_id = 'M-3'`)
		}},
		{"merged and reverted", "", func(t *testing.T, _ *world, _, _ string, src *store.LandingSource) {
			src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
				return landing.MainHistory{State: landing.Reverted, Commit: commitB, RevertedBy: strings.Repeat("d", 40)}, nil
			}
		}},
		{"stopped", "stopped", func(t *testing.T, w *world, task, op string, _ *store.LandingSource) {
			ok(t, appendNote(w, landingRepoStream, map[string]any{"text": "stop", "landing_stop": map[string]any{"repo": "bifrost-platform", "generation": 1, "op": op, "root": task}}))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, task, op, src, prepared := liveReady(t)
			at := prepared.Add(10 * time.Minute)
			defer store.FreezeEventClock(at)()
			c.late(t, w, task, op, &src)
			_, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
			ok(t, err)
			if d.Allowed || (c.reason != "" && d.Reason != c.reason) {
				t.Fatalf("pushed after %s: %+v", c.name, d)
			}
			if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase == "main_intent" {
				t.Fatal("an intent after", c.name)
			}
			if q := landingState(t, w, "landing-quota:dev:"+at.UTC().Format("2006-01-02")); len(q.Reservations) != 0 {
				t.Fatal("reserved after", c.name, q.Reservations)
			}
		})
	}
}

// Serial: FreezeEventClock. Case ① (the other order) and ⑩: a rule
// tightened after main_intent does not undo the push; main read with R
// under a later commit of someone else's records it landed.
func TestLiveTighteningAfterTheIntentStillLands(t *testing.T) {
	w, _, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute)
	defer store.FreezeEventClock(at)()
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed {
		t.Fatal(d)
	}
	body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
	store.FreezeEventClock(at.Add(time.Minute))
	_, err = store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev",
		[]byte(ruleText("landing:dev", 2, "tighten", at.Add(time.Minute).Format(time.RFC3339), until, body)))
	ok(t, err)
	store.FreezeEventClock(at.Add(2 * time.Minute))
	wrote, err := store.RecordPushResult(ctx, w.rt, auth, "ok")
	ok(t, err)
	if !wrote {
		t.Fatal("result not recorded")
	}
	store.FreezeEventClock(at.Add(3 * time.Minute))
	_, err = store.ReconcileMain(ctx, w.rt, "bifrost-platform", evidence(store.MainEvidence{Head: strings.Repeat("d", 40), HasR: true, Validity: "effective"}))
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "landed" || !o.EverLanded {
		t.Fatalf("a push under the old rule was not recorded landed: %s %s", o.Phase, o.Outcome)
	}
}

// Serial: FreezeEventClock. Case ⑤ with B4: one root delivered and one
// failed unanswered keep the mission active; the rule changing to not none
// just before the close holds the landed root on the rule.
func TestB4CloseCombinations(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	liveRoot(t, w, "land:acc1:1", at, "main_intent", newLandingRecord("land:acc1:1", "main_result", "outcome", "ok"))
	liveRoot(t, w, "land:acc2:1", at, "branch_pushed", newLandingRecord("land:acc2:1", "failed", "why", "stale"))
	readOne(t, w, "land:acc1:1", at.Add(-10*time.Minute), "effective")
	for _, d := range []time.Duration{0, 2 * time.Minute, 7 * time.Hour} {
		if d > 0 {
			readOne(t, w, "land:acc1:1", at.Add(d-time.Minute), "effective")
		}
		if st := closePass(t, w, at.Add(d)); st != "active" {
			t.Fatalf("one delivered and one unanswered closed after %s: %s", d, st)
		}
	}

	w2 := liveWorld(t, "none", at)
	liveRoot(t, w2, "land:acc3:1", at, "main_intent", newLandingRecord("land:acc3:1", "main_result", "outcome", "ok"))
	readOne(t, w2, "land:acc3:1", at.Add(-10*time.Minute), "effective")
	closePass(t, w2, at)
	readOne(t, w2, "land:acc3:1", at.Add(time.Minute), "effective")
	// The rule turns to an STG release just before the close.
	store.FreezeEventClock(at.Add(90 * time.Second))
	// Asking for an STG release widens what a landing must reach: the
	// Owner signs it.
	_, err := signer{t, rptest.NewKey(t)}.sign(w2, ruleText("landing:dev", 2, "loosen", at.Add(90*time.Second).Format(time.RFC3339), until, landingRuleBody))
	ok(t, err)
	if st := closePass(t, w2, at.Add(2*time.Minute)); st != "active" {
		t.Fatal("closed done on a rule that asks for an STG release", st)
	}
	status, err := store.ReadMissionLandingStatus(ctx, w2.rt, "M-3")
	ok(t, err)
	if len(status.Roots) != 1 || status.Roots[0].Reason != "rule" || !strings.Contains(status.Rule, "stg_release bifrost-deliver-platform") {
		t.Fatalf("status: %+v", status)
	}
}

func appendNote(w *world, stream string, body map[string]any) error {
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: "controller", Body: body})
		return err
	})
}

// Serial: FreezeEventClock. CUT §11.13: a refused claim says why on the
// mission's landing status; the same reason is recorded once, a new one again.
func TestClaimRefusalShowsOnTheStatus(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	store.FreezeEventClock(at.Add(-40 * time.Minute))
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "refused", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	status, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if len(status.Roots) != 1 || status.Roots[0].Reason != "unclaimed" {
		t.Fatalf("before any refusal: %+v", status.Roots)
	}
	store.FreezeEventClock(at)
	for i, want := range []bool{true, false} {
		wrote, err := store.RecordClaimRefusal(ctx, w.rt, "bifrost-platform", root, "stale")
		ok(t, err)
		if wrote != want {
			t.Fatalf("stale #%d wrote %v", i+1, wrote)
		}
	}
	status, err = store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if status.Roots[0].Reason != "unclaimed:stale" {
		t.Fatalf("refusal not shown: %+v", status.Roots[0])
	}
	if wrote, err := store.RecordClaimRefusal(ctx, w.rt, "bifrost-platform", root, "rule"); err != nil || !wrote {
		t.Fatal("a new reason not recorded", wrote, err)
	}
	if wrote, err := store.RecordClaimRefusal(ctx, w.rt, "bifrost-platform", root, "changed"); err != nil || wrote {
		t.Fatal("a reason that is not a refusal recorded", wrote, err)
	}
	status, err = store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
	ok(t, err)
	if status.Roots[0].Reason != "unclaimed:rule" {
		t.Fatalf("latest refusal not shown: %+v", status.Roots[0])
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND body_json ? 'landing_refusal'`, landingRepoStream).Scan(&n))
	if n != 2 {
		t.Fatal("refusal notes", n)
	}
	// Only the controller writes it, under its task.
	if err := appendNote(w, landingRepoStream, map[string]any{"text": "x", "landing_refusal": map[string]any{"v": "1", "task": root, "reason": "stale"}}); err == nil {
		t.Fatal("a refusal without its task id was accepted")
	}
}
