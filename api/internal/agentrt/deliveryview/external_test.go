package deliveryview_test

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
)

const (
	externalCommit = "9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d"
	externalOther  = "9d9d9d9d9d9d0000000000000000000000000000"
)

// externalRun is a recorded pipeline run of externalCommit.
func externalRun(run, pipeline, result string, start time.Time, took time.Duration) deliveryview.Run {
	kind := "delivery"
	if strings.HasPrefix(pipeline, "bifrost-ci-") {
		kind = "ci"
	}
	return deliveryview.Run{Run: run, Pipeline: pipeline, Kind: kind, Result: result,
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
	in := func(runs ...deliveryview.Run) deliveryview.ExternalDelivery {
		if runs == nil {
			runs = []deliveryview.Run{}
		}
		return deliveryview.ExternalDelivery{Work: "ew_1", Registrant: "thread:giant", Repo: "bifrost-platform", Commit: externalCommit, NamedAt: submitted, Runs: runs}
	}

	t.Run("all the way to PROD", func(t *testing.T) {
		st := externalStages(t, deliveryview.FoldExternalDelivery(in(red, ci, reuse, stgRed, stg, prod)))
		// Handed in when the commit first reached the platform: its first run, before the submitted event.
		if s := st[deliveryview.StageHandedIn]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(red.CreatedAt) || s.Source != "pipeline_seen" || s.Actor != "thread:giant" || s.ActorRole != "executor" {
			t.Errorf("handed in %+v", s)
		}
		if s := st[deliveryview.StageLandingCI]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(red.StartedAt) || !s.LeftAt.Equal(ci.CompletedAt) || s.Reason != "" || s.WaitsOn != "" {
			t.Errorf("CI %+v", s)
		}
		for _, name := range []string{deliveryview.StageOwnerWait, deliveryview.StageOnMain} {
			if s := st[name]; s.State != deliveryview.StageUnknown || !s.EnteredAt.IsZero() || !s.LeftAt.IsZero() {
				t.Errorf("%s %+v; the platform does not record the merge", name, s)
			}
		}
		if s := st[deliveryview.StageSTG]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(stgRed.StartedAt) || !s.LeftAt.Equal(stg.CompletedAt) {
			t.Errorf("STG %+v", s)
		}
		if s := st[deliveryview.StageProdRelease]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(prod.StartedAt) || !s.LeftAt.Equal(prod.CompletedAt) {
			t.Errorf("PROD release %+v", s)
		}
		if s := st[deliveryview.StageOnProd]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(prod.CompletedAt) || s.Source != "pipeline_seen" {
			t.Errorf("on PROD %+v", s)
		}
	})

	t.Run("CI green, waiting for STG", func(t *testing.T) {
		st := externalStages(t, deliveryview.FoldExternalDelivery(in(ci)))
		if s := st[deliveryview.StageSTG]; s.State != deliveryview.StageCurrent || s.WaitsOn != "STG release" || !s.EnteredAt.IsZero() {
			t.Errorf("STG %+v", s)
		}
		for _, name := range []string{deliveryview.StageProdRelease, deliveryview.StageOnProd} {
			if s := st[name]; s.State != deliveryview.StageNotReached || s.WaitsOn != "" {
				t.Errorf("%s %+v", name, s)
			}
		}
	})

	t.Run("CI failed and STG delivery failed say which run", func(t *testing.T) {
		st := externalStages(t, deliveryview.FoldExternalDelivery(in(red)))
		if s := st[deliveryview.StageLandingCI]; s.State != deliveryview.StageCurrent || s.Reason != "CI failed: ci-platform-red" || !s.LeftAt.IsZero() {
			t.Errorf("CI %+v", s)
		}
		if s := st[deliveryview.StageSTG]; s.State != deliveryview.StageNotReached {
			t.Errorf("STG %+v", s)
		}
		st = externalStages(t, deliveryview.FoldExternalDelivery(in(ci, stgRed)))
		if s := st[deliveryview.StageSTG]; s.State != deliveryview.StageCurrent || s.Reason != "STG delivery failed: bifrost-deliver-platform-0" {
			t.Errorf("STG %+v", s)
		}
	})

	t.Run("no run recorded: CI is unknown, and handed in is the submitted event", func(t *testing.T) {
		st := externalStages(t, deliveryview.FoldExternalDelivery(in()))
		if s := st[deliveryview.StageHandedIn]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(submitted) || s.Source != "external_work" || !s.LeftAt.IsZero() {
			t.Errorf("handed in %+v", s)
		}
		if s := st[deliveryview.StageLandingCI]; s.State != deliveryview.StageUnknown || !strings.Contains(s.Reason, "no completed CI run") {
			t.Errorf("CI %+v", s)
		}
	})

	t.Run("runs not read: every stage they tell is unknown", func(t *testing.T) {
		d := in()
		d.Runs = nil
		st := externalStages(t, deliveryview.FoldExternalDelivery(d))
		for _, name := range []string{deliveryview.StageLandingCI, deliveryview.StageSTG, deliveryview.StageProdRelease, deliveryview.StageOnProd} {
			if s := st[name]; s.State != deliveryview.StageUnknown {
				t.Errorf("%s %+v", name, s)
			}
		}
	})

	t.Run("a run of another revision does not count", func(t *testing.T) {
		later := stg
		later.Revision = externalOther
		st := externalStages(t, deliveryview.FoldExternalDelivery(in(ci, later)))
		if s := st[deliveryview.StageSTG]; s.State != deliveryview.StageCurrent || !s.EnteredAt.IsZero() {
			t.Errorf("STG %+v", s)
		}
	})

	t.Run("another repository's delivery is unknown, not waiting", func(t *testing.T) {
		d := in(ci)
		d.Repo = "bifrost-trade-api"
		st := externalStages(t, deliveryview.FoldExternalDelivery(d))
		for _, name := range []string{deliveryview.StageSTG, deliveryview.StageProdRelease, deliveryview.StageOnProd} {
			if s := st[name]; s.State != deliveryview.StageUnknown || !strings.Contains(s.Reason, "bifrost-trade-api") {
				t.Errorf("%s %+v", name, s)
			}
		}
	})

	t.Run("the current PROD release names the commit before its run is recorded", func(t *testing.T) {
		d := in(ci, stg)
		d.Production = &deliveryview.DeliveryProduction{Commit: externalCommit, StartedAt: prod.StartedAt, CompletedAt: prod.CompletedAt}
		st := externalStages(t, deliveryview.FoldExternalDelivery(d))
		if s := st[deliveryview.StageOnProd]; s.State != deliveryview.StageDone || !s.EnteredAt.Equal(prod.CompletedAt) || s.Source != "release_record" {
			t.Errorf("on PROD %+v", s)
		}
	})

	// STG delivered the commit and no PROD run of it is recorded yet (a run is
	// recorded minutes after it ends). Waiting is said only when what PROD
	// runs was read; otherwise the PROD stages are unknown with why.
	t.Run("no PROD run recorded: waiting only when what PROD runs was read", func(t *testing.T) {
		d := in(ci, stg)
		waits := func(name string, p *deliveryview.DeliveryProduction) {
			t.Helper()
			d.Production = p
			st := externalStages(t, deliveryview.FoldExternalDelivery(d))
			if s := st[deliveryview.StageProdRelease]; s.State != deliveryview.StageCurrent || s.WaitsOn != "PROD release" {
				t.Errorf("%s: PROD release %+v", name, s)
			}
			if s := st[deliveryview.StageOnProd]; s.State != deliveryview.StageNotReached {
				t.Errorf("%s: on PROD %+v", name, s)
			}
		}
		unknown := func(name string, p *deliveryview.DeliveryProduction, why string) {
			t.Helper()
			d.Production = p
			st := externalStages(t, deliveryview.FoldExternalDelivery(d))
			for _, stage := range []string{deliveryview.StageProdRelease, deliveryview.StageOnProd} {
				if s := st[stage]; s.State != deliveryview.StageUnknown || !strings.Contains(s.Reason, why) || s.WaitsOn != "" {
					t.Errorf("%s: %s %+v; want unknown naming %q", name, stage, s, why)
				}
			}
		}
		waits("the current release is another commit", &deliveryview.DeliveryProduction{Commit: externalOther, StartedAt: prod.StartedAt, CompletedAt: prod.CompletedAt})
		waits("the workloads do not run this commit's build", &deliveryview.DeliveryProduction{Reason: "current STG evidence does not match the promoted PROD revision", NotCommit: externalCommit})
		unknown("the current release was not read", nil, "was not read")
		unknown("the workloads could not be read", &deliveryview.DeliveryProduction{Reason: "PROD workloads are unavailable or unsettled"}, "PROD workloads are unavailable or unsettled")
		unknown("the workloads already run the STG build", &deliveryview.DeliveryProduction{Reason: "PROD workloads run the newest STG build, and no PROD release record names it yet"}, "no PROD release record names it yet")
		unknown("the workloads do not run another commit's build", &deliveryview.DeliveryProduction{Reason: "current STG evidence does not match the promoted PROD revision", NotCommit: externalOther}, "does not match")

		// Before STG delivered it, PROD is simply not reached; a failed PROD run is its own fact.
		d = in(ci)
		d.Production = &deliveryview.DeliveryProduction{Reason: "PROD workloads are unavailable or unsettled"}
		if s := externalStages(t, deliveryview.FoldExternalDelivery(d))[deliveryview.StageProdRelease]; s.State != deliveryview.StageNotReached {
			t.Errorf("before STG: PROD release %+v", s)
		}
		failed := prod
		failed.Result = "Failed"
		d = in(ci, stg, failed)
		if s := externalStages(t, deliveryview.FoldExternalDelivery(d))[deliveryview.StageProdRelease]; s.State != deliveryview.StageCurrent || !strings.Contains(s.Reason, "PROD delivery failed") {
			t.Errorf("failed PROD run: PROD release %+v", s)
		}
	})
}
