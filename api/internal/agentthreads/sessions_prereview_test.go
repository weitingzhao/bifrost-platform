package agentthreads

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// flushGate holds one flush at a test hook until it is opened, and reports
// each caller that starts waiting for that flush. Cleanup opens it, so a
// failing test leaves no caller blocked.
type flushGate struct {
	paused, release, waiting chan struct{}
	pauseOnce, openOnce      sync.Once
}

func holdFlush(t *testing.T, rec *Recorder, afterWrite bool) *flushGate {
	g := &flushGate{paused: make(chan struct{}), release: make(chan struct{}), waiting: make(chan struct{}, 8)}
	hook := func() { g.pauseOnce.Do(func() { close(g.paused) }); <-g.release }
	if afterWrite {
		rec.afterFlushWrite = hook
	} else {
		rec.beforeFlushWrite = hook
	}
	rec.onFlushWait = func() {
		select {
		case g.waiting <- struct{}{}:
		default:
		}
	}
	t.Cleanup(g.open)
	return g
}

func (g *flushGate) open() { g.openOnce.Do(func() { close(g.release) }) }

type snapshot struct {
	st  State
	err error
}

// snapshotDuringFlush starts State while g holds a flush and returns once
// that call waits for the flush. A snapshot built while the flush is held
// fails the test with what it read.
func snapshotDuringFlush(t *testing.T, rec *Recorder, g *flushGate, describe func(State) string) <-chan snapshot {
	t.Helper()
	out := make(chan snapshot, 1)
	go func() {
		st, err := rec.State()
		out <- snapshot{st, err}
	}()
	select {
	case <-g.waiting:
	case s := <-out:
		t.Fatalf("a snapshot was built during the flush (err=%v): %s", s.err, describe(s.st))
	case <-time.After(5 * time.Second):
		t.Fatal("the snapshot neither waited for the flush nor returned")
	}
	return out
}

// P-c2 pre-review M1: a flush writes its batch with the lock released. A
// synchronous write (here a nonce replay) arriving meanwhile waits for it,
// and so does a snapshot: the buffered precompact and refusal it carries are
// never lost to a newer sequence written first, nor missing from a list.
func TestSyncWriteWaitsForFlushInFlight(t *testing.T) {
	r := newRig2(t, nil)
	nonce := strings.Repeat("b", 64)
	first := r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1, "register_nonce": nonce})
	r.clk.at = t0.Add(time.Second)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 2, "event": "precompact", "reason": "auto",
		"client_bind": map[string]any{"state": "refused", "reason": "no_context"}, "thread_key": first["thread_key"]})
	g := holdFlush(t, r.rec, false)
	flushed := make(chan error, 1)
	go func() { flushed <- r.rec.Flush() }()
	<-g.paused
	describe := func(st State) string {
		th := st.Threads[Key("claude", "s")]
		return fmt.Sprintf("compactions=%d client_bind=%v", th.Compactions, th.ClientBind)
	}
	snap := snapshotDuringFlush(t, r.rec, g, describe)
	r.clk.at = t0.Add(2 * time.Second)
	replayed := make(chan int, 1)
	go func() {
		w := r.beat(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 3, "event": "after_tool", "tool": "Read", "register_nonce": nonce})
		replayed <- w.Code
	}()
	select {
	case <-g.waiting:
	case code := <-replayed:
		t.Fatalf("the nonce replay was written during the flush (%d)", code)
	case <-time.After(5 * time.Second):
		t.Fatal("the nonce replay neither waited for the flush nor returned")
	}
	g.open()
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	s := <-snap
	if th := s.st.Threads[Key("claude", "s")]; s.err != nil || th.Compactions != 1 || th.ClientBind == nil || th.ClientBind.State != BindRefused {
		t.Fatalf("the snapshot taken across the flush (err=%v): %s", s.err, describe(s.st))
	}
	if code := <-replayed; code != http.StatusAccepted {
		t.Fatalf("the nonce replay: %d", code)
	}
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
	th := recState(t, r.rec).Threads[Key("claude", "s")]
	if th.Seq != 3 || th.Compactions != 1 || th.ClientBind == nil || th.ClientBind.State != BindRefused {
		t.Fatalf("seq=%d compactions=%d client_bind=%v; want 3, 1 and the refusal", th.Seq, th.Compactions, th.ClientBind)
	}
}

