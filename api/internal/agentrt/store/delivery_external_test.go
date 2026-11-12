package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const (
	externalCommit = "9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d"
	externalOther  = "9d9d9d9d9d9d0000000000000000000000000000"
)

// externalRun is a recorded pipeline run of externalCommit.
func externalRun(run, pipeline, result string, start time.Time, took time.Duration) store.PipelineRun {
	kind := "delivery"
	if strings.HasPrefix(pipeline, "bifrost-ci-") {
		kind = "ci"
	}
	return store.PipelineRun{Run: run, UID: "uid-" + run, Pipeline: pipeline, Kind: kind, Result: result, Repo: "bifrost-platform",
		Revision: externalCommit, CreatedAt: start, StartedAt: start, CompletedAt: start.Add(took)}
}

func externalStages(t *testing.T, list []store.DeliveryStage) map[string]store.DeliveryStage {
	t.Helper()
	if len(list) != len(store.DeliveryStageOrder) {
		t.Fatalf("stages %+v", list)
	}
	out := map[string]store.DeliveryStage{}
	for i, s := range list {
		if s.Stage != store.DeliveryStageOrder[i] {
			t.Fatalf("stage %d is %s", i, s.Stage)
		}
		if s.State == store.StageUnknown && s.Reason == "" {
			t.Errorf("%s is unknown without a reason", s.Stage)
		}
		out[s.Stage] = s
	}
	return out
}

