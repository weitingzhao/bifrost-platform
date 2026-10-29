package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	clocktesting "k8s.io/utils/clock/testing"
)

const leaseBatch = "stg:bifrost-platform:1"

// The real ConfigMap CAS contract is exercised in delivery/stg_window_test.
// This transport additionally controls request ordering and ambiguous replies
// without sockets, sleeps, database locks, or a production control loop.
type leaseWindows struct {
	mu                                  sync.Mutex
	clock                               *clocktesting.FakeClock
	w                                   delivery.STGWindow
	version, reads, takes, renews, ends int
	operations                          chan string
	beforeTake, beforeRenew             func(context.Context)
	renewErr, readErr                   error
}

func (f *leaseWindows) bounded(ctx context.Context) {
	d, ok := ctx.Deadline()
	if !ok || time.Until(d) > 10*time.Second {
		panic("window I/O is not bounded to ten seconds")
	}
}
func (f *leaseWindows) ReadSTGWindow(ctx context.Context) (delivery.STGWindow, error) {
	f.bounded(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.w, f.readErr
}
func (f *leaseWindows) TakeSTGWindow(ctx context.Context, old delivery.STGWindow, who string, gen int64, recover bool) (delivery.STGWindow, error) {
	f.bounded(ctx)
	if f.beforeTake != nil {
		f.beforeTake(ctx)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.takes++
	if f.w.ResourceVersion != old.ResourceVersion {
		return delivery.STGWindow{}, &delivery.WindowChanged{}
	}
	if old.Live(f.clock.Now()) && (!recover || old.Who != who || old.What != "bifrost-platform" || old.Env != "stg") {
		return delivery.STGWindow{}, &delivery.WindowHeld{Who: old.Who}
	}
	f.version++
	f.w = delivery.STGWindow{Who: who, What: "bifrost-platform", Env: "stg", Generation: gen, ResourceVersion: fmt.Sprint(f.version), Expires: f.clock.Now().Add(5 * time.Minute)}
	return f.w, nil
}
func (f *leaseWindows) RenewSTGWindow(ctx context.Context, old delivery.STGWindow) (delivery.STGWindow, error) {
	f.bounded(ctx)
	if f.beforeRenew != nil {
		f.beforeRenew(ctx)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renews++
	if old.ResourceVersion != f.w.ResourceVersion {
		return delivery.STGWindow{}, &delivery.WindowChanged{}
	}
	f.version++
	f.w.ResourceVersion, f.w.Expires = fmt.Sprint(f.version), f.clock.Now().Add(5*time.Minute)
	f.operations <- "renewed"
	return f.w, f.renewErr
}
func (f *leaseWindows) EndSTGWindow(ctx context.Context, old delivery.STGWindow) error {
	f.bounded(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ends++
	if old.ResourceVersion != f.w.ResourceVersion {
		return &delivery.WindowChanged{}
	}
	f.version++
	f.w.ResourceVersion, f.w.Expires = fmt.Sprint(f.version), f.clock.Now()
	f.operations <- "ended"
	return nil
}
func (f *leaseWindows) snapshot() (delivery.STGWindow, [4]int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.w, [4]int{f.reads, f.takes, f.renews, f.ends}
}

func leaseFixture() (WindowConfig, *leaseWindows, *atomic.Pointer[WindowPolicy]) {
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	c := clocktesting.NewFakeClock(at)
	f := &leaseWindows{clock: c, operations: make(chan string, 100)}
	p := &atomic.Pointer[WindowPolicy]{}
	p.Store(&WindowPolicy{Environment: "prod", Live: true, Rule: &rules.Rule{Header: rules.Header{Version: 1, Effective: at.Add(-time.Hour), Expires: at.Add(time.Hour)}, Kind: rules.KindLanding, Landing: &rules.Landing{Repos: []rules.LandingRepo{{Repo: "bifrost-platform", STGRelease: delivery.STGReleasePipeline}}}}})
	return WindowConfig{Windows: f, Policy: func() WindowPolicy { return *p.Load() }, Clock: c}, f, p
}
func leaseState(started bool) store.StgState {
	b := &store.StgBatch{Phase: "OPEN", Tries: map[int64]*store.StgTry{}, Diagnostics: map[string]store.StgEvent{}}
	if started {
		lease := map[string]any{"who": "stgrel-bifrost-platform-1-1", "gen": int64(1)}
		e := store.StgEvent{Seq: 5, Record: store.StgRecord{"lease": lease}}
		b.Phase, b.Try = "STARTED", 1
		b.Tries[1] = &store.StgTry{Started: e, Permit: e}
	}
	return store.StgState{Batches: map[string]*store.StgBatch{leaseBatch: b}}
}
func awaitLease[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case value := <-c:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("lease operation did not finish")
	}
	var zero T
	return zero
}
func holdLease(t *testing.T, cfg WindowConfig, s store.StgState) *WindowHolder {
	t.Helper()
	h, err := HoldWindow(context.Background(), cfg, s, leaseBatch, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Release() })
	return h
}

func TestWindowPolicyNoneDoesNoIO(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"enabled", "none", "live off", "stg", "missing", "expired", "future", "unsupported", "missing repo"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg, f, policy := leaseFixture()
			p := policy.Load()
			switch mode {
			case "none":
				p.Rule.Landing.Repos[0].STGRelease = "none"
			case "live off":
				p.Live = false
			case "stg":
				p.Environment = "stg"
			case "missing":
				p.Rule = nil
			case "expired":
				p.Rule.Expires = cfg.Clock.Now()
			case "future":
				p.Rule.Effective = cfg.Clock.Now().Add(time.Minute)
			case "unsupported":
				p.Rule.Landing.Repos[0].STGRelease = "other"
			case "missing repo":
				p.Rule.Landing.Repos = nil
			}
			h, err := HoldWindow(context.Background(), cfg, leaseState(false), leaseBatch, 1)
			if mode == "enabled" {
				if err != nil || h.Generation() != 1 {
					t.Fatal("same-entry positive control", err)
				}
				if err := h.Release(); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrWindowDisabled) || h != nil {
				t.Fatal(h, err)
			}
			f.clock.Step(10 * time.Minute)
			_, counts := f.snapshot()
			if mode == "enabled" {
				if counts != [4]int{1, 1, 0, 1} {
					t.Fatal(counts)
				}
			} else if counts != [4]int{} {
				t.Fatal("disabled policy touched window", counts)
			}
		})
	}
}