// P-c2 pre-review M2: at capacity a new row opened by a side beat is not its
// own eviction victim. Its registration is refused and rolls back, so a
// retry of the same request is refused again and nothing is counted twice.
func TestSideRegistrationAtCapacityIsRefusedOnce(t *testing.T) {
	r := newRig2(t, nil)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1})
	for i := 1; i < maxThreads; i++ {
		r.ok(reporterP, map[string]any{"thread": "busy-" + strconv.Itoa(i), "seq": 1})
	}
	nonce := strings.Repeat("c", 64)
	for try := 0; try < 2; try++ {
		w := r.beat(reporterP, map[string]any{"thread": "s:1", "session": "s", "epoch": 1, "seq": 1, "event": "precompact", "reason": "auto", "register_nonce": nonce})
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("try %d: %d %s", try, w.Code, w.Body)
		}
	}
	st := recState(t, r.rec)
	if _, ok := st.Threads[Key("claude", "s:1")]; ok {
		t.Fatal("the refused row was kept")
	}
	if got := st.Threads[Key("claude", "s")].Compactions; got != 0 {
		t.Fatalf("s counted %d compactions from refused registrations", got)
	}
}

// P-c2 pre-review M3: a side beat that moves an existing row to another host
// takes and shares that host's key state, not the old one's.
func TestSideBeatHostChangeKeepsKeysApart(t *testing.T) {
	st := State{}
	st.Apply(Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: TurnStart, Seq: 1, TurnID: "t"}, t0)
	st.Apply(Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: PreCompact, Reason: "auto", Seq: 2, TurnID: "t"}, t0.Add(time.Second))
	st.Apply(Beat{Thread: "s:1", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: TurnStart, Seq: 1, TurnID: "u"}, t0.Add(2*time.Second))
	st.Apply(Beat{Thread: "h2-s", Vendor: "claude", Host: "h2", Session: "s", Epoch: 1, Event: TurnStart, Seq: 1, TurnID: "v",
		ClientBind: &ClientBind{State: BindRefused, Reason: "no_context"}}, t0.Add(3*time.Second))
	st.Apply(Beat{Thread: "s", Vendor: "claude", Host: "h2", Session: "s", Epoch: 1, Event: PreCompact, Reason: "auto", Seq: 3, TurnID: "t"}, t0.Add(4*time.Second))
	s := st.Threads[Key("claude", "s")]
	if s.Host != "h2" {
		t.Fatalf("s stayed on %s", s.Host)
	}
	h1 := st.Threads[Key("claude", "s:1")]
	if h1.Compactions != 1 || h1.ClientBind != nil || !h1.FirstSeenAt.Equal(t0) {
		t.Fatalf("h1's key took h2's state: %+v", h1)
	}
	for _, row := range []string{"s", "h2-s"} {
		got := st.Threads[Key("claude", row)]
		if got.Compactions != 1 || got.ClientBind == nil || got.ClientBind.State != BindRefused || !got.FirstSeenAt.Equal(t0.Add(3*time.Second)) {
			t.Fatalf("%s does not carry h2's key state: compactions=%d client_bind=%v first_seen=%v", row, got.Compactions, got.ClientBind, got.FirstSeenAt)
		}
	}
}

// P-c2 pre-review M4: retention reads when a row was last heard from, a side
// beat included, not the turn's clock it leaves alone.
func TestFreshSideBeatIsNotPruned(t *testing.T) {
	cfg := DefaultConfig()
	st := State{}
	st.Apply(Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: WaitingOwner, Reason: "permission_prompt", Seq: 1, TurnID: "t"}, t0)
	st.Apply(Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: PreCompact, Reason: "auto", Seq: 2, TurnID: "t"}, t0.Add(cfg.Retain-time.Hour))
	if got := st.Threads[Key("claude", "s")]; !got.At.Equal(t0) {
		t.Fatalf("the side beat moved the wait's clock: %v", got.At)
	}
	now := t0.Add(cfg.Retain + time.Second)
	st.Prune(now.Add(-cfg.Retain), cfg, now)
	if _, ok := st.Threads[Key("claude", "s")]; !ok {
		t.Fatal("a row heard from an hour ago was pruned as older than the retention")
	}
	later := t0.Add(2*cfg.Retain + time.Second)
	st.Prune(later.Add(-cfg.Retain), cfg, later)
	if _, ok := st.Threads[Key("claude", "s")]; ok {
		t.Fatal("a row silent for the whole retention was kept")
	}
}

