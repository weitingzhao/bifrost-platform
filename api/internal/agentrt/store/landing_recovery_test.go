package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock. The name is kept from when a red landing CI was
// rerun once and the rerun's recovery was tested here. Since R6:6.2-1 a red
// landing CI is not rerun as it is: the operation ends as ci_red, creates no
// run, and a restarted controller creates none either.
func TestDrillRecoveryRerunCreatedAfterIntentInSameSecond(t *testing.T) {
	w, task, src := acceptedDrill(t)
	op, prepared := pushedDrill(t, w, task, src)
	intent := prepared.Add(time.Minute).Truncate(time.Second).Add(100 * time.Millisecond)
	restore := store.FreezeEventClock(intent)
	defer restore()
	creates := 0
	drill := controller.DrillSources{
		Facts:         src,
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, revision string, _ time.Time) (landing.CIRun, error) {
			status := "Failed"
			if revision == commitA {
				status = "Succeeded"
			}
			return landing.CIRun{Run: "old-failed", R: revision, Status: status, CreatedAt: prepared.Add(time.Second)}, nil
		},
		EnsureCI: func(context.Context, string, string, string, string) error {
			creates++
			return nil
		},
	}
	c := controller.Controller{DB: w.rt, Env: "stg"}
	action, err := c.LandingDrillCI(ctx, drill, op, intent)
	ok(t, err)
	if action.Kind != "failed" || action.Why != "ci_red" || creates != 0 {
		t.Fatal(action, creates)
	}
	later := intent.Add(time.Minute)
	store.FreezeEventClock(later)
	c = controller.Controller{DB: w.rt, Env: "stg"} // restart
	_, err = c.LandingDrillCI(ctx, drill, op, later)
	ok(t, err)
	if state := landingState(t, w, landingRepoStream); state.Operations[op].Phase != "failed" || creates != 0 {
		t.Fatalf("a red landing CI after a restart: %s, %d creates", state.Operations[op].Phase, creates)
	}
}

type recoveryUnavailableGit struct{ deletes int }

func (g *recoveryUnavailableGit) Open(context.Context, string) (*landinggit.Workspace, error) {
	return nil, errors.New("offline")
}
func (g *recoveryUnavailableGit) Ref(context.Context, string) (string, error) {
	return "", errors.New("offline")
}
func (g *recoveryUnavailableGit) Delete(ctx context.Context, ref, sha string) error {
	g.deletes++
	select {
	case <-time.After(400 * time.Millisecond):
		return errors.New("git read timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Scale the real 2-minute Git / 5-minute pass
// deadlines to 400ms / 1s; three older cleanup reads exhaust the pass.
func TestDrillRecoveryCleanupCannotStarveExpiredOperation(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w := newWorld(t)
	now := time.Now()
	first := now.Add(-7 * time.Hour)
	restore := freezeClock(t, first)
	defer restore()
	active := "land:zzz:1"
	for i := 1; i <= 4; i++ {
		root := fmt.Sprintf("t_%026d", i)
		op := fmt.Sprintf("land:aaa:%d", i)
		if i == 4 {
			op = active
		}
		ref := "refs/heads/land/stg/" + root + "/1"
		for _, record := range []map[string]any{
			newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1),
			newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", first.Format(time.RFC3339Nano), "dry_run", true),
			newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB),
			newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB),
		} {
			ok(t, appendLanding(w, landingRepoStream, record))
		}
		if i != 4 {
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
		}
	}
	freezeClock(t, now)
	remote := &recoveryUnavailableGit{}
	c := controller.Controller{DB: w.db.Pool(t, w.db.Runtime), Env: "stg", Now: func() time.Time { return now }, DrillGit: map[string]controller.DrillGit{"bifrost-platform": remote}, Drill: &controller.DrillSources{}}
	passCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err := c.LandingDrillPass(passCtx)
	current := landingState(t, w, landingRepoStream).Operations[active]
	if current.Phase != "failed" || current.Outcome != "bound" {
		t.Fatalf("expired operation stayed %s (active=%v); cleanup calls=%d; pass error=%v", current.Phase, current.Active(), remote.deletes, err)
	}
	if remote.deletes != 4 || err == nil {
		t.Fatalf("every cleanup must be visited and report its timeout: calls=%d err=%v", remote.deletes, err)
	}
}

// Serial: FreezeEventClock. Crash after discarded and before the next claim;
// restart with all facts available after the root's six-hour deadline.
func TestDrillRecoveryDiscardRestartPastBudget(t *testing.T) {
	w, task, src := acceptedDrill(t)
	op, at := pushedDrill(t, w, task, src)
	restore := store.FreezeEventClock(at.Add(5*time.Hour + 59*time.Minute))
	defer restore()
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
	later := at.Add(7 * time.Hour)
	store.FreezeEventClock(later)
	if err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "owner", Body: map[string]any{
			"text": "invalid writer", "landing": newLandingRecord(fmt.Sprintf("land:%s:2", task), "failed", "why", "bound"),
		}})
		return err
	}); err == nil {
		t.Fatal("non-controller created a born-terminal failure")
	}
	c := controller.Controller{DB: w.rt, Env: "stg", Now: func() time.Time { return later }, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}, Drill: &controller.DrillSources{Facts: src}}
	// New controller after the crash: recover through the actual scan entry.
	_ = c.LandingDrillPass(ctx)
	state := landingState(t, w, landingRepoStream)
	latestID := state.LatestOperation(task)
	latest := state.Operations[latestID]
	if latestID == op || latest.Phase != "failed" || latest.Outcome != "bound" {
		t.Fatalf("latest=%s/%s; elapsed bound has no durable retryable failure", latest.Phase, latest.Outcome)
	}
	if state.Operations[op].Phase != "discarded" || latest.Active() || len(state.Operations) != 2 {
		t.Fatal("discard resurrected or terminal failure occupied a slot", state)
	}
	var records int
	var kind string
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*), min(body_json->'landing'->>'kind') FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'op'=$2`, landingRepoStream, latestID).Scan(&records, &kind))
	if records != 1 || kind != "failed" {
		t.Fatal("successor was not born terminal in one event", records, kind)
	}
	c = controller.Controller{DB: w.rt, Env: "stg", Now: func() time.Time { return later }, DrillGit: map[string]controller.DrillGit{"bifrost-platform": &branchObserved{}}, Drill: &controller.DrillSources{Facts: src}}
	_ = c.LandingDrillPass(ctx)
	if len(landingState(t, w, landingRepoStream).Operations) != 2 {
		t.Fatal("restart duplicated bound failure")
	}
	ok(t, applyRetry(ctx, w.rt, task, latestID, "retry after recovery", later))
	next, decision, err := store.ClaimLandingDrill(ctx, w.rt, src, task, later)
	ok(t, err)
	if next == "" || !decision.Allowed {
		t.Fatal("retry cannot open next operation", next, decision)
	}
	state = landingState(t, w, landingRepoStream)
	first, count := landing.RootBudget(state, task)
	if !first.Equal(later) || count != 3 || state.Retries[task].ConsumedBy != next {
		t.Fatal("retry lost time or cumulative budget", first, count, state.Retries)
	}
}