func TestWindowRenewsThenPolicyChangeStops(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"release", "none", "live off", "expired"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg, f, policy := leaseFixture()
			h := holdLease(t, cfg, leaseState(false))
			f.clock.Step(time.Minute)
			if op := awaitLease(t, f.operations); op != "renewed" {
				t.Fatal(op)
			}
			obs, err := h.Observe(context.Background())
			if err != nil || obs.Generation != 1 || !obs.Expires.Equal(f.clock.Now().Add(5*time.Minute)) {
				t.Fatal(obs, err)
			}
			p := *policy.Load()
			r := *p.Rule
			l := *r.Landing
			l.Repos = append([]rules.LandingRepo(nil), l.Repos...)
			r.Landing, p.Rule = &l, &r
			switch mode {
			case "none":
				l.Repos[0].STGRelease = "none"
			case "live off":
				p.Live = false
			case "expired":
				r.Expires = f.clock.Now()
			}
			policy.Store(&p)
			if mode == "release" {
				if err := h.Release(); err != nil {
					t.Fatal(err)
				}
			} else {
				if h.Generation() != 0 {
					t.Fatal("policy change left sends enabled")
				}
				f.clock.Step(time.Minute)
				awaitLease(t, h.Done())
			}
			f.clock.Step(time.Hour)
			w, counts := f.snapshot()
			if w.Live(f.clock.Now()) || counts[2] != 1 || counts[3] != 1 || h.Generation() != 0 {
				t.Fatal(w, counts)
			}
			select {
			case loss, ok := <-h.Losses():
				if ok {
					t.Fatal("normal stop became lease loss", loss)
				}
			default:
			}
		})
	}
}

func TestWindowReleaseWaitsForInflightRenewal(t *testing.T) {
	t.Parallel()
	cfg, f, _ := leaseFixture()
	entered, canceled, allow := make(chan struct{}), make(chan struct{}), make(chan struct{})
	f.beforeRenew = func(ctx context.Context) { close(entered); <-ctx.Done(); close(canceled); <-allow }
	h := holdLease(t, cfg, leaseState(false))
	f.clock.Step(time.Minute)
	awaitLease(t, entered)
	done := make(chan error, 1)
	go func() { done <- h.Release() }()
	awaitLease(t, canceled)
	_, counts := f.snapshot()
	if counts[3] != 0 || h.Generation() != 0 {
		t.Fatal("release raced the renewer", counts)
	}
	select {
	case err := <-done:
		t.Fatal("release returned before renewal exited", err)
	default:
	}
	close(allow) // successful reply despite cancellation: release the new RV
	if err := awaitLease(t, done); err != nil {
		t.Fatal(err)
	}
	w, counts := f.snapshot()
	if w.Live(f.clock.Now()) || counts[2] != 1 || counts[3] != 1 {
		t.Fatal(w, counts)
	}
	if err := h.Release(); err != nil {
		t.Fatal(err)
	}
	_, again := f.snapshot()
	if again != counts {
		t.Fatal("second release wrote again")
	}
}

