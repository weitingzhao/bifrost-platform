package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// fakeLedger holds rows in cursor order and a horizon, as EventsAfter reads.
type fakeLedger struct {
	mu      sync.Mutex
	rows    []relay.Row
	horizon int64
	reads   int
}

func (l *fakeLedger) add(r relay.Row) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows = append(l.rows, r)
	if r.TxID >= l.horizon {
		l.horizon = r.TxID + 1
	}
}

func (l *fakeLedger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reads
}

func (l *fakeLedger) After(_ context.Context, after relay.Cursor, limit int, types []string) ([]relay.Row, relay.Cursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reads++
	var out []relay.Row
	for _, r := range l.rows {
		if after.Less(r.Cursor()) && r.TxID < l.horizon && slices.Contains(types, r.Type) && len(out) < limit {
			out = append(out, r)
		}
	}
	switch {
	case len(out) == limit:
		return out, out[len(out)-1].Cursor(), nil
	case l.horizon > after.TxID:
		return out, relay.Cursor{TxID: l.horizon}, nil
	}
	return out, after, nil
}

func (l *fakeLedger) Horizon(context.Context) (relay.Cursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return relay.Cursor{TxID: l.horizon}, nil
}

func (l *fakeLedger) Event(_ context.Context, id int64) (relay.Row, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.rows {
		if r.EventID == id {
			return r, true, nil
		}
	}
	return relay.Row{}, false, nil
}

func gitRow(id, txid int64, repo string) relay.Row {
	commit := strings.Repeat("a", 40)
	return relay.Row{EventID: id, TxID: txid, Stream: "git:" + repo, Type: "git.main.advanced", Actor: "service:gate-git",
		At: time.Now().UTC(),
		Body: map[string]any{"repo": repo, "commit": commit, "before": "", "subject": repo + "@" + commit,
			"ref": "/api/v1/delivery/context?repo=" + repo, "key": repo, "change": "pushed"}}
}

func newGate(t *testing.T, l *fakeLedger) *Gate {
	t.Helper()
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	return &Gate{Ledger: l, Tenant: all[relay.Tenant], Env: "stg", Wake: &Waker{}}
}

type answer struct {
	Events []relay.Envelope `json:"events"`
	Next   string           `json:"next"`
}

func get(t *testing.T, g *Gate, query string) (answer, time.Duration) {
	t.Helper()
	start := time.Now()
	rec := httptest.NewRecorder()
	g.HandleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/events?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d %s", query, rec.Code, rec.Body)
	}
	var a answer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	return a, time.Since(start)
}

// 2026-11-16: bus step 1 (RULES.md §8 items 3-4): a long poll answers as soon
// as a wake comes and the ledger has the event, well before its wait.
func TestLongPollReturnsOnWake(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	g := newGate(t, l)
	g.NATSUp = func() bool { return true } // the ledger is re-read only every 16 s
	go func() {
		time.Sleep(200 * time.Millisecond)
		l.add(gitRow(7, 60, "bifrost-platform"))
		g.Wake.Notify()
	}()
	a, took := get(t, g, "types=git.main.advanced&after=50.0&wait=20&client=test")
	if len(a.Events) != 1 || a.Events[0].ID != "ev-7" || took > 5*time.Second {
		t.Fatalf("after %s: %+v", took, a)
	}
	if a.Next != "61.0" {
		t.Errorf("next %q", a.Next)
	}
}

// 2026-11-16: bus step 1 (RULES.md §8): with nothing to give, the poll ends
// at its wait with no events and a cursor to read on from.
func TestLongPollTimesOut(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	g := newGate(t, l)
	g.Poll = 100 * time.Millisecond
	a, took := get(t, g, "types=git.main.advanced&wait=1")
	if len(a.Events) != 0 || took < time.Second || took > 4*time.Second || a.Next != "50.0" {
		t.Fatalf("after %s: %+v", took, a)
	}
	// match filters by subject: an event of another repository is not given.
	l.add(gitRow(8, 70, "other-repo"))
	a, _ = get(t, g, "types=git.main.advanced&after=50.0&match=bifrost-platform@&wait=1")
	if len(a.Events) != 0 || a.Next != "71.0" {
		t.Fatalf("an unmatched event was given: %+v", a)
	}
}

