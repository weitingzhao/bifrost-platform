package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 B3 (CUT §11.6 v3 items 1, 7): which landing cards apply now, one kind
// per repository-stream event.

func triggersAt(t *testing.T, w *world, now time.Time) map[string]store.CardTrigger {
	t.Helper()
	var ts []store.CardTrigger
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		ts, err = store.LandingCardTriggers(ctx, tx, "bifrost-platform", now)
		return err
	}))
	out := map[string]store.CardTrigger{}
	for _, tr := range ts {
		key := tr.Op
		if tr.Kind == store.CardRepoStopped {
			key = "repo"
		}
		if _, dup := out[key]; dup {
			t.Fatalf("two cards for %s", key)
		}
		out[key] = tr
	}
	return out
}

func controllerNote(t *testing.T, w *world, root string, body map[string]any) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: root, Body: body})
		return err
	}))
}

// Serial: FreezeEventClock.
func TestLandingCardTriggers(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Now().Add(-5 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	ended := func(op, kind string, kv ...any) string {
		t.Helper()
		root := w.ready(t, store.Task{OffPlanReason: "trigger " + op})
		prodChain(t, w, root, op, at, "branch_pushed")
		store.FreezeEventClock(at.Add(10 * time.Minute))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, kind, kv...)))
		return root
	}
	retryAt := at.Add(11 * time.Minute).Format(time.RFC3339Nano)
	ended("land:stale:1", "failed", "why", "stale")
	ended("land:conflict:1", "failed", "why", "conflict")
	ended("land:floor:1", "failed", "why", "floor")
	ended("land:declined:1", "failed", "why", "stale")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:declined:1", "failed", "why", "declined")))
	ended("land:stuck:1", "parked", "why", "ci_stuck", "retry_at", retryAt)
	ended("land:window:1", "parked", "why", "window", "retry_at", retryAt)
	ended("land:rule:1", "parked", "why", "rule", "retry_at", retryAt)
	closedRoot := ended("land:closed:1", "failed", "why", "stale")
	inMain := w.ready(t, store.Task{OffPlanReason: "trigger in main"})
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:"+inMain+":1", "failed", "why", "in_main")))
	missing := w.ready(t, store.Task{OffPlanReason: "trigger ok missing"})
	prodChain(t, w, missing, "land:missing:1", at, "main_intent")
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:missing:1", "main_result", "outcome", "ok")))
	controllerNote(t, w, missing, map[string]any{"text": "anomaly", "landing_anomaly": map[string]any{"op": "land:missing:1", "root": missing, "kind": "ok_missing", "R": commitB, "M0": commitA, "head": commitA}})
	controllerNote(t, w, missing, map[string]any{"text": "anomaly", "landing_anomaly": map[string]any{"op": "land:late:1", "root": missing, "kind": "late_landing", "R": commitB, "M0": commitA, "head": commitB}})
	controllerNote(t, w, missing, map[string]any{"text": "stop", "landing_stop": map[string]any{"repo": "bifrost-platform", "op": "land:missing:1", "root": missing, "generation": 1}})
	var mission string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, closedRoot).Scan(&mission))

	got := triggersAt(t, w, at.Add(time.Hour))
	want := map[string][2]string{ // op → kind, first choice
		"land:stale:1":    {store.CardRootFailed, "try_again"},
		"land:conflict:1": {store.CardRootFailed, "do_not_merge"},
		"land:floor:1":    {store.CardFloor, "do_not_merge"},
		"land:stuck:1":    {store.CardCIStuck, "ack"},
		"land:closed:1":   {store.CardRootFailed, "try_again"},
		"land:missing:1":  {store.CardOkMissing, "confirm_main_lost"},
		"land:late:1":     {store.CardLateLanding, "ack"},
		"repo":            {store.CardRepoStopped, "repo_resume"},
	}
	for key, k := range want {
		if tr, ok := got[key]; !ok || tr.Kind != k[0] || tr.Choices[0] != k[1] || tr.Seq < 1 {
			t.Errorf("%s: %+v, want %v", key, tr, k)
		}
	}
	if tr := got["land:"+inMain+":1"]; tr.Kind != store.CardInMain {
		t.Errorf("a root already in main asks to be counted, not to try again: %+v", tr)
	}
	for _, none := range []string{"land:declined:1", "land:window:1", "land:rule:1"} {
		if tr, ok := got[none]; ok {
			t.Errorf("%s: an unwanted card %+v", none, tr)
		}
	}
	if got["repo"].StopGeneration != 1 {
		t.Fatal(got["repo"])
	}
	// Two hours into a park (not rule): parked_long. A closed mission's failed
	// root and a resumed stop have none; a new stop has its own generation.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "stopped", "test") }))
	controllerNote(t, w, missing, map[string]any{"text": "resume", "landing_resume": map[string]any{"repo": "bifrost-platform", "generation": 1}})
	got = triggersAt(t, w, at.Add(10*time.Minute+store.ParkedLongAfter))
	if tr, ok := got["land:window:1"]; !ok || tr.Kind != store.CardParkedLong {
		t.Errorf("parked two hours: %+v", tr)
	}
	if _, ok := got["land:rule:1"]; ok {
		t.Error("a rule park has a card")
	}
	if _, ok := got["land:closed:1"]; ok {
		t.Error("a closed mission's failed root has a card")
	}
	if _, ok := got["repo"]; ok {
		t.Error("a resumed stop has a card")
	}
	controllerNote(t, w, missing, map[string]any{"text": "stop", "landing_stop": map[string]any{"repo": "bifrost-platform", "op": "land:missing:1", "root": missing, "generation": 2}})
	if tr := triggersAt(t, w, at.Add(3*time.Hour))["repo"]; tr.StopGeneration != 2 {
		t.Fatal("a second stop", tr)
	}
	// ok_missing settles when the operation lands.
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord("land:missing:1", "landed", "R", commitB)))
	if _, ok := triggersAt(t, w, at.Add(3*time.Hour))["land:missing:1"]; ok {
		t.Error("ok_missing after it landed")
	}
}

// Serial: FreezeEventClock. Review 670 R-06: a park whose reason changes (a
// window park whose CI then runs long) becomes a new park with the new
// reason, so its start and reason are what the triggers read.
func TestAParkWhoseReasonChangesIsANewPark(t *testing.T) {
	w, task, src := acceptedDrill(t)
	op, at := pushedDrill(t, w, task, src)
	store.FreezeEventClock(at.Add(time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "parked", "why", "window", "retry_at", at.Add(2*time.Minute).Format(time.RFC3339Nano))))
	now := at.Add(40 * time.Minute)
	store.FreezeEventClock(now)
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	drill := controller.DrillSources{Facts: src,
		Main:          func(context.Context, string) (string, error) { return commitA, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, r string, _ time.Time) (landing.CIRun, error) {
			return landing.CIRun{Run: "ci-long", R: r, Status: "Running", CreatedAt: at.Add(time.Minute)}, nil
		},
	}
	a, err := ctl.LandingDrillCI(ctx, drill, op, now)
	ok(t, err)
	if a.Kind != "parked" || a.Why != "ci_stuck" {
		t.Fatalf("%+v", a)
	}
	o := landingState(t, w, landingRepoStream).Operations[op]
	if o.Phase != "parked" || o.Why != "ci_stuck" {
		t.Fatalf("not parked anew: %s %s", o.Phase, o.Why)
	}
	if tr, ok := triggersAt(t, w, now)[op]; !ok || tr.Kind != store.CardCIStuck || !tr.At.Equal(now) {
		t.Fatalf("the trigger is not the new park: %+v", tr)
	}
}