func TestWindowRestartFencesLateRenewer(t *testing.T) {
	t.Parallel()
	cfg, f, _ := leaseFixture()
	entered, allow := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	f.beforeRenew = func(context.Context) {
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-allow
		}
	}
	old := holdLease(t, cfg, leaseState(false))
	f.clock.Step(time.Minute)
	awaitLease(t, entered)
	next := holdLease(t, cfg, leaseState(true))
	if next.Generation() != 2 {
		t.Fatal("restart did not advance generation")
	}
	before, _ := f.snapshot()
	close(allow)
	awaitLease(t, old.Done())
	loss := awaitLease(t, old.Losses())
	if loss.Batch != leaseBatch || loss.Try != 1 || loss.Generation != 1 || old.Generation() != 0 {
		t.Fatal(loss)
	}
	after, counts := f.snapshot()
	if after != before || counts[3] != 0 {
		t.Fatal("old process changed new window", after, counts)
	}
	if _, err := old.Observe(context.Background()); err == nil {
		t.Fatal("old process still sends")
	}
	if _, err := next.Observe(context.Background()); err != nil {
		t.Fatal("new process positive control", err)
	}
	if err := old.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case loss, ok := <-old.Losses():
		if ok {
			t.Fatal("duplicate loss", loss)
		}
	default:
	}
}

func TestWindowTwoProcessesOnlyOneAcquire(t *testing.T) {
	t.Parallel()
	cfg, f, _ := leaseFixture()
	arrived, allow := make(chan struct{}, 2), make(chan struct{})
	f.beforeTake = func(context.Context) { arrived <- struct{}{}; <-allow }
	type result struct {
		h   *WindowHolder
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			h, err := HoldWindow(context.Background(), cfg, leaseState(false), leaseBatch, 1)
			results <- result{h, err}
		}()
	}
	awaitLease(t, arrived)
	awaitLease(t, arrived)
	close(allow)
	wins := 0
	for range 2 {
		r := awaitLease(t, results)
		if r.err == nil {
			wins++
			if err := r.h.Release(); err != nil {
				t.Fatal(err)
			}
		} else {
			var changed *delivery.WindowChanged
			if !errors.As(r.err, &changed) {
				t.Fatal(r.err)
			}
		}
	}
	if wins != 1 {
		t.Fatal("two acquired the same try", wins)
	}
}

func TestWindowLossAndReleaseLeaveForeignWindow(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"observe", "release", "renew unknown", "read unknown", "expired"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg, f, _ := leaseFixture()
			if mode == "renew unknown" {
				f.renewErr = context.DeadlineExceeded
			}
			h := holdLease(t, cfg, leaseState(false))
			f.mu.Lock()
			if mode == "observe" || mode == "release" {
				f.version++
				f.w.Who, f.w.ResourceVersion = "operator@host", fmt.Sprint(f.version)
			}
			if mode == "read unknown" {
				f.readErr = context.DeadlineExceeded
			}
			foreign := f.w
			f.mu.Unlock()
			switch mode {
			case "release":
				if err := h.Release(); err == nil {
					t.Fatal("release conflict hidden")
				}
			case "observe", "read unknown":
				if _, err := h.Observe(context.Background()); err == nil {
					t.Fatal("lost lease still accepted")
				}
			case "renew unknown":
				f.clock.Step(time.Minute)
			case "expired":
				f.clock.Step(5 * time.Minute)
			}
			awaitLease(t, h.Done())
			w, counts := f.snapshot()
			if mode != "renew unknown" && w != foreign {
				t.Fatal("changed a foreign/unknown/expired window", w)
			}
			if mode != "release" {
				loss := awaitLease(t, h.Losses())
				if loss.Generation != 1 || counts[3] != 0 {
					t.Fatal(loss, counts)
				}
			}
			if h.Generation() != 0 {
				t.Fatal("loss retained generation")
			}
		})
	}
}

