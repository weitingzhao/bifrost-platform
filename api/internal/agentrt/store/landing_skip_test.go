package store_test

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// countedFacts wraps a fixture's Facts so a pass's Git reads can be counted.
func countedFacts(src store.LandingSource) (store.LandingSource, *atomic.Int32, *atomic.Int32) {
	var results, histories atomic.Int32
	return store.LandingSource{
		Result: func(ctx context.Context, repo, task, attempt, base, commit string) (bool, []rules.LandingChange, error) {
			results.Add(1)
			return src.Result(ctx, repo, task, attempt, base, commit)
		},
		History: func(ctx context.Context, repo, task, commit, snapshot string) (landing.MainHistory, error) {
			histories.Add(1)
			return src.History(ctx, repo, task, commit, snapshot)
		},
	}, &results, &histories
}

// Serial: FreezeEventClock via acceptedDrill. A pass reads no Git for a
// candidate the landing stream already refuses (rehearsed; an operation in
// progress), and still reads it for one it does not (review 642 §4). Without
// the pass's unsettled filter the two refusals come only after Result and
// History.
func TestDrillPassSkipsStreamRefusalsBeforeFacts(t *testing.T) {
	stillCandidate := func(t *testing.T, w *world, task string) {
		t.Helper()
		tasks, e := store.LandingCandidates(ctx, w.rt, landingState(t, w, landingRepoStream).EnabledAt)
		ok(t, e)
		if !slices.Contains(tasks, task) {
			t.Fatal("fixture task is not a candidate", tasks)
		}
	}
	pass := func(t *testing.T, w *world, src store.LandingSource, now time.Time) (error, int32, int32, int32) {
		t.Helper()
		counted, results, histories := countedFacts(src)
		g := &startupDrillGit{}
		ctl := controller.Controller{DB: w.rt, Env: "stg", Now: func() time.Time { return now }, DrillGit: map[string]controller.DrillGit{"bifrost-platform": g}, Drill: &controller.DrillSources{Facts: counted}}
		e := ctl.LandingDrillPass(ctx)
		return e, results.Load(), histories.Load(), g.opens.Load()
	}

	t.Run("rehearsed", func(t *testing.T) {
		w, task, src := acceptedDrill(t)
		op, at := pushedDrill(t, w, task, src)
		restore := store.FreezeEventClock(at.Add(time.Second))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_seen", "R", commitB, "run", "green", "status", "Succeeded", "created_at", at.Add(time.Second).Format(time.RFC3339Nano))))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "rehearsed", "R", commitB)))
		restore()
		stillCandidate(t, w, task)
		before := len(landingState(t, w, landingRepoStream).Operations)
		e, results, histories, opens := pass(t, w, src, at.Add(time.Minute))
		ok(t, e)
		state := landingState(t, w, landingRepoStream)
		if results != 0 || histories != 0 || opens != 0 || len(state.Operations) != before || state.Operations[op].Phase != "rehearsed" {
			t.Fatal("rehearsed task read Git", results, histories, opens, state.Operations)
		}
	})
	t.Run("in progress", func(t *testing.T) {
		w, task, src := acceptedDrill(t)
		now := time.Now()
		op, d, e := store.ClaimLandingDrill(ctx, w.rt, src, task, now)
		ok(t, e)
		if !d.Allowed || op == "" {
			t.Fatal(d)
		}
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "parked", "why", "rule", "retry_at", now.Add(10*time.Minute).Format(time.RFC3339Nano))))
		if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "parked" || o.Resume != "claimed" {
			t.Fatal("fixture", o)
		}
		stillCandidate(t, w, task)
		e, results, histories, opens := pass(t, w, src, now.Add(time.Minute))
		ok(t, e)
		if results != 0 || histories != 0 || opens != 0 || landingState(t, w, landingRepoStream).Operations[op].Phase != "parked" {
			t.Fatal("in-progress task read Git", results, histories, opens)
		}
	})
	t.Run("unclaimed task is still read and claimed", func(t *testing.T) {
		w, task, src := acceptedDrill(t)
		stillCandidate(t, w, task)
		e, results, histories, _ := pass(t, w, src, time.Now())
		if e == nil || !strings.Contains(e.Error(), "fixture stop") {
			t.Fatal("expected the fixture's Open stop after the claim", e)
		}
		if results == 0 || histories == 0 || landingState(t, w, landingRepoStream).LatestOperation(task) == "" {
			t.Fatal("eligible task was not read and claimed", results, histories)
		}
	})
}
