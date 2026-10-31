package stgrelease

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kt "k8s.io/client-go/testing"
)

// TD-460 review regressions (A1, B1, B2). Each one failed on the reviewed
// commit cfd603f4 and passes with the send record and the retry gate.

// hookRuns runs before once, after the window is held and just before the
// Create, and afterPermit once, after the send permit passed and just before
// the POST. Reactors of the fake cluster run under its lock and cannot call
// back into it, so the hooks sit here.
type hookRuns struct {
	SampleRuns
	before, afterPermit func()
}

func (h *hookRuns) CreatePermittedNamedDeliverRun(ctx context.Context, in delivery.NamedDeliverInput, permit func(context.Context) error) (cluster.ActuationResponse, *unstructured.Unstructured, error) {
	if f := h.before; f != nil {
		h.before = nil
		f()
	}
	wrapped := func(ctx context.Context) error {
		if err := permit(ctx); err != nil {
			return err
		}
		if f := h.afterPermit; f != nil {
			h.afterPermit = nil
			f()
		}
		return nil
	}
	return h.SampleRuns.CreatePermittedNamedDeliverRun(ctx, in, wrapped)
}

// hookLedger runs after once, right after a successful Allocate.
type hookLedger struct {
	*sampleLedger
	after func()
}

func (h *hookLedger) Allocate(ctx context.Context, req store.StgSampleAllocation) (store.StgSample, error) {
	x, err := h.sampleLedger.Allocate(ctx, req)
	if f := h.after; err == nil && f != nil {
		h.after = nil
		f()
	}
	return x, err
}

func (w *sampleWorld) freeWindow() {
	w.t.Helper()
	if err := w.core.CoreV1().ConfigMaps(delivery.STGReleaseNamespace).Delete(context.Background(), "bifrost-release-window", metav1.DeleteOptions{}); err != nil {
		w.t.Fatal(err)
	}
}

// waitingAllocation leaves approval with an allocation that never reached a
// send: its first execution found the window held by another release.
func (w *sampleWorld) waitingAllocation(approval string) {
	w.t.Helper()
	w.holdWindow("release-sh-other")
	if _, err := w.create(approval); !errors.Is(err, ErrSampleBusy) {
		w.t.Fatal(err)
	}
	w.freeWindow()
}

// noAbandonedRun is the invariant A1 was about: no abandoned sample has a
// run in cicd, so every run the sample actions created can be finalized.
func (w *sampleWorld) noAbandonedRun() {
	w.t.Helper()
	s, err := w.ledger.Read(context.Background())
	if err != nil {
		w.t.Fatal(err)
	}
	for _, approval := range s.Order {
		x := s.Samples[approval]
		if !x.Abandoned {
			continue
		}
		id, err := SampleInput(*x).Identity()
		if err != nil {
			w.t.Fatal(err)
		}
		if _, err := w.sampler.Runs.ReadRun(context.Background(), id.Name); !apierrors.IsNotFound(err) {
			w.t.Fatalf("abandoned sample of %s has run %s (read: %v)", approval, id.Name, err)
		}
	}
}

// A1: a waiting approval retries after the grace from its allocation. Its
// window is held and its Create is next when a second approval proves its run
// absent and abandons it. The retry's send permit must then refuse, so no run
// exists for an abandoned sample and the second approval can proceed.
func TestSampleAbandonedAllocationIsNeverSent(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.waitingAllocation("A1")
	later := sampleNow.Add(store.StgSampleAbandonGrace + time.Minute)
	w.sampler.Now = func() time.Time { return later }
	w.ledger.at = later
	var a2 error
	w.sampler.Runs = &hookRuns{SampleRuns: w.sampler.Runs, before: func() { _, a2 = w.create("A2") }}
	_, a1 := w.create("A1")
	if !errors.Is(a2, ErrSampleBusy) {
		t.Fatal("A2 abandons A1 and waits for A1's window", a2)
	}
	if a1 == nil || !strings.Contains(a1.Error(), "abandoned") || errors.Is(a1, ErrSampleUncertain) || w.creates != 0 {
		t.Fatalf("A1's send must be refused, not sent: err=%v creates=%d", a1, w.creates)
	}
	w.noAbandonedRun()
	// A1's refused execution ended the window it held, so A2 is not busy
	// and does not wait for A1's TTL.
	if who, _, expires := w.window(); !strings.HasPrefix(who, "stgrel-bifrost-platform-") || expires.After(later) {
		t.Fatal("A1's window still live", who, expires)
	}
	out, err := w.create("A2")
	if err != nil || out.UID == "" || w.creates != 1 {
		t.Fatal("A2 proceeds at once, without busy", out, err)
	}
	w.finish(out.Run, "Succeeded")
	if done, err := w.finalize("F1", out.Run, out.UID); err != nil || !done.Released {
		t.Fatal("A2's run is finalized", done, err)
	}
	w.noAbandonedRun()
}