// The stages of a commit an external work handed in come from the recorded
// pipeline runs naming exactly that commit. What the platform does not record
// (the merge into main) is unknown with its reason, and never stops the walk.
func TestFoldExternalDelivery(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	ci := externalRun("ci-platform-a", "bifrost-ci-platform", "Succeeded", at, 7*time.Minute)
	red := externalRun("ci-platform-red", "bifrost-ci-platform", "Failed", at.Add(-10*time.Minute), 3*time.Minute)
	reuse := externalRun("ci-platform-b", "bifrost-ci-platform", "Succeeded", at.Add(10*time.Minute), 25*time.Second)
	stg := externalRun("bifrost-deliver-platform-1", "bifrost-deliver-platform", "Succeeded", at.Add(15*time.Minute), 5*time.Minute)
	stgRed := externalRun("bifrost-deliver-platform-0", "bifrost-deliver-platform", "Failed", at.Add(12*time.Minute), time.Minute)
	prod := externalRun("bifrost-deliver-platform-prod-1", "bifrost-deliver-platform-prod", "Succeeded", at.Add(30*time.Minute), time.Minute)
	submitted := at.Add(40 * time.Minute)
	in := func(runs ...store.PipelineRun) store.ExternalDelivery {
		if runs == nil {
			runs = []store.PipelineRun{}
		}
		return store.ExternalDelivery{Work: "ew_1", Registrant: "thread:giant", Repo: "bifrost-platform", Commit: externalCommit, SubmittedAt: submitted, Runs: runs}
	}

	t.Run("all the way to PROD", func(t *testing.T) {
		st := externalStages(t, store.FoldExternalDelivery(in(red, ci, reuse, stgRed, stg, prod)))
		// Handed in when the commit first reached the platform: its first run, before the submitted event.
		if s := st[store.StageHandedIn]; s.State != store.StageDone || !s.EnteredAt.Equal(red.CreatedAt) || s.Source != "pipeline_seen" || s.Actor != "thread:giant" || s.ActorRole != "executor" {
			t.Errorf("handed in %+v", s)
		}
		if s := st[store.StageLandingCI]; s.State != store.StageDone || !s.EnteredAt.Equal(red.StartedAt) || !s.LeftAt.Equal(ci.CompletedAt) || s.Reason != "" || s.WaitsOn != "" {
			t.Errorf("CI %+v", s)
		}
		for _, name := range []string{store.StageOwnerWait, store.StageOnMain} {
			if s := st[name]; s.State != store.StageUnknown || !s.EnteredAt.IsZero() || !s.LeftAt.IsZero() {
				t.Errorf("%s %+v; the platform does not record the merge", name, s)
			}
		}
		if s := st[store.StageSTG]; s.State != store.StageDone || !s.EnteredAt.Equal(stgRed.StartedAt) || !s.LeftAt.Equal(stg.CompletedAt) {
			t.Errorf("STG %+v", s)
		}
		if s := st[store.StageProdRelease]; s.State != store.StageDone || !s.EnteredAt.Equal(prod.StartedAt) || !s.LeftAt.Equal(prod.CompletedAt) {
			t.Errorf("PROD release %+v", s)
		}
		if s := st[store.StageOnProd]; s.State != store.StageDone || !s.EnteredAt.Equal(prod.CompletedAt) || s.Source != "pipeline_seen" {
			t.Errorf("on PROD %+v", s)
		}
	})

	t.Run("CI green, waiting for STG", func(t *testing.T) {
		st := externalStages(t, store.FoldExternalDelivery(in(ci)))
		if s := st[store.StageSTG]; s.State != store.StageCurrent || s.WaitsOn != "STG release" || !s.EnteredAt.IsZero() {
			t.Errorf("STG %+v", s)
		}
		for _, name := range []string{store.StageProdRelease, store.StageOnProd} {
			if s := st[name]; s.State != store.StageNotReached || s.WaitsOn != "" {
				t.Errorf("%s %+v", name, s)
			}
		}
	})

	t.Run("CI failed and STG delivery failed say which run", func(t *testing.T) {
		st := externalStages(t, store.FoldExternalDelivery(in(red)))
		if s := st[store.StageLandingCI]; s.State != store.StageCurrent || s.Reason != "CI failed: ci-platform-red" || !s.LeftAt.IsZero() {
			t.Errorf("CI %+v", s)
		}
		if s := st[store.StageSTG]; s.State != store.StageNotReached {
			t.Errorf("STG %+v", s)
		}
		st = externalStages(t, store.FoldExternalDelivery(in(ci, stgRed)))
		if s := st[store.StageSTG]; s.State != store.StageCurrent || s.Reason != "STG delivery failed: bifrost-deliver-platform-0" {
			t.Errorf("STG %+v", s)
		}
	})

	t.Run("no run recorded: CI is unknown, and handed in is the submitted event", func(t *testing.T) {
		st := externalStages(t, store.FoldExternalDelivery(in()))
		if s := st[store.StageHandedIn]; s.State != store.StageDone || !s.EnteredAt.Equal(submitted) || s.Source != "external_work" || !s.LeftAt.IsZero() {
			t.Errorf("handed in %+v", s)
		}
		if s := st[store.StageLandingCI]; s.State != store.StageUnknown || !strings.Contains(s.Reason, "no completed CI run") {
			t.Errorf("CI %+v", s)
		}
	})

	t.Run("runs not read: every stage they tell is unknown", func(t *testing.T) {
		d := in()
		d.Runs = nil
		st := externalStages(t, store.FoldExternalDelivery(d))
		for _, name := range []string{store.StageLandingCI, store.StageSTG, store.StageProdRelease, store.StageOnProd} {
			if s := st[name]; s.State != store.StageUnknown {
				t.Errorf("%s %+v", name, s)
			}
		}
	})

	t.Run("a run of another revision does not count", func(t *testing.T) {
		later := stg
		later.Revision = externalOther
		st := externalStages(t, store.FoldExternalDelivery(in(ci, later)))
		if s := st[store.StageSTG]; s.State != store.StageCurrent || !s.EnteredAt.IsZero() {
			t.Errorf("STG %+v", s)
		}
	})

	t.Run("another repository's delivery is unknown, not waiting", func(t *testing.T) {
		d := in(ci)
		d.Repo = "bifrost-trade-api"
		st := externalStages(t, store.FoldExternalDelivery(d))
		for _, name := range []string{store.StageSTG, store.StageProdRelease, store.StageOnProd} {
			if s := st[name]; s.State != store.StageUnknown || !strings.Contains(s.Reason, "bifrost-trade-api") {
				t.Errorf("%s %+v", name, s)
			}
		}
	})

	t.Run("the current PROD release names the commit before its run is recorded", func(t *testing.T) {
		d := in(ci, stg)
		d.Production = &store.DeliveryProduction{Commit: externalCommit, StartedAt: prod.StartedAt, CompletedAt: prod.CompletedAt}
		st := externalStages(t, store.FoldExternalDelivery(d))
		if s := st[store.StageOnProd]; s.State != store.StageDone || !s.EnteredAt.Equal(prod.CompletedAt) || s.Source != "release_record" {
			t.Errorf("on PROD %+v", s)
		}
		// Unsettled, or of another commit, it says nothing of this one.
		for _, p := range []store.DeliveryProduction{{Reason: "PROD workloads are unavailable or unsettled"}, {Commit: externalOther, StartedAt: prod.StartedAt, CompletedAt: prod.CompletedAt}} {
			d.Production = &p
			st = externalStages(t, store.FoldExternalDelivery(d))
			if s := st[store.StageProdRelease]; s.State != store.StageCurrent || s.WaitsOn != "PROD release" {
				t.Errorf("PROD release %+v under %+v", s, p)
			}
		}
	})
}

// externalSubmitted registers an external work on node and submits it naming commits.
func externalSubmitted(t *testing.T, w *world, actor, node, run string, commits ...string) string {
	t.Helper()
	who := wr(actor)
	out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg-" + run, Root: root, Node: node, RunKey: run, Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "end", Seq: 2, Type: "submitted", Commits: commits})
	ok(t, err)
	return id
}

