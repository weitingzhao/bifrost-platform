package stgrelease

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kt "k8s.io/client-go/testing"
)

// TD-460 landing review P1: a permitted POST whose response is lost may be
// stored by the apiserver after the client's deadline, and after a GET 404.

// lostPost makes the first sample POST time out on the client and keeps the
// object the apiserver "stores later"; commit stores it.
type lostPost struct {
	w   *sampleWorld
	obj *unstructured.Unstructured
}

func newLostPost(w *sampleWorld) *lostPost {
	l := &lostPost{w: w}
	w.dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
		if l.obj != nil {
			return false, nil, nil
		}
		l.obj = action.(kt.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		l.obj.SetUID(types.UID(sampleUIDOf(100)))
		l.obj.SetResourceVersion("1")
		return true, nil, apierrors.NewTimeoutError("request did not complete within the client deadline", 0)
	})
	return l
}

func (l *lostPost) commit() {
	l.w.t.Helper()
	if err := l.w.dyn.Tracker().Create(samplePRGVR, l.obj, delivery.STGReleaseNamespace); err != nil {
		l.w.t.Fatal(err)
	}
}

func (w *sampleWorld) at(t time.Time) {
	w.sampler.Now = func() time.Time { return t }
	w.ledger.at = t
}

// A1 sends, its POST is lost and the approval ends unknown. A 404 two minutes
// later is no proof: the allocation stays occupied, so no replacement is
// allocated. When the POST is stored after that 404, the run is A1's and is
// finalized under A1's allocation; only then does a new sample proceed.
func TestSampleLatePostAfterTheGrace404KeepsTheAllocation(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	late := newLostPost(w)
	_, err := w.create("A1")
	if !errors.Is(err, ErrSampleUncertain) || late.obj == nil {
		t.Fatal("A1's POST is lost and uncertain", err)
	}
	w.at(sampleNow.Add(store.StgSampleAbandonGrace + time.Minute))
	if _, err = w.create("A2"); err == nil || !strings.Contains(err.Error(), "not proven absent") {
		t.Fatal("a 404 after the client deadline does not abandon a sent allocation", err)
	}
	late.commit() // the apiserver stores A1's POST after that 404
	if _, err = w.create("A2"); err == nil || !strings.Contains(err.Error(), "held STG release run already exists") {
		t.Fatal("A1's late run blocks a new sample", err)
	}
	s, _ := w.ledger.Read(context.Background())
	if s.Samples["A1"].Abandoned || s.Samples["A2"] != nil || w.creates != 0 {
		t.Fatalf("A1 %+v A2 %+v creates=%d", s.Samples["A1"], s.Samples["A2"], w.creates)
	}
	w.finish(late.obj.GetName(), "Succeeded")
	done, err := w.finalize("F1", late.obj.GetName(), sampleUIDOf(100))
	if err != nil || !done.Released || done.SampleApproval != "A1" || done.Abandoned || !done.WindowEnded {
		t.Fatal("A1's late run is finalized under its allocation", done, err)
	}
	out, err := w.create("A2")
	if err != nil || out.UID == "" || w.creates != 1 {
		t.Fatal("a new sample after A1 is finalized", out, err)
	}
}

// Positive control for the stronger proof: with no late run, a 404 read 30
// minutes after the last send record abandons the allocation; 29 minutes is
// still too early.
func TestSampleSentAllocationIsAbandonedAfterTheApiserverBound(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	newLostPost(w)
	if _, err := w.create("A1"); !errors.Is(err, ErrSampleUncertain) {
		t.Fatal(err)
	}
	w.at(sampleNow.Add(store.StgSampleAbandonAfterSend - time.Minute))
	if _, err := w.create("A2"); err == nil || !strings.Contains(err.Error(), "not proven absent") {
		t.Fatal("29 minutes after the send", err)
	}
	w.at(sampleNow.Add(store.StgSampleAbandonAfterSend))
	if _, err := w.create("A2"); !errors.Is(err, ErrSampleBusy) {
		t.Fatal("A2 abandons A1, then waits for A1's window", err)
	}
	s, _ := w.ledger.Read(context.Background())
	if !s.Samples["A1"].Abandoned || s.Open() != s.Samples["A2"] {
		t.Fatalf("A1 %+v", s.Samples["A1"])
	}
	w.freeWindow()
	if out, err := w.create("A2"); err != nil || out.UID == "" {
		t.Fatal("A2 proceeds", out, err)
	}
}

// Defence in depth: if the apiserver bound fails and A1's POST is stored
// after A1 was abandoned and A2 created its own run, Finalize adopts A1's run
// (same name, intent annotation and hold), archives it and removes only its
// hold. A2's run, hold, sample and window are untouched, and A2 finalizes
// normally.
func TestSampleFinalizeAdoptsALateRunOfAnAbandonedAllocation(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	late := newLostPost(w)
	if _, err := w.create("A1"); !errors.Is(err, ErrSampleUncertain) {
		t.Fatal(err)
	}
	w.at(sampleNow.Add(store.StgSampleAbandonAfterSend + time.Minute))
	if _, err := w.create("A2"); !errors.Is(err, ErrSampleBusy) {
		t.Fatal(err)
	}
	w.freeWindow()
	a2, err := w.create("A2")
	if err != nil || a2.UID == "" {
		t.Fatal(a2, err)
	}
	late.commit()
	w.finish(late.obj.GetName(), "Succeeded")

	// A run that lost its hold is not adopted as a late sample.
	stripped := w.run(late.obj.GetName())
	stripped.SetFinalizers(nil)
	if err := w.dyn.Tracker().Update(samplePRGVR, stripped, delivery.STGReleaseNamespace); err != nil {
		t.Fatal(err)
	}
	if _, err := w.finalize("F0", late.obj.GetName(), sampleUIDOf(100)); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatal("adoption without the hold", err)
	}
	stripped.SetFinalizers([]string{delivery.STGReleaseFinalizer})
	if err := w.dyn.Tracker().Update(samplePRGVR, stripped, delivery.STGReleaseNamespace); err != nil {
		t.Fatal(err)
	}

	done, err := w.finalize("F1", late.obj.GetName(), sampleUIDOf(100))
	if err != nil || !done.Released || !done.Abandoned || done.SampleApproval != "A1" || done.WindowEnded {
		t.Fatal("A1's late run adopted and released, A2's window left alone", done, err)
	}
	if slices.Contains(w.run(late.obj.GetName()).GetFinalizers(), delivery.STGReleaseFinalizer) {
		t.Fatal("A1's hold not removed")
	}
	if !slices.Contains(w.run(a2.Run).GetFinalizers(), delivery.STGReleaseFinalizer) {
		t.Fatal("A2's hold was touched")
	}
	if who, _, expires := w.window(); who != a2.Who || !expires.After(sampleNow) {
		t.Fatal("A2's window was touched", who, expires)
	}
	s, _ := w.ledger.Read(context.Background())
	if s.Open() != s.Samples["A2"] || s.Samples["A2"].Archived {
		t.Fatalf("A2 stays the open sample %+v", s.Samples["A2"])
	}
	w.finish(a2.Run, "Succeeded")
	if done, err := w.finalize("F2", a2.Run, a2.UID); err != nil || !done.Released || !done.WindowEnded {
		t.Fatal("A2 finalizes normally", done, err)
	}
}