// Ending the abandoned allocation's window is best effort: when it fails,
// the execution still reports the abandon (a plain failure, not uncertain),
// with a note, and nothing is sent.
func TestSampleAbandonedWindowEndFailureKeepsTheRefusal(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.waitingAllocation("A1")
	later := sampleNow.Add(store.StgSampleAbandonGrace + time.Minute)
	w.sampler.Now = func() time.Time { return later }
	w.ledger.at = later
	w.sampler.Runs = &hookRuns{SampleRuns: w.sampler.Runs, before: func() {
		_, _ = w.create("A2")
		w.core.PrependReactor("update", "configmaps", func(kt.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("apiserver unavailable")
		})
	}}
	_, err := w.create("A1")
	if err == nil || !strings.Contains(err.Error(), "abandoned") || !strings.Contains(err.Error(), "window not ended") || errors.Is(err, ErrSampleUncertain) || w.creates != 0 {
		t.Fatalf("want the abandon refusal with a note, got %v creates=%d", err, w.creates)
	}
	var refusal *store.StgSampleError
	if !errors.As(err, &refusal) || refusal.Code != "abandoned" {
		t.Fatal("the refusal stays typed", err)
	}
}

// A1, other order: the retry's send is recorded first and its POST has not
// landed yet. A GET 404 then is not proof of absence: the grace restarts at
// the send record, so the second approval is refused and the run is kept.
func TestSampleSendRecordRestartsTheAbandonGrace(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.waitingAllocation("A1")
	later := sampleNow.Add(store.StgSampleAbandonGrace + time.Minute)
	w.sampler.Now = func() time.Time { return later }
	w.ledger.at = later
	var a2, forged error
	w.sampler.Runs = &hookRuns{SampleRuns: w.sampler.Runs, afterPermit: func() {
		_, a2 = w.create("A2")
		// A proof read after the send record is refused by the fold under
		// the stream lock, whatever the caller checked.
		_, forged = w.ledger.Allocate(context.Background(), store.StgSampleAllocation{Approval: "A3", P: sampleP, U: sampleU, Infra: sampleInfra,
			Abandon: &store.StgSampleAbsence{Approval: "A1", Run: "stgrel-x", ReadAt: later.Add(time.Minute)}})
	}}
	out, err := w.create("A1")
	if err != nil || out.UID == "" || w.creates != 1 {
		t.Fatal("A1 is created", out, err)
	}
	if a2 == nil || !strings.Contains(a2.Error(), "not proven absent") {
		t.Fatal("A2 must not abandon a just-sent allocation", a2)
	}
	if forged == nil || !strings.Contains(forged.Error(), "30 minutes from its last send") {
		t.Fatal("abandon inside the grace from the send record", forged)
	}
	s, _ := w.ledger.Read(context.Background())
	if x := s.Samples["A1"]; x.Abandoned || !x.SentAt.Equal(later) || s.Samples["A2"] != nil || s.Samples["A3"] != nil {
		t.Fatalf("A1 %+v A2 %+v", x, s.Samples["A2"])
	}
	w.noAbandonedRun()
}

// B1: a retry of a waiting approval passes the same first-opening gate as a
// new sample, at the allocation and again at the send: neither an open rule
// nor a B5 ledger that has opened a batch (rule since closed) lets it send.
func TestSampleRetryKeepsTheFirstOpeningGate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		set  func(*sampleWorld, *hookLedger)
		want string
	}{
		{"B5 opened a batch while it waited", func(w *sampleWorld, _ *hookLedger) { w.ledger.lastBatch = 1 }, "not_first"},
		{"rule opened while it waited", func(w *sampleWorld, _ *hookLedger) { w.ledger.allocOpen = true }, "rule_open"},
		{"B5 opened a batch after the allocation read", func(w *sampleWorld, h *hookLedger) {
			h.after = func() { w.ledger.lastBatch = 1 }
		}, "sample not sent: stg_sample not_first"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			w := newSampleWorld(t)
			h := &hookLedger{sampleLedger: w.ledger}
			w.sampler.Ledger = h
			w.waitingAllocation("A1")
			test.set(w, h)
			out, err := w.create("A1")
			if err == nil || !strings.Contains(err.Error(), test.want) || w.creates != 0 {
				t.Fatalf("want %q without a Create, got out=%+v err=%v creates=%d", test.want, out, err, w.creates)
			}
		})
	}
}