func recordRuns(t *testing.T, w *world, runs ...store.PipelineRun) {
	t.Helper()
	facts := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		facts = append(facts, r.Body())
	}
	n, err := store.RecordExternal(ctx, w.rt, "pipeline_seen", facts)
	ok(t, err)
	if n != len(runs) {
		t.Fatalf("recorded %d of %d runs", n, len(runs))
	}
}

// GET /delivery?commit= of a commit only an external work names: the work,
// its step and the stages its recorded runs tell, against the migrated
// database. A commit a landing operation names keeps its own answer.
// Serial: FreezeEventClock
func TestReadExternalDelivery(t *testing.T) {
	w := newKernelWorld(t)
	at := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	t.Cleanup(store.FreezeEventClock(at.Add(40 * time.Minute)))
	read := func(commit string, p *store.DeliveryProduction) (map[string]any, error) {
		return store.ReadDelivery(ctx, w.rt, store.DeliveryQuery{Commit: commit, Production: p}, nil, nil)
	}
	_, err := read(externalCommit[:8], nil)
	is(t, err, pgx.ErrNoRows)

	id := externalSubmitted(t, w, "thread:giant", s1, "wave-1", externalCommit)
	ci := externalRun("ci-platform-a", "bifrost-ci-platform", "Succeeded", at, 7*time.Minute)
	stg := externalRun("bifrost-deliver-platform-1", "bifrost-deliver-platform", "Succeeded", at.Add(15*time.Minute), 5*time.Minute)
	recordRuns(t, w, ci, stg)
	// The run recorded again with its end: the latest record of a run is the one read.
	stgEnd := stg
	stgEnd.CompletedAt = stg.CompletedAt.Add(time.Minute)
	recordRuns(t, w, stgEnd)

	doc, err2 := read(externalCommit[:8], nil)
	err = err2
	ok(t, err)
	if doc["schema"] != "agentrt.delivery_response/v1" || doc["commit"] != externalCommit || doc["external_work_id"] != id || doc["plan_node_id"] != s1 ||
		doc["repo"] != "bifrost-platform" || doc["task_id"] != "" || doc["attempt_id"] != "" || doc["operation"] != "" || doc["as_of"] == "" {
		t.Fatalf("response %+v", doc)
	}
	if ws, _ := doc["warnings"].([]string); !anyContains(ws, "exact commit") {
		t.Errorf("warnings %v", doc["warnings"])
	}
	st := externalStages(t, doc["stages"].([]store.DeliveryStage))
	if s := st[store.StageHandedIn]; s.State != store.StageDone || !s.EnteredAt.Equal(ci.CreatedAt) || s.Actor != "thread:giant" {
		t.Errorf("handed in %+v", s)
	}
	if s := st[store.StageLandingCI]; s.State != store.StageDone || !s.LeftAt.Equal(ci.CompletedAt) {
		t.Errorf("CI %+v", s)
	}
	if s := st[store.StageSTG]; s.State != store.StageDone || !s.LeftAt.Equal(stgEnd.CompletedAt) {
		t.Errorf("STG %+v; want the run as last recorded", s)
	}
	if s := st[store.StageProdRelease]; s.State != store.StageCurrent {
		t.Errorf("PROD release %+v", s)
	}

	// The current PROD release is passed through for the platform's own repository.
	doc, err = read(externalCommit, &store.DeliveryProduction{Commit: externalCommit, StartedAt: at.Add(30 * time.Minute), CompletedAt: at.Add(31 * time.Minute)})
	ok(t, err)
	if s := externalStages(t, doc["stages"].([]store.DeliveryStage))[store.StageOnProd]; s.State != store.StageDone || s.Source != "release_record" {
		t.Errorf("on PROD %+v", s)
	}

	// A second work naming the same commit changes nothing: the first submitted is the answer.
	externalSubmitted(t, w, "thread:other", s1, "wave-1-again", externalCommit)
	if doc, err = read(externalCommit[:12], nil); err != nil || doc["external_work_id"] != id {
		t.Fatalf("same commit twice: %v %v", doc["external_work_id"], err)
	}
	// Two commits under one prefix are refused naming both; the longer prefix still resolves.
	externalSubmitted(t, w, "thread:other", s1, "wave-2", externalOther)
	_, err = read(externalCommit[:12], nil)
	is(t, err, store.ErrInvalid)
	if !strings.Contains(err.Error(), externalOther) {
		t.Fatalf("ambiguous prefix does not name both: %v", err)
	}
	if doc, err = read(externalCommit[:16], nil); err != nil || doc["commit"] != externalCommit {
		t.Fatalf("longer prefix: %v %v", doc["commit"], err)
	}
}
