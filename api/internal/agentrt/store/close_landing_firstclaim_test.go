package store_test

// TD-447 4 (CUT §7.7): the first-claim handoff. A root in the handoff is held
// until the landing stream has its first operation: its claim, the claim's
// record that it never will be (born failed as land:<root>:1), or the window's
// end (failed{bound}, recorded by the close itself or by the pass's database
// step). Each test here is red on c3cb9e76 (the two-minute grace).
import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func firstOp(t *testing.T, w *world, task string) (landing.Operation, bool) {
	t.Helper()
	o, found := landingState(t, w, landingRepoStream).Operations["land:"+task+":1"]
	return o, found
}

// Serial: FreezeEventClock (goalDrill). A refusal no later pass can turn
// around is recorded, and the mission is free at once; a read error after a
// refusing answer does not hide it (the claim stops reading there).
func TestClaimRecordsRefusalForGood(t *testing.T) {
	readErr := errors.New("history read failed")
	for _, c := range []struct {
		name, reason, why string
		single            bool
		history           landing.History
		historyErr        error
	}{
		{"result, then a history error", "result", "result", false, landing.Never, readErr},
		{"merged by hand", "history", "in_main", true, landing.Effective, nil},
		{"merged and reverted", "history", "reverted", true, landing.Reverted, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, task, src := goalDrill(t, "M-3")
			result := src.Result
			src.Result = func(cx context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
				_, changes, err := result(cx, repo, id, attempt, base, commit)
				return c.single, changes, err
			}
			src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
				return landing.MainHistory{State: c.history, Commit: commitA}, c.historyErr
			}
			op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
			ok(t, err)
			if op != "" || d.Allowed || d.Reason != c.reason {
				t.Fatalf("op=%q decision=%+v", op, d)
			}
			o, found := firstOp(t, w, task)
			if !found || o.Phase != "failed" || o.Outcome != c.why || o.ClaimNumber != 1 {
				t.Fatalf("no record of the refusal: %+v", o)
			}
			if c.why == "in_main" {
				var text string
				ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`, landingRepoStream).Scan(&text))
				if !strings.Contains(text, "at commit "+commitA) {
					t.Fatalf("the record does not say what main held: %q", text)
				}
			}
			closed, err := store.CloseMissions(ctx, w.rt)
			ok(t, err)
			if !slices.Contains(closed, "M-3") {
				t.Fatalf("a refused root still holds its mission: closed=%v", closed)
			}
		})
	}
}

// Serial: FreezeEventClock. A refusal that may pass by itself (here: history
// unknown) records nothing and keeps the mission, past the old grace and for
// hours; at the window's end the close records failed{bound} and closes, in
// one transaction.
func TestRefusalThatMayPassHoldsUntilTheWindow(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	src.History = func(context.Context, string, string, string, string) (landing.MainHistory, error) {
		return landing.MainHistory{State: landing.Unknown}, nil
	}
	accepted := time.Now()
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, accepted)
	ok(t, err)
	if op != "" || d.Reason != "history" {
		t.Fatalf("op=%q decision=%+v", op, d)
	}
	if _, found := firstOp(t, w, task); found {
		t.Fatal("a refusal that may pass was recorded")
	}
	restore := store.FreezeEventClock(accepted)
	defer restore()
	for _, after := range []time.Duration{3 * time.Minute, time.Hour, landing.ClaimWindow - time.Minute} {
		store.FreezeEventClock(accepted.Add(after))
		closed, err := store.CloseMissions(ctx, w.rt)
		ok(t, err)
		if len(closed) != 0 {
			t.Fatalf("closed %v after %v with the root still queued", closed, after)
		}
	}
	store.FreezeEventClock(accepted.Add(landing.ClaimWindow + time.Minute))
	closed, err := store.CloseMissions(ctx, w.rt)
	ok(t, err)
	o, found := firstOp(t, w, task)
	if !slices.Contains(closed, "M-3") || !found || o.Outcome != "bound" {
		t.Fatalf("at the window's end: closed=%v record=%+v", closed, o)
	}
}

// Serial: FreezeEventClock. The handoff is durable facts, not the current
// rule (CUT §7.7, §5 item 5): landing was enabled on the stream, the rule then
// expired; the root still holds its mission (c3cb9e76 closed it once the rule
// was gone).
func TestHandoffHoldsWithoutACurrentRule(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	expires := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	_, err := s.sign(w, ruleText("landing:dev", 1, "initial", since, expires, landingRuleBody))
	ok(t, err)
	enabled := time.Now().Add(-time.Hour)
	restore := store.FreezeEventClock(enabled)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", enabled)
		return e
	}))
	restore()
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "an operator's drill", BaseCommit: commitB})
	w.passRound(t, task, "h1", "h2", "h3")
	defer store.FreezeEventClock(time.Now().Add(time.Hour))()
	closed, err := store.CloseMissions(ctx, w.rt)
	ok(t, err)
	if len(closed) != 0 {
		t.Fatalf("the root left the handoff with the rule: closed=%v", closed)
	}
}

// Serial: FreezeEventClock (goalDrill). The pass's database step: no Git, no
// rule, no claim. A root of a stopped mission ends failed{stale}; a root whose
// window is over ends failed{bound}; a paused mission's root waits. Once
// recorded, a second run records nothing.
func TestSettleUnclaimed(t *testing.T) {
	w, stopped, _ := goalDrill(t, "M-3")
	w.goalMission(t, "M-4")
	late := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a second drill", BaseCommit: commitB})
	w.passRound(t, late, "h4", "h5", "h6")
	w.goalMission(t, "M-5")
	paused := w.ready(t, store.Task{MissionID: "M-5", OffPlanReason: "a third drill", BaseCommit: commitB})
	w.passRound(t, paused, "h1", "h2", "h3")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.MoveMission(ctx, tx, "controller", "M-3", "stopped", "test"); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-5", "paused", "test")
	}))
	now := time.Now()
	got, err := store.SettleUnclaimed(ctx, w.rt, "bifrost-platform", now)
	ok(t, err)
	if !slices.Equal(got, []string{stopped}) {
		t.Fatalf("now: recorded %v, want the stopped mission's root", got)
	}
	got, err = store.SettleUnclaimed(ctx, w.rt, "bifrost-platform", now.Add(landing.ClaimWindow+time.Minute))
	ok(t, err)
	slices.Sort(got)
	want := []string{late, paused}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("past the window: recorded %v, want %v", got, want)
	}
	for task, why := range map[string]string{stopped: "stale", late: "bound", paused: "bound"} {
		if o, found := firstOp(t, w, task); !found || o.Outcome != why {
			t.Fatalf("%s: %+v, want failed{%s}", task, o, why)
		}
	}
	got, err = store.SettleUnclaimed(ctx, w.rt, "bifrost-platform", now.Add(landing.ClaimWindow+time.Minute))
	ok(t, err)
	if len(got) != 0 {
		t.Fatalf("recorded twice: %v", got)
	}
}

// Serial: FreezeEventClock (goalDrill). Never claimed and past its window, the
// claim records failed{bound} from the database alone: no Git read.
func TestClaimPastTheWindowRecordsBoundWithoutGit(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	src.Result = func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
		t.Fatal("Result read for a root past its window")
		return false, nil, nil
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now().Add(landing.ClaimWindow+time.Minute))
	ok(t, err)
	if op != "" || d.Reason != "bound" {
		t.Fatalf("op=%q decision=%+v", op, d)
	}
	if o, found := firstOp(t, w, task); !found || o.Outcome != "bound" {
		t.Fatalf("no bound record: %+v", o)
	}
}