// B2: the POST succeeded but the stored object does not carry the sample's
// identity. Something exists under the deterministic name, so the approval
// goes to unknown with the run and its uid, not to a plain failure.
func TestSampleMismatchAfterCreateIsUncertain(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
		obj := action.(kt.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		obj.SetUID("webhook-uid")
		obj.SetResourceVersion("1")
		obj.SetFinalizers(nil)
		return true, obj, w.dyn.Tracker().Create(samplePRGVR, obj, delivery.STGReleaseNamespace)
	})
	_, err := w.create("approval-mismatch")
	if !errors.Is(err, ErrSampleUncertain) || !strings.Contains(err.Error(), "stgrel-") || !strings.Contains(err.Error(), "webhook-uid") {
		t.Fatal("want an uncertain outcome naming the run and uid", err)
	}
}

// C4: finalize takes only a sample run name (try 1) and a UUID uid, before
// the approval row is written and again at execution.
func TestCheckSampleFinalizeShapes(t *testing.T) {
	t.Parallel()
	good := sampleUIDOf(1)
	if err := CheckSampleFinalize("stg", "stgrel-5007-1-aaaaaaaa", good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{
		{"stgrel-5007-2-aaaaaaaa", good},       // not try 1
		{"stgrel-5007-1-AAAAAAAA", good},       // not lowercase hex
		{"stgrel-x-1-aaaaaaaa", good},          // batch not a number
		{"cicd/stgrel-5007-1-aaaaaaaa", good},  // namespace prefix
		{"stgrel-5007-1-aaaaaaaa", "uid-1"},    // not a UUID
		{"stgrel-5007-1-aaaaaaaa", good + " "}, // trailing space
	} {
		if err := CheckSampleFinalize("stg", bad[0], bad[1]); err == nil {
			t.Errorf("accepted run=%q uid=%q", bad[0], bad[1])
		}
	}
}

// Every path that proves no POST ends the window this execution holds for
// the allocation (same who, same reason), so no one waits for its TTL; the
// error keeps its class. A POST that may have been sent keeps the window.
func TestSampleWindowEndsOnlyWhenNothingWasSent(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		setup     func(*sampleWorld)
		want      string
		uncertain bool
	}{
		{"send permit refuses rule_open", func(w *sampleWorld) { w.ledger.sendOpen = true }, "sample not sent: stg_sample rule_open", false},
		{"send permit refuses not_first", func(w *sampleWorld) {
			h := &hookLedger{sampleLedger: w.ledger, after: func() { w.ledger.lastBatch = 1 }}
			w.sampler.Ledger = h
		}, "sample not sent: stg_sample not_first", false},
		{"Pipeline read fails before the permit", func(w *sampleWorld) {
			w.dyn.PrependReactor("get", "pipelines", func(kt.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewServiceUnavailable("apiserver restarting")
			})
		}, "apiserver restarting", false},
		{"POST lost and readback fails", func(w *sampleWorld) {
			w.dyn.PrependReactor("create", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewServiceUnavailable("apiserver restarting")
			})
		}, "stgrel-", true},
		{"POST stored a mismatching object", func(w *sampleWorld) {
			w.dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
				obj := action.(kt.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
				obj.SetUID(types.UID(sampleUIDOf(7)))
				obj.SetResourceVersion("1")
				obj.SetFinalizers(nil)
				return true, obj, w.dyn.Tracker().Create(samplePRGVR, obj, delivery.STGReleaseNamespace)
			})
		}, sampleUIDOf(7), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			w := newSampleWorld(t)
			test.setup(w)
			_, err := w.create("approval-window")
			if err == nil || !strings.Contains(err.Error(), test.want) || errors.Is(err, ErrSampleUncertain) != test.uncertain || errors.Is(err, ErrSampleBusy) {
				t.Fatalf("want %q (uncertain=%v), got %v", test.want, test.uncertain, err)
			}
			who, _, expires := w.window()
			if !strings.HasPrefix(who, "stgrel-bifrost-platform-") {
				t.Fatal("window holder", who)
			}
			if live := expires.After(sampleNow); live != test.uncertain {
				t.Fatalf("window live=%v, want %v (expires %s)", live, test.uncertain, expires)
			}
		})
	}
}

// The end checks the reason too: the same who renewed under another reason
// (release_window_hold) is left alone, and the refusal is unchanged.
func TestSampleUnsentWindowEndChecksTheReason(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.ledger.sendOpen = true
	w.sampler.Runs = &hookRuns{SampleRuns: w.sampler.Runs, before: func() {
		who, _, _ := w.window()
		if _, err := w.sampler.Runs.(*hookRuns).SampleRuns.HoldSampleWindow(context.Background(), who, "renewed by release_window_hold"); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := w.create("approval-renewed")
	if err == nil || !strings.Contains(err.Error(), "rule_open") || strings.Contains(err.Error(), "window not ended") {
		t.Fatal(err)
	}
	if _, _, expires := w.window(); !expires.After(sampleNow) {
		t.Fatal("a window renewed under another reason was ended")
	}
}
