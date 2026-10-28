package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-447 1, 2, 6: close, the bound settlement and the drill read one notion
// of "the root still has a step to take" (landing.RootPending).

// prodChain appends a production operation's records up to and including
// last, one minute apart from at.
func prodChain(t *testing.T, w *world, task, op string, at time.Time, last string) {
	t.Helper()
	ref := "refs/heads/land/prod/" + task + "/1"
	chain := []map[string]any{
		landingRecord(op, "claimed", "task", task, "attempt", landingAttempt, "A", commitA, "rule_version", 1),
		landingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", false),
		landingRecord(op, "branch_intent", "ref", ref, "R", commitB),
		landingRecord(op, "branch_pushed", "ref", ref, "R", commitB),
		landingRecord(op, "ci_seen", "run", "ci-td447", "status", "Succeeded", "R", commitB, "created_at", at.Add(3*time.Minute).Format(time.RFC3339Nano)),
		landingRecord(op, "main_intent", "R", commitB, "M0", commitA),
		landingRecord(op, "main_result", "outcome", "non_ff"),
	}
	for i, r := range chain {
		store.FreezeEventClock(at.Add(time.Duration(i) * time.Minute))
		ok(t, appendLanding(w, landingRepoStream, r))
		if r["kind"] == last {
			return
		}
	}
}

func closeKeeps(t *testing.T, w *world, why string) {
	t.Helper()
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || len(closed) != 0 || w.state(t, "mission", "M-3") != "active" {
		t.Fatalf("%s: closed=%v err=%v", why, closed, err)
	}
}

func closeEnds(t *testing.T, w *world, why string) {
	t.Helper()
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || len(closed) != 1 || w.state(t, "mission", "M-3") != "done" {
		t.Fatalf("%s: closed=%v err=%v state=%s", why, closed, err, w.state(t, "mission", "M-3"))
	}
}

// Serial: FreezeEventClock. A push main did not fast-forward ends only that
// operation: the replay k+1 is due, so the mission holds (review 638
// TestWork638NonFFKeepsMissionForReplay), and the bound gives the root its
// born-terminal successor as it does after discarded.
func TestCloseHoldsNonFFUntilBound(t *testing.T) {
	w, task, _ := goalDrill(t, "M-3")
	op := "land:" + task + ":1"
	at := time.Now().UTC().Truncate(time.Microsecond)
	restore := store.FreezeEventClock(at)
	defer restore()
	prodChain(t, w, task, op, at, "main_result")
	closeKeeps(t, w, "non_ff lost its replay")

	later := at.Add(7 * time.Hour)
	store.FreezeEventClock(later)
	_, err := store.SettleLandingDrillBounds(ctx, w.rt, later)
	ok(t, err)
	s := landingState(t, w, landingRepoStream)
	next := s.LatestOperation(task)
	if next != "land:"+task+":2" || s.Operations[next].Phase != "failed" || s.Operations[next].Outcome != "bound" {
		t.Fatalf("non_ff root got no failed{bound} successor: latest=%s %+v", next, s.Operations[next])
	}
	if s.Operations[op].Phase != "main_result" {
		t.Fatal("the non_ff operation changed", s.Operations[op].Phase)
	}
	closeEnds(t, w, "not closed after the bound")
}

// Serial: FreezeEventClock. An Owner retry with no claim yet: k+1 is due, so
// the mission holds; six hours after the retry with no claim, the root gets
// its born-terminal failed{bound}, which uses the retry up (review 642 §2).
func TestCloseHoldsRetryUntilClaimedOrBound(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	at := time.Now().UTC().Truncate(time.Microsecond)
	restore := store.FreezeEventClock(at)
	defer restore()
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("claim refused", d)
	}
	bound := at.Add(7 * time.Hour)
	store.FreezeEventClock(bound)
	_, err = store.SettleLandingDrillBounds(ctx, w.rt, bound)
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "failed" || o.Outcome != "bound" {
		t.Fatal("fixture: claimed operation not failed{bound}", o.Phase, o.Outcome)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.ApplyLandingAnswer(ctx, tx, task, op, "try_again", "retry", "owner", bound)
	}))
	closeKeeps(t, w, "closed between the retry and its claim")

	later := bound.Add(6*time.Hour + time.Minute)
	store.FreezeEventClock(later)
	_, err = store.SettleLandingDrillBounds(ctx, w.rt, later)
	ok(t, err)
	s := landingState(t, w, landingRepoStream)
	next := s.LatestOperation(task)
	if next != "land:"+task+":2" || s.Operations[next].Phase != "failed" || s.Operations[next].Outcome != "bound" {
		t.Fatalf("retried root got no failed{bound} successor: latest=%s %+v", next, s.Operations[next])
	}
	if s.Retries[task].ConsumedBy != next {
		t.Fatal("the born-terminal successor did not use the retry", s.Retries[task])
	}
	closeEnds(t, w, "not closed after the retry expired")
}

// Serial: FreezeEventClock. An operation at main cannot take failed{bound}
// (only reconciling with main ends it). The settlement leaves it alone and
// still ends the other roots' expired operations, instead of rolling all of
// them back; Close keeps its mission past the hold limit.
func TestSettleSkipsOperationAtMain(t *testing.T) {
	w, task, _ := goalDrill(t, "M-3")
	op := "land:" + task + ":1"
	other := "t_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	otherOp := "land:" + other + ":1"
	at := time.Now().UTC().Truncate(time.Microsecond)
	restore := store.FreezeEventClock(at)
	defer restore()
	ok(t, appendLanding(w, landingRepoStream, landingRecord(otherOp, "claimed", "task", other, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	prodChain(t, w, task, op, at.Add(time.Second), "main_intent")

	later := at.Add(7 * time.Hour)
	store.FreezeEventClock(later)
	_, err := store.SettleLandingDrillBounds(ctx, w.rt, later)
	ok(t, err)
	s := landingState(t, w, landingRepoStream)
	if o := s.Operations[otherOp]; o.Phase != "failed" || o.Outcome != "bound" {
		t.Fatalf("the other root's expired operation was not settled: %+v", o)
	}
	if s.Operations[op].Phase != "main_intent" {
		t.Fatal("an operation at main was ended without reconciling", s.Operations[op].Phase)
	}
	closeKeeps(t, w, "closed with an operation at main")
}