// P-c2 pre-review, second round: a flush that has written its batch, and in
// the same update pruned a row of it heard last before the retention cutoff,
// is followed by no snapshot that replays the batch. That replay revived the
// pruned row, counted its precompact a second time on the key's other row,
// and put back the older diagnosis over the newer one stored.
func TestSnapshotAfterFlushKeepsPrunedRowGone(t *testing.T) {
	r := newRig2(t, nil)
	s := r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1})
	s1 := r.ok(reporterP, map[string]any{"thread": "s:1", "session": "s", "epoch": 1, "seq": 1})
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 2, "event": "precompact", "reason": "auto",
		"client_bind": map[string]any{"state": "refused", "reason": "no_context"}, "thread_key": s["thread_key"]})
	r.clk.at = t0.Add(r.rec.cfg.Retain + time.Second)
	r.ok(reporterP, map[string]any{"thread": "s:1", "session": "s", "epoch": 1, "seq": 2, "event": "after_tool", "tool": "Read",
		"client_bind": map[string]any{"state": "unlinked", "reason": "no_description"}, "thread_key": s1["thread_key"]})
	g := holdFlush(t, r.rec, true)
	flushed := make(chan error, 1)
	go func() { flushed <- r.rec.Flush() }()
	<-g.paused
	describe := func(st State) string {
		_, revived := st.Threads[Key("claude", "s")]
		th := st.Threads[Key("claude", "s:1")]
		bind := "nil"
		if th.ClientBind != nil {
			bind = th.ClientBind.State + "/" + th.ClientBind.Reason
		}
		return fmt.Sprintf("s present=%v; s:1 compactions=%d client_bind=%s", revived, th.Compactions, bind)
	}
	want := "s present=false; s:1 compactions=1 client_bind=unlinked/no_description"
	stored, err := r.rec.store.Load()
	if err != nil || describe(stored) != want {
		t.Fatalf("the store after the write (err=%v): %s; want %s", err, describe(stored), want)
	}
	snap := snapshotDuringFlush(t, r.rec, g, describe)
	g.open()
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if got := <-snap; got.err != nil || describe(got.st) != want {
		t.Fatalf("the snapshot taken across the flush (err=%v): %s; want %s", got.err, describe(got.st), want)
	}
}

// A host heartbeat arriving during a flush waits for it too: the hosts the
// flush is writing count toward the cap, so the host over it is refused at
// once rather than buffered and dropped later.
func TestHostBeatWaitsForFlushInFlight(t *testing.T) {
	r := newRig2(t, nil)
	for i := 0; i < maxHosts; i++ {
		b := HostBeat{Host: "host-" + strconv.Itoa(i)}
		if err := b.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := r.rec.RecordHost(b); err != nil {
			t.Fatal(err)
		}
		if i == maxHosts-2 {
			if err := r.rec.Flush(); err != nil {
				t.Fatal(err)
			}
		}
	}
	g := holdFlush(t, r.rec, false)
	flushed := make(chan error, 1)
	go func() { flushed <- r.rec.Flush() }()
	<-g.paused
	recorded := make(chan error, 1)
	go func() { recorded <- r.rec.RecordHost(HostBeat{Host: "one-too-many"}) }()
	select {
	case <-g.waiting:
	case err := <-recorded:
		t.Fatalf("a host beat was judged during the flush: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the host beat neither waited for the flush nor returned")
	}
	g.open()
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if err := <-recorded; err != ErrHostFull {
		t.Fatalf("the host over the cap: %v, want ErrHostFull", err)
	}
}
