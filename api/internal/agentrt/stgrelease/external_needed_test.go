package stgrelease

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// openBlock opens a why block of P a against controlFixture's trusted
// baseline, as Pass's gate would.
func openBlock(t *testing.T, l *memoryLedger, why string) string {
	t.Helper()
	s := l.state(t)
	base, ok := store.StgTrustedBaseline(s)
	if !ok {
		t.Fatal("fixture has no trusted baseline")
	}
	set := []any{memberValue(controlMember)}
	if b := s.Batches[leaseBatch]; b != nil {
		set = members(b)
	}
	r, err := store.StgBlockedRecord(sha("a"), why, base, l.clock.Now(), why+"-version", map[string]any{"members": set})
	if err != nil {
		t.Fatal(err)
	}
	l.add(t, r)
	return r.String("block")
}

// manualRuns lists runs of P a created before and after controlFixture's
// trusted baseline was deployed.
func manualRuns(t *testing.T, c *Controller, l *memoryLedger, r *controlRuns) *externalRuns {
	t.Helper()
	at := store.StgDeployedAt(l.state(t), "cluster/cicd/base-uid")
	input := delivery.NamedDeliverInput{Batch: 2, Try: 1, IntentSeq: 30, P: sha("a"), U: sha("b"), Infra: sha("c")}
	before, after := controlObservationUID(t, input, "Succeeded", "before-uid"), controlObservationUID(t, input, "Succeeded", "after-uid")
	manualRun(before, at.Add(-time.Hour))
	manualRun(after, at.Add(time.Second))
	x := &externalRuns{controlRuns: r, listed: []*unstructured.Unstructured{before.Run, after.Run},
		byUID: map[string]delivery.STGRunObservation{"before-uid": before, "after-uid": after, "run-uid": r.obs}}
	c.Runs = x
	return x
}

// R5:5.7-11: with a trusted baseline and no block a manual run can clear,
// ExternalPass makes no cluster read at all.
func TestExternalNotNeededReadsNoRuns(t *testing.T) {
	t.Parallel()
	for _, why := range []string{"none", "ci_red", "budget_exhausted"} {
		t.Run(why, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Succeeded")
			if why != "none" {
				openBlock(t, l, why)
			}
			x := manualRuns(t, c, l, r)
			if needed, _ := manualRunsNeeded(l.state(t)); needed {
				t.Fatal("manual runs needed")
			}
			before := l.state(t).LastSeq
			externalPass(t, c)
			if x.lists != 0 || x.observations != 0 || x.workReads != 0 || l.state(t).LastSeq != before {
				t.Fatal("cluster reads", x.lists, x.observations, x.workReads)
			}
		})
	}
}

// An OPEN batch may fail before recording a blocker (for example when its
// historical Git baseline cannot be read). A successful normal delivery must
// still be discovered; observing it must not send another deployment.
func TestExternalOpenBatchDiscoversSuccessfulRecovery(t *testing.T) {
	t.Parallel()
	c, l, r, _ := controlFixture(t, "Succeeded")
	l.entries = l.entries[:4] // baseline, opened, p_selected, ci_accepted
	x := manualRuns(t, c, l, r)
	l.clock.Step(time.Minute)
	if needed, _ := manualRunsNeeded(l.state(t)); !needed {
		t.Fatal("open batch must observe newer manual delivery without a blocker")
	}
	externalPass(t, c)
	s := l.state(t)
	if x.observations != 1 || s.Evidence["cluster/cicd/after-uid"].Seq == 0 {
		t.Fatal("successful recovery was not recorded")
	}
	if base, ok := store.StgTrustedBaseline(s); !ok || base.String("P") != sha("a") {
		t.Fatal("new deployed baseline not adopted", base)
	}
	if needed, _ := manualRunsNeeded(s); needed {
		t.Fatal("same selected revision must not keep scanning after observation")
	}
	before := s.LastSeq
	externalPass(t, c)
	if x.lists != 1 || x.observations != 1 || l.state(t).LastSeq != before || s.Batches[leaseBatch].Try != 0 {
		t.Fatal("repeated observation or deployment started")
	}
}

// A baseline_moved block waits on a manual run: one created after the
// baseline is archived and becomes the trusted baseline; one created before
// it is never observed.
func TestExternalBaselineMovedArchivesOnlyNewerRuns(t *testing.T) {
	t.Parallel()
	c, l, r, _ := controlFixture(t, "Succeeded")
	openBlock(t, l, "baseline_moved")
	x := manualRuns(t, c, l, r)
	l.clock.Step(time.Minute)
	externalPass(t, c)
	s := l.state(t)
	if x.lists != 1 || x.observations != 1 || s.Evidence["cluster/cicd/before-uid"].Seq != 0 || s.Evidence["cluster/cicd/after-uid"].Record.String("source") != "external" {
		t.Fatal("observed", x.observations, s.Evidence)
	}
	if base, ok := store.StgTrustedBaseline(s); !ok || base.String("evidence") != "cluster/cicd/after-uid" {
		t.Fatal("trusted baseline", base)
	}
	externalPass(t, c)
	if x.observations != 1 {
		t.Fatal("observed again", x.observations)
	}
}

// A hard_floor block is lifted only by unblocked{external_release}, after
// a manual run of the blocked P.
func TestExternalHardFloorLiftedByManualRun(t *testing.T) {
	t.Parallel()
	c, l, r, _ := controlFixture(t, "Succeeded")
	l.entries = l.entries[:1] // the trusted baseline only: no batch holds Pass
	block := openBlock(t, l, "hard_floor")
	x := manualRuns(t, c, l, r)
	l.clock.Step(time.Minute)
	if needed, _ := manualRunsNeeded(l.state(t)); !needed {
		t.Fatal("hard_floor does not wait on a manual run")
	}
	externalPass(t, c)
	pass(t, c)
	s := l.state(t)
	b := s.Blocks[block]
	if b.IsOpen() || b.Closed.Record.String("why") != "external_release" || b.Closed.Record.String("ref") != "cluster/cicd/after-uid" {
		t.Fatal("block", b.Closed.Record)
	}
	if x.observations != 1 {
		t.Fatal("observed", x.observations)
	}
	if needed, _ := manualRunsNeeded(s); needed {
		t.Fatal("still needed after the block lifted")
	}
}

// With no evidence there is no baseline: any manual run is archived.
func TestExternalBootstrapArchivesManualRun(t *testing.T) {
	t.Parallel()
	c, l, r, _ := externalFixture(t)
	if needed, at := manualRunsNeeded(l.state(t)); !needed || !at.IsZero() {
		t.Fatal("no baseline", needed, at)
	}
	externalPass(t, c)
	s := l.state(t)
	if r.lists != 1 || s.Evidence["cluster/cicd/manual-uid"].Record.String("source") != "external" {
		t.Fatal("bootstrap", r.lists, s.Evidence)
	}
	if base, ok := store.StgTrustedBaseline(s); !ok || base.String("evidence") != "cluster/cicd/manual-uid" {
		t.Fatal("trusted baseline", base)
	}
	externalPass(t, c)
	if r.lists != 1 {
		t.Fatal("listed with a baseline and no block", r.lists)
	}
}