// 2026-11-16: bus step 1 (RULES.md §8: entry points work without the bus):
// with no NATS at all the waiter finds the event by re-reading the ledger.
func TestLongPollWorksWithoutNATS(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	g := newGate(t, l)
	g.Wake, g.NATSUp, g.Poll = nil, nil, 100*time.Millisecond
	go func() {
		time.Sleep(300 * time.Millisecond)
		l.add(gitRow(9, 60, "bifrost-platform"))
	}()
	a, took := get(t, g, "types=git.main.advanced&after=50.0&wait=10")
	if len(a.Events) != 1 || took > 3*time.Second || l.count() < 2 {
		t.Fatalf("after %s, %d reads: %+v", took, l.count(), a)
	}
}

// 2026-11-16: bus step 1 (RULES.md §8 item 2): the SSE form gives each
// event its cursor as id, and a reconnect with Last-Event-ID resumes after it.
func TestStreamResumesFromLastEventID(t *testing.T) {
	l := &fakeLedger{}
	l.add(gitRow(1, 10, "bifrost-platform"))
	l.add(gitRow(2, 11, "bifrost-platform"))
	l.add(gitRow(3, 12, "bifrost-platform"))
	g := newGate(t, l)
	g.Heartbeat, g.Poll = 200*time.Millisecond, 50*time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(g.HandleStream))
	defer srv.Close()

	ids := func(lastEventID string, n int) []string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?types=git.main.advanced&after=0.0", nil)
		if lastEventID != "" {
			req.Header.Set("Last-Event-ID", lastEventID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("content type %q", ct)
		}
		var got []string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() && len(got) < n {
			if id, ok := strings.CutPrefix(sc.Text(), "id: "); ok {
				got = append(got, id)
			}
		}
		return got
	}
	if got := ids("", 3); !slices.Equal(got, []string{"10.1", "11.2", "12.3"}) {
		t.Fatalf("first connection: %v", got)
	}
	if got := ids("11.2", 1); !slices.Equal(got, []string{"12.3"}) {
		t.Fatalf("resumed from 11.2: %v", got)
	}
}

// 2026-11-16: bus step 1: what a reader may ask is the register's types.
func TestUnregisteredTypeIsRefused(t *testing.T) {
	g := newGate(t, &fakeLedger{})
	rec := httptest.NewRecorder()
	g.HandleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/events?types=not.a.type", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HTTP %d", rec.Code)
	}
}

// 2026-11-19, bus step 4 (the Owner: "every page polling will poll you to
// death"): before, each of N open streams read the ledger itself when an
// event woke them, so reads grew with open Consoles. Twenty waiters woken by
// one event now cost at most a few ledger reads, not twenty.
func TestWaitersWokenTogetherShareOneRead(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	g := newGate(t, l)
	g.NATSUp = func() bool { return true }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Follow(ctx)
	for !g.shared.following.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	const n = 20
	var wg sync.WaitGroup
	got := make(chan int, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, _ := get(t, g, "types=git.main.advanced&after=50.0&wait=10")
			got <- len(a.Events)
		}()
	}
	time.Sleep(300 * time.Millisecond) // all waiting, each after its first read
	before := l.count()
	l.add(gitRow(7, 60, "bifrost-platform"))
	g.Wake.Notify()
	wg.Wait()
	close(got)
	for k := range got {
		if k != 1 {
			t.Fatalf("a waiter got %d events, want 1", k)
		}
	}
	if extra := l.count() - before; extra > 3 {
		t.Errorf("one event cost %d ledger reads for %d waiters; want them shared", extra, n)
	}
}

// 2026-11-19, bus step 4: an idle waiter re-read the ledger on its own timer
// (every 2 s without NATS), so ten idle Consoles were ten readers. With the
// follower each process reads once per poll, and an event is still found
// without NATS.
func TestIdleWaitersCostNoReadsAndTheFollowerWakesThem(t *testing.T) {
	l := &fakeLedger{horizon: 50}
	g := newGate(t, l)
	g.NATSUp, g.Poll = nil, 100*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Follow(ctx)
	for !g.shared.following.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	const n = 10
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			get(t, g, "types=git.main.advanced&after=50.0&wait=1")
		}()
	}
	wg.Wait()
	// One second of 10 idle waiters: their first reads (shared or not) and
	// the follower's ~10, not 10 waiters x 10 polls.
	if c := l.count(); c > n+15 {
		t.Errorf("%d ledger reads in a second of %d idle waiters", c, n)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		l.add(gitRow(9, 60, "bifrost-platform")) // no NATS wake
	}()
	a, took := get(t, g, "types=git.main.advanced&after=50.0&wait=10")
	if len(a.Events) != 1 || took > 3*time.Second {
		t.Fatalf("without NATS the follower did not wake the waiter: after %s, %+v", took, a)
	}
}
