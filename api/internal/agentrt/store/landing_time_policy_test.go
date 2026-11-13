package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// CUT §9 (review 646 §1, N652-2): a locked decision is taken at the clock
// read once its locks are held, not at the time its pass began. A claim reads
// Git for minutes between its two locked reads; what took effect or ran out
// meanwhile decides it.

// A rule that takes effect while the claim reads
// Git refuses it at the second read. On 6ea51e0e the second read selected
// the rule as of the pass's start and claimed.
func TestClaimJudgesTheRuleInForceAtItsSecondRead(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w, task, src := goalDrill(t, "M-3")
	t0 := time.Now().UTC().Truncate(time.Second)
	defer freezeClock(t, t0)()
	result := src.Result
	tightened := false
	src.Result = func(c context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		if !tightened {
			tightened = true
			body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
			effective := t0.Add(time.Minute).Format(time.RFC3339)
			_, err := store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", effective, until, body)))
			ok(t, err)
			freezeClock(t, t0.Add(2*time.Minute))
		}
		return result(c, repo, task, attempt, base, a)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, t0)
	ok(t, err)
	if !tightened {
		t.Fatal("fixture: the claim did not read Git")
	}
	if op != "" || d.Allowed || d.Reason != "rule" {
		t.Fatalf("claimed under the rule of the pass's start: op=%q %+v", op, d)
	}
	if n := len(landingState(t, w, landingRepoStream).Operations); n != 0 {
		t.Fatalf("a rule refusal recovers: nothing is recorded, got %d operations", n)
	}
}

// A root whose claim window runs out while the
// claim reads Git is not claimed: the second read finds the window over and
// records failed{bound} (N652-2). On 6ea51e0e it wrote claimed after the
// window.
func TestClaimWindowEndingDuringTheReadIsBound(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w, task, src := goalDrill(t, "M-3")
	var accepted time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT max(at) FROM agentrt.event WHERE task_id = $1
	  AND event_type = 'task_state_changed' AND body_json->>'to' = 'accepted'`, task).Scan(&accepted))
	start := accepted.Add(6*time.Hour - time.Second)
	defer freezeClock(t, start)()
	result := src.Result
	read := false
	src.Result = func(c context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		read = true
		freezeClock(t, accepted.Add(6*time.Hour+time.Second))
		return result(c, repo, task, attempt, base, a)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, start)
	ok(t, err)
	if !read {
		t.Fatal("fixture: the window ended before the read")
	}
	if op != "" || d.Reason != "bound" {
		t.Fatalf("claimed after its window: op=%q %+v", op, d)
	}
	s := landingState(t, w, landingRepoStream)
	o, exists := s.Operations[s.LatestOperation(task)]
	if !exists || o.Phase != "failed" || o.Outcome != "bound" {
		t.Fatalf("the window's end was not recorded: %+v", o)
	}
}

// A decision is taken at the event clock, the
// clock its records are written with, whatever the pass's now says (CUT §9
// v2, review 656 B-R2).
func TestDecisionAtIsTheEventClock(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	restore := freezeClock(t, now.Add(-time.Minute))
	defer restore()
	if got := store.DecisionAt(ctx); !got.Equal(now.Add(-time.Minute)) {
		t.Fatal("not the clock", got)
	}
	freezeClock(t, now.Add(time.Minute))
	if got := store.DecisionAt(ctx); !got.Equal(now.Add(time.Minute)) {
		t.Fatal("not the clock", got)
	}
}

// The event clock is read for the context: two contexts with a clock each
// read their own, a context made from one keeps it, an event is written at
// its context's time, and a context with no clock, as every one the platform
// makes, reads the wall clock.
func TestEventClockIsReadForTheContext(t *testing.T) {
	t.Parallel()
	early := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	late := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	a := store.WithEventClock(context.Background(), func() time.Time { return early })
	b := store.WithEventClock(context.Background(), func() time.Time { return late })
	if got := store.DecisionAt(a); !got.Equal(early) {
		t.Fatalf("the first context read %s, not its own clock", got)
	}
	if got := store.DecisionAt(b); !got.Equal(late) {
		t.Fatalf("the second context read %s, not its own clock", got)
	}
	child, cancel := context.WithCancel(a)
	defer cancel()
	if got := store.DecisionAt(child); !got.Equal(early) {
		t.Fatalf("a context made from the first read %s", got)
	}
	before := time.Now().UTC().Truncate(time.Microsecond)
	got := store.DecisionAt(context.Background())
	if after := time.Now().UTC(); got.Before(before) || got.After(after) {
		t.Fatalf("a context with no clock read %s; the wall clock went from %s to %s", got, before, after)
	}

	w := newWorld(t)
	for _, c := range []struct {
		ctx  context.Context
		want time.Time
	}{{a, early}, {b, late}} {
		var written store.Appended
		ok(t, store.InTx(c.ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			written, err = store.Append(c.ctx, tx, store.Event{Stream: "system", Type: "note", Actor: "controller", Body: map[string]any{"text": "a clock of its own"}})
			return err
		}))
		var at time.Time
		ok(t, w.rt.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE event_id = $1`, written.EventID).Scan(&at))
		if !written.At.Equal(c.want) || !at.Equal(c.want) {
			t.Fatalf("an event under a context whose clock reads %s was written at %s (stored %s)", c.want, written.At, at)
		}
	}
}

// A tightened version that takes effect while the
// CI step reads CI: no CI is kicked under the rule of before the read
// (review 659 R659-1). Before the refresh, the step judged the new time
// against the old rule's facts and called EnsureCI.
func TestDrillCIStepKicksNothingUnderARuleThatEndedDuringItsReads(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w, task, src := goalDrill(t, "M-3")
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("fixture claim", d)
	}
	clock := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	defer freezeClock(t, clock)()
	ref := "refs/heads/land/stg/" + task + "/1"
	ok(t, appendLanding(w, landingRepoStream, landingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", clock.Format(time.RFC3339Nano), "dry_run", true)))
	ok(t, appendLanding(w, landingRepoStream, landingRecord(op, "branch_intent", "ref", ref, "R", commitB)))
	ok(t, appendLanding(w, landingRepoStream, landingRecord(op, "branch_pushed", "ref", ref, "R", commitB)))
	// Past the ten minutes after prepared: a missing CI is kicked.
	start := clock.Add(11 * time.Minute)
	freezeClock(t, start)
	tightened, ensured := false, 0
	drill := controller.DrillSources{Facts: src,
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, revision string, _ time.Time) (landing.CIRun, error) {
			if !tightened {
				tightened = true
				body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
				_, e := store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", start.Add(time.Minute).Format(time.RFC3339), until, body)))
				ok(t, e)
				freezeClock(t, start.Add(2*time.Minute))
			}
			return landing.CIRun{Status: "Missing"}, nil
		},
		EnsureCI: func(context.Context, string, string, string, string) error { ensured++; return nil },
	}
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	a, err := ctl.LandingDrillCI(ctx, drill, op, start)
	if !tightened {
		t.Fatal("fixture: CI was not read")
	}
	if a.Kind == "ci_kick" || a.Kind == "ci_rerun" || ensured != 0 {
		t.Fatalf("CI kicked under the rule of before the read: %+v err=%v ensured=%d", a, err, ensured)
	}
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Kicked {
		t.Fatal("ci_kick recorded", o)
	}
}