func TestWindowRecoveryRequiresOwnOutstandingTry(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"running", "foreign", "missing", "terminal", "quarantined", "unsent", "closed", "backoff", "wrong who", "newer durable generation"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg, f, _ := leaseFixture()
			old := holdLease(t, cfg, leaseState(false))
			s := leaseState(true)
			b, tr := s.Batches[leaseBatch], s.Batches[leaseBatch].Tries[1]
			switch mode {
			case "foreign":
				f.mu.Lock()
				f.w.Who = "operator@host"
				f.mu.Unlock()
			case "missing":
				f.mu.Lock()
				f.w = delivery.STGWindow{}
				f.mu.Unlock()
			case "terminal":
				b.Phase, tr.RunEvidence.Seq = "TERMINAL", 6
			case "quarantined":
				b.Phase, tr.Quarantined.Seq = "QUARANTINED", 6
			case "unsent":
				b.Phase, tr.Unsent.Seq = "UNSENT", 6
			case "closed":
				b.Phase, tr.RunEvidence.Seq = "CLOSED", 6
			case "backoff":
				b.Phase = "SNAP"
				tr.Started.Seq = 0
				tr.Snapshot.Record = store.StgRecord{"not_before": f.clock.Now().Add(time.Minute).Format(time.RFC3339)}
			case "wrong who":
				object(tr.Started.Record, "lease")["who"] = "stgrel-bifrost-platform-2-1"
			case "newer durable generation":
				object(tr.Permit.Record, "lease")["gen"] = int64(9)
			}
			_, before := f.snapshot()
			next, err := HoldWindow(context.Background(), cfg, s, leaseBatch, 1)
			_, after := f.snapshot()
			if mode == "running" || mode == "newer durable generation" {
				want := int64(2)
				if mode == "newer durable generation" {
					want = 10
				}
				if err != nil || next.Generation() != want {
					t.Fatal("recovery success control", err)
				}
				if _, err := old.Observe(context.Background()); err == nil {
					t.Fatal("old owner still accepted")
				}
				if err := next.Release(); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrWindowWait) || next != nil || before[1] != after[1] {
				t.Fatal("ineligible recovery wrote", next, err, before, after)
			}
		})
	}
}

func TestWindowHumanHolderWaitsWithoutRenewing(t *testing.T) {
	t.Parallel()
	cfg, f, _ := leaseFixture()
	f.w = delivery.STGWindow{Who: "operator@host", What: "bifrost-platform", Env: "stg", ResourceVersion: "1", Expires: f.clock.Now().Add(5 * time.Minute)}
	f.version = 1
	h, err := HoldWindow(context.Background(), cfg, leaseState(false), leaseBatch, 1)
	var held *delivery.WindowHeld
	if !errors.As(err, &held) || h != nil {
		t.Fatal(h, err)
	}
	f.clock.Step(time.Minute)
	w, counts := f.snapshot()
	if w.Who != "operator@host" || counts != [4]int{1, 1, 0, 0} {
		t.Fatal("busy entry retried or changed the holder", w, counts)
	}
	f.clock.Step(5 * time.Minute)
	h = holdLease(t, cfg, leaseState(false))
	if h.Generation() != 1 {
		t.Fatal("same-entry control after human expiry")
	}
}

type leaseReadChange struct {
	Windows
	change func()
}

func (f leaseReadChange) ReadSTGWindow(ctx context.Context) (delivery.STGWindow, error) {
	w, err := f.Windows.ReadSTGWindow(ctx)
	f.change()
	return w, err
}

func TestWindowPolicyRereadAfterSlowRead(t *testing.T) {
	t.Parallel()
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprint(disable), func(t *testing.T) {
			t.Parallel()
			cfg, f, policy := leaseFixture()
			cfg.Windows = leaseReadChange{Windows: f, change: func() {
				if disable {
					p := *policy.Load()
					p.Live = false
					policy.Store(&p)
				}
			}}
			h, err := HoldWindow(context.Background(), cfg, leaseState(false), leaseBatch, 1)
			if disable {
				_, counts := f.snapshot()
				if !errors.Is(err, ErrWindowDisabled) || h != nil || counts != [4]int{1, 0, 0, 0} {
					t.Fatal("policy changed during read still acquired", err, counts)
				}
			} else {
				if err != nil {
					t.Fatal("same-entry success control", err)
				}
				if err := h.Release(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
