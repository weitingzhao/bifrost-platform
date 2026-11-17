//bifrost:owner delivery

package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const (
	externalCommit = "9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d"
	externalOther  = "9d9d9d9d9d9d0000000000000000000000000000"
)

// externalRun is a recorded pipeline run of externalCommit.
func externalRun(run, pipeline, result string, start time.Time, took time.Duration) seenfacts.PipelineRun {
	kind := "delivery"
	if strings.HasPrefix(pipeline, "bifrost-ci-") {
		kind = "ci"
	}
	return seenfacts.PipelineRun{Run: run, UID: "uid-" + run, Pipeline: pipeline, Kind: kind, Result: result, Repo: "bifrost-platform",
		Revision: externalCommit, CreatedAt: start, StartedAt: start, CompletedAt: start.Add(took)}
}

func externalStages(t *testing.T, list []deliveryview.DeliveryStage) map[string]deliveryview.DeliveryStage {
	t.Helper()
	if len(list) != len(deliveryview.DeliveryStageOrder) {
		t.Fatalf("stages %+v", list)
	}
	out := map[string]deliveryview.DeliveryStage{}
	for i, s := range list {
		if s.Stage != deliveryview.DeliveryStageOrder[i] {
			t.Fatalf("stage %d is %s", i, s.Stage)
		}
		if s.State == deliveryview.StageUnknown && s.Reason == "" {
			t.Errorf("%s is unknown without a reason", s.Stage)
		}
		out[s.Stage] = s
	}
	return out
}

// externalSubmitted registers an external work on node and submits it naming commits.
func externalSubmitted(t *testing.T, w *world, actor, node, run string, commits ...string) string {
	t.Helper()
	ctx := ctxOf(t)
	who := wr(actor)
	out, err := externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "reg-" + run, Root: root, Node: node, RunKey: run, Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
	ok(t, err)
	_, err = externalwork.PostExternalEvent(ctx, w.rt, who, id, externalwork.ExternalEvent{Key: "end", Seq: 2, Type: "submitted", Commits: commits})
	ok(t, err)
	return id
}

func recordRuns(t *testing.T, w *world, runs ...seenfacts.PipelineRun) {
	t.Helper()
	ctx := ctxOf(t)
	facts := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		facts = append(facts, r.Body())
	}
	n, err := seenfacts.RecordExternal(ctx, w.rt, "pipeline_seen", facts)
	ok(t, err)
	if n != len(runs) {
		t.Fatalf("recorded %d of %d runs", n, len(runs))
	}
}

