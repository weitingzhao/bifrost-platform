package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

func TestReview711CreateCanCompleteAfterPermitDeadline(t *testing.T) {
	t.Parallel()
	a, dyn, _ := stgAdapter(t)
	deadline := stgTestNow.Add(time.Second)
	now := deadline.Add(-time.Millisecond)
	a.Now = func() time.Time { return now }
	in := stgInput()
	id, err := in.Identity()
	if err != nil {
		t.Fatal(err)
	}
	tr := &store.StgTry{
		Started: store.StgEvent{Seq: in.IntentSeq},
		Permit:  store.StgEvent{Seq: in.IntentSeq, Record: store.StgRecord{"deadline": deadline.Format(time.RFC3339Nano)}},
	}
	control := store.StgRunObservation{IntentSeq: in.IntentSeq, PermitSeq: in.IntentSeq, Kind: "found", UID: "control", ReadAt: now}
	if got := store.StgRecoveryDecision(tr, control, now); got != "bound" {
		t.Fatal("same-entry found control", got)
	}
	checks, creates := 0, 0
	dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
		// Inside the actual POST boundary, before the object is stored.
		// Advance logical time by 2ms, within the adapter's 10s timeout.
		now = deadline.Add(time.Millisecond)
		_, err := dyn.Tracker().Get(pipelineRunGVR, STGReleaseNamespace, id.Name)
		if !apierrors.IsNotFound(err) {
			t.Fatal("expected genuine absence", err)
		}
		obs := store.StgRunObservation{IntentSeq: in.IntentSeq, PermitSeq: in.IntentSeq, Kind: "not_found", ReadAt: now, FinalizerGuaranteed: true}
		if got := store.StgRecoveryDecision(tr, obs, now); got != "unknown_execution" {
			t.Fatalf("in-flight POST can still succeed: recovery=%s, want unknown_execution", got)
		}
		// Isolation retains the slot while this already accepted request completes.
		obj := action.(kt.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		obj.SetUID("late-run")
		if err := dyn.Tracker().Create(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
			t.Fatal(err)
		}
		creates++
		return true, obj, nil
	})
	_, obj, err := a.CreatePermittedNamedDeliverRun(context.Background(), in, func(context.Context) error {
		checks++
		if !now.Before(deadline) {
			t.Fatal("permit was not current at dispatch")
		}
		return nil
	})
	if err != nil || checks != 1 || creates != 1 || string(obj.GetUID()) != "late-run" {
		t.Fatal("probe did not execute actual adapter path", err, checks, creates)
	}
	t.Logf("actual adapter created UID %s at deadline+%s; post-deadline 404 retained execution occupancy", obj.GetUID(), now.Sub(deadline))
}