// GET /delivery?commit= of a commit only an external work names: the work,
// its step and the stages its recorded runs tell, against the migrated
// database. A commit a landing operation names keeps its own answer.
func TestReadExternalDelivery(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w := newKernelWorld(t)
	at := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	t.Cleanup(freezeClock(t, at.Add(40*time.Minute)))
	read := func(commit string, p *deliveryview.DeliveryProduction) (map[string]any, error) {
		return deliveryreadings.ReadDelivery(ctx, w.rt, deliveryreadings.DeliveryQuery{Commit: commit, Production: p})
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
	st := externalStages(t, doc["stages"].([]deliveryview.DeliveryStage))
	if s := st[deliveryview.StageHandedIn]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(ci.CreatedAt) || s.Actor != "thread:giant" {
		t.Errorf("handed in %+v", s)
	}
	if s := st[deliveryview.StageLandingCI]; s.State != deliveryview.StageDone || !s.LeftAt.Equal(ci.CompletedAt) {
		t.Errorf("CI %+v", s)
	}
	if s := st[deliveryview.StageSTG]; s.State != deliveryview.StageDone || !s.LeftAt.Equal(stgEnd.CompletedAt) {
		t.Errorf("STG %+v; want the run as last recorded", s)
	}
	// The current PROD release was not read (no reader passed): unknown, not waiting.
	if s := st[deliveryview.StageProdRelease]; s.State != deliveryview.StageUnknown || !strings.Contains(s.Reason, "was not read") {
		t.Errorf("PROD release %+v", s)
	}
	doc, err = read(externalCommit, &deliveryview.DeliveryProduction{Reason: "current STG evidence does not match the promoted PROD revision", NotCommit: externalCommit})
	ok(t, err)
	if s := externalStages(t, doc["stages"].([]deliveryview.DeliveryStage))[deliveryview.StageProdRelease]; s.State != deliveryview.StageCurrent {
		t.Errorf("PROD release %+v; want waiting: the workloads do not run this build", s)
	}

	// The current PROD release is passed through for the platform's own repository.
	doc, err = read(externalCommit, &deliveryview.DeliveryProduction{Commit: externalCommit, StartedAt: at.Add(30 * time.Minute), CompletedAt: at.Add(31 * time.Minute)})
	ok(t, err)
	if s := externalStages(t, doc["stages"].([]deliveryview.DeliveryStage))[deliveryview.StageOnProd]; s.State != deliveryview.StageDone || s.Source != "release_record" {
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

// A work that named its commit on a heartbeat has its strip while it runs:
// GET /delivery?commit= answers from the earliest event carrying the commit,
// with no submitted event. Every commit any event of the work named is found,
// each from when it was first named; the step is the work's as bound now.
func TestReadExternalDeliveryFromHeartbeat(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	w := newKernelWorld(t)
	at := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	clock := func(d time.Duration) { t.Cleanup(freezeClock(t, at.Add(d))) }
	read := func(commit string) map[string]any {
		t.Helper()
		doc, err := deliveryreadings.ReadDelivery(ctx, w.rt, deliveryreadings.DeliveryQuery{Commit: commit})
		ok(t, err)
		return doc
	}
	handedIn := func(doc map[string]any) deliveryview.DeliveryStage {
		t.Helper()
		return externalStages(t, doc["stages"].([]deliveryview.DeliveryStage))[deliveryview.StageHandedIn]
	}
	who := wr("thread:giant")
	clock(0)
	out, err := externalwork.RegisterExternalWork(ctx, w.rt, who, externalwork.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "early", Artifact: externalSpec(t, w)})
	ok(t, err)
	id := out["external_work_id"].(string)
	post := func(e externalwork.ExternalEvent) {
		t.Helper()
		_, err := externalwork.PostExternalEvent(ctx, w.rt, who, id, e)
		ok(t, err)
	}
	post(externalwork.ExternalEvent{Key: "start", Type: "started"})
	_, err = deliveryreadings.ReadDelivery(ctx, w.rt, deliveryreadings.DeliveryQuery{Commit: externalCommit})
	is(t, err, pgx.ErrNoRows)

	// started + heartbeat{commit_shas}: the strip is there, handed in at the heartbeat.
	clock(10 * time.Minute)
	post(externalwork.ExternalEvent{Key: "pushed", Type: "heartbeat", Commits: []string{externalCommit}})
	doc := read(externalCommit[:8])
	if doc["commit"] != externalCommit || doc["external_work_id"] != id || doc["plan_node_id"] != s1 || doc["repo"] != "platform" {
		t.Fatalf("response %+v", doc)
	}
	st := externalStages(t, doc["stages"].([]deliveryview.DeliveryStage))
	if s := st[deliveryview.StageHandedIn]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(at.Add(10*time.Minute)) || s.Source != "external_work" || s.Actor != "thread:giant" {
		t.Errorf("handed in %+v; want the heartbeat naming the commit", s)
	}
	if s := st[deliveryview.StageLandingCI]; s.State != deliveryview.StageUnknown || !strings.Contains(s.Reason, "no completed CI run") {
		t.Errorf("CI %+v", s)
	}
	// Its recorded runs tell the stages as they do after a submitted event.
	ci := externalRun("ci-platform-a", "bifrost-ci-platform", "Succeeded", at.Add(12*time.Minute), 7*time.Minute)
	recordRuns(t, w, ci)
	st = externalStages(t, read(externalCommit)["stages"].([]deliveryview.DeliveryStage))
	if s := st[deliveryview.StageHandedIn]; !s.EnteredAt.Equal(at.Add(10*time.Minute)) || !s.LeftAt.Equal(ci.StartedAt) {
		t.Errorf("handed in %+v", s)
	}
	if s := st[deliveryview.StageLandingCI]; s.State != deliveryview.StageDone || !s.LeftAt.Equal(ci.CompletedAt) {
		t.Errorf("CI %+v", s)
	}

	// Later heartbeats that carry the commit along, a new link, a second
	// commit and the submitted event: the first commit keeps its first time,
	// and the answer names the step the work is bound to now.
	clock(20 * time.Minute)
	post(externalwork.ExternalEvent{Key: "alive", Type: "heartbeat"})
	post(externalwork.ExternalEvent{Key: "moved", Type: "linked", Root: root, Node: s2})
	clock(30 * time.Minute)
	post(externalwork.ExternalEvent{Key: "pushed-again", Type: "heartbeat", Commits: []string{externalOther}})
	clock(40 * time.Minute)
	third := strings.Repeat("e", 40)
	post(externalwork.ExternalEvent{Key: "end", Type: "submitted", Commits: []string{third, externalCommit}})
	for commit, named := range map[string]time.Duration{externalOther: 30 * time.Minute, third: 40 * time.Minute} {
		doc = read(commit)
		if s := handedIn(doc); doc["external_work_id"] != id || doc["plan_node_id"] != s2 || !s.EnteredAt.Equal(at.Add(named)) || s.Source != "external_work" {
			t.Errorf("%s: work %v step %v handed in %+v; want named at +%s", commit[:8], doc["external_work_id"], doc["plan_node_id"], s, named)
		}
	}
	doc = read(externalCommit)
	if s := handedIn(doc); doc["external_work_id"] != id || doc["plan_node_id"] != s2 || !s.EnteredAt.Equal(at.Add(10*time.Minute)) {
		t.Errorf("first commit after submitted: step %v handed in %+v", doc["plan_node_id"], s)
	}

	// Another work naming the first commit later changes nothing; one prefix
	// over two of this work's commits is refused naming both.
	clock(50 * time.Minute)
	externalSubmitted(t, w, "thread:other", s1, "early-again", externalCommit)
	if doc = read(externalCommit[:16]); doc["external_work_id"] != id {
		t.Fatalf("same commit named later by another work: %v", doc["external_work_id"])
	}
	_, err = deliveryreadings.ReadDelivery(ctx, w.rt, deliveryreadings.DeliveryQuery{Commit: externalCommit[:12]})
	is(t, err, store.ErrInvalid)
	if !strings.Contains(err.Error(), externalOther) {
		t.Fatalf("ambiguous prefix does not name both: %v", err)
	}
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
