package agentthreads

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 10, 4, 3, 0, 0, time.UTC)

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

type pager struct {
	mu   sync.Mutex
	sent []string
	fail bool
}

func (p *pager) notify(_ context.Context, title, body string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return errors.New("relay down")
	}
	p.sent = append(p.sent, title+" | "+body)
	return nil
}

func (p *pager) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

// rig is one store with an API-side recorder and a workers-side watcher sharing a clock.
type rig struct {
	clk   *clock
	store *Store
	rec   *Recorder
	w     *Watcher
	page  *pager
}

func newRig(t *testing.T, path string) *rig {
	t.Helper()
	clk := &clock{at: t0}
	store := NewStore(path)
	cfg := DefaultConfig()
	rec := NewRecorder(store, cfg)
	rec.now = clk.now
	page := &pager{}
	w := NewWatcher(store, cfg, page.notify, nil)
	w.now = clk.now
	return &rig{clk: clk, store: store, rec: rec, w: w, page: page}
}

func (r *rig) beat(t *testing.T, ev Event, tool string, timeout int) {
	t.Helper()
	b := Beat{Thread: "7e939cd5-b3b1-4705-96e4-0a3b9e61648d", Vendor: "claude", Host: "mbp", Work: "W-54", Event: ev, Tool: tool, ToolTimeoutS: timeout}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	r.rec.Record(b)
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) at(d time.Duration) { r.clk.at = t0.Add(d) }

func (r *rig) tick(t *testing.T) {
	t.Helper()
	if !r.w.tick(context.Background()) {
		t.Fatal("watch pass failed")
	}
}

func TestThresholdIsTenMinutesOrDeclaredTimeoutPlusTwo(t *testing.T) {
	c := DefaultConfig()
	cases := []struct {
		ev      Event
		timeout int
		want    time.Duration
	}{
		{TurnStart, 0, 10 * time.Minute},
		{BeforeTool, 0, 10 * time.Minute},
		{BeforeTool, 60, 10 * time.Minute},
		{BeforeTool, 600, 12 * time.Minute},
		{BeforeTool, 1800, 32 * time.Minute},
		{AfterTool, 600, 10 * time.Minute},
	}
	for _, tc := range cases {
		if got := c.Threshold(Thread{Event: tc.ev, ToolTimeoutS: tc.timeout}); got != tc.want {
			t.Errorf("%s timeout %d: threshold %s, want %s", tc.ev, tc.timeout, got, tc.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	c := DefaultConfig()
	mid := Thread{Event: AfterTool, At: t0}
	if s := c.StatusOf(mid, t0.Add(10*time.Minute)); s != InTurn {
		t.Fatalf("at exactly 10m: %s", s)
	}
	if s := c.StatusOf(mid, t0.Add(10*time.Minute+time.Second)); s != Silent {
		t.Fatalf("past 10m: %s", s)
	}
	if s := c.StatusOf(Thread{Event: TurnEnd, At: t0}, t0.Add(48*time.Hour)); s != Idle {
		t.Fatalf("turn ended: %s", s)
	}
}

// Acceptance 1: a session stops mid-turn; one push within 12 minutes, never a second.
func TestSilentMidTurnPushesOnceWithinTwelveMinutes(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, TurnStart, "", 0)
	r.at(time.Minute)
	r.beat(t, BeforeTool, "Bash", 120)
	for d := time.Minute; d <= 11*time.Minute; d += WatchInterval {
		r.at(d)
		r.tick(t)
	}
	if r.page.count() != 0 {
		t.Fatalf("pushed before the threshold: %v", r.page.sent)
	}
	r.at(11*time.Minute + WatchInterval)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("want one push by 11m30s, got %d", r.page.count())
	}
	for _, d := range []time.Duration{12 * time.Minute, time.Hour, 5 * time.Hour} {
		r.at(d)
		r.tick(t)
	}
	if r.page.count() != 1 {
		t.Fatalf("pushed again for the same silence: %v", r.page.sent)
	}
	msg := r.page.sent[0]
	for _, want := range []string{"Agent thread silent 10m", "claude on mbp", "W-54", "last event before_tool Bash (timeout 120s) at 04:04 UTC", "in turn 11m"} {
		if !strings.Contains(msg, want) {
			t.Errorf("push %q lacks %q", msg, want)
		}
	}
}

// Acceptance 2: after the silence, a new event puts the thread back in turn
// with no push; a later silence is a new one.
func TestRecoveryIsQuietAndANewSilencePushesAgain(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, AfterTool, "Read", 0)
	r.at(11 * time.Minute)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("first silence: %d pushes", r.page.count())
	}
	r.at(5 * time.Hour)
	r.beat(t, BeforeTool, "Bash", 0)
	r.tick(t)
	st, _ := r.store.Load()
	views := DefaultConfig().Views(st, r.clk.at)
	if len(views) != 1 || views[0].Status != InTurn {
		t.Fatalf("after waking: %+v", views)
	}
	if r.page.count() != 1 {
		t.Fatalf("recovery pushed: %v", r.page.sent)
	}
	r.at(5*time.Hour + 11*time.Minute)
	r.tick(t)
	if r.page.count() != 2 {
		t.Fatalf("second silence: %d pushes", r.page.count())
	}
}

// Acceptance 3: a turn that ends normally never pushes.
func TestTurnEndNeverPushes(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, TurnStart, "", 0)
	r.at(time.Minute)
	r.beat(t, BeforeTool, "Shell", 30)
	r.at(2 * time.Minute)
	r.beat(t, AfterTool, "Shell", 0)
	r.at(3 * time.Minute)
	r.beat(t, TurnEnd, "", 0)
	for _, d := range []time.Duration{15 * time.Minute, time.Hour, 24 * time.Hour} {
		r.at(d)
		r.tick(t)
	}
	if r.page.count() != 0 {
		t.Fatalf("pushed after a normal turn end: %v", r.page.sent)
	}
}

// Acceptance 4: a command that declared 600s and runs all of it does not push.
func TestDeclaredSixHundredSecondsRunToTheEndDoesNotPush(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, TurnStart, "", 0)
	r.beat(t, BeforeTool, "Bash", 600)
	for d := WatchInterval; d <= 10*time.Minute; d += WatchInterval {
		r.at(d)
		r.tick(t)
	}
	r.beat(t, AfterTool, "Bash", 0)
	for d := 10 * time.Minute; d <= 19*time.Minute; d += WatchInterval {
		r.at(d)
		r.tick(t)
	}
	r.beat(t, TurnEnd, "", 0)
	r.at(2 * time.Hour)
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatalf("pushed during a declared 600s command: %v", r.page.sent)
	}
}

// The declared timeout only stretches the threshold to timeout + 2m.
func TestDeclaredTimeoutOverrunPushes(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, BeforeTool, "Bash", 600)
	r.at(12 * time.Minute)
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatal("pushed at exactly timeout + grace")
	}
	r.at(12*time.Minute + WatchInterval)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("want one push past timeout + grace, got %d", r.page.count())
	}
}

func TestFailedPushIsRetriedNextPass(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, AfterTool, "Edit", 0)
	r.page.fail = true
	r.at(11 * time.Minute)
	if r.w.tick(context.Background()) {
		t.Fatal("a failed push counted as a good pass")
	}
	r.page.fail = false
	r.at(11*time.Minute + WatchInterval)
	r.tick(t)
	r.at(12 * time.Minute)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("want exactly one push after the retry, got %d", r.page.count())
	}
}

// A thread left mid-turn long ago (laptop retired, deploy after a week) is
// marked, not paged.
func TestOldSilenceIsMarkedNotPushed(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, AfterTool, "Read", 0)
	r.at(7 * time.Hour)
	r.tick(t)
	r.at(8 * time.Hour)
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatalf("paged for a silence older than PushWithin: %v", r.page.sent)
	}
	st, _ := r.store.Load()
	for _, th := range st.Threads {
		if !th.Pushed() {
			t.Fatal("old silence not marked")
		}
	}
}

// Two workers pods during a rollout share the state; only one pushes.
func TestTwoWatchersOnOneStatePushOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-threads")
	r := newRig(t, path)
	r.beat(t, AfterTool, "Grep", 0)
	other := NewWatcher(NewStore(path), DefaultConfig(), r.page.notify, nil)
	other.now = r.clk.now
	r.at(11 * time.Minute)
	var wg sync.WaitGroup
	for _, w := range []*Watcher{r.w, other, r.w, other} {
		wg.Add(1)
		go func(w *Watcher) {
			defer wg.Done()
			w.tick(context.Background())
		}(w)
	}
	wg.Wait()
	if r.page.count() != 1 {
		t.Fatalf("want one push from two watchers, got %d", r.page.count())
	}
}

func TestLineageTitleNamesThePush(t *testing.T) {
	r := newRig(t, "")
	r.w.titles = func(context.Context) (map[string]string, error) {
		return map[string]string{"7e939cd5-b3b1-4705-96e4-0a3b9e61648d": "W-31 decisions"}, nil
	}
	r.beat(t, AfterTool, "Read", 0)
	r.at(11 * time.Minute)
	r.tick(t)
	if r.page.count() != 1 || !strings.Contains(r.page.sent[0], ": W-31 decisions |") {
		t.Fatalf("push %v does not carry the lineage title", r.page.sent)
	}
}

func TestRecorderShowsBufferedEventsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-threads")
	clk := &clock{at: t0}
	rec := NewRecorder(NewStore(path), DefaultConfig())
	rec.now = clk.now
	rec.Record(Beat{Thread: "a1", Vendor: "cursor", Host: "mbp", Event: TurnStart})
	st, err := rec.State()
	if err != nil || len(st.Threads) != 1 {
		t.Fatalf("buffered event not visible: %v %+v", err, st)
	}
	if raw, _ := NewStore(path).Load(); len(raw.Threads) != 0 {
		t.Fatal("written before a flush")
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, _ := NewStore(path).Load(); got.Threads[Key("cursor", "a1")].Event != TurnStart {
		t.Fatalf("not persisted: %+v", got)
	}
	clk.at = t0.Add(73 * time.Hour)
	rec.Record(Beat{Thread: "b2", Vendor: "codex", Host: "mbp", Event: TurnStart})
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	got, _ := NewStore(path).Load()
	if _, kept := got.Threads[Key("cursor", "a1")]; kept || len(got.Threads) != 1 {
		t.Fatalf("thread older than Retain kept: %+v", got.Threads)
	}
}

func TestApplyTracksTurnStart(t *testing.T) {
	var st State
	st.Apply(Beat{Thread: "x", Vendor: "codex", Host: "h", Event: AfterTool}, t0)
	st.Apply(Beat{Thread: "x", Vendor: "codex", Host: "h", Event: BeforeTool}, t0.Add(time.Minute))
	if got := st.Threads[Key("codex", "x")].TurnStartedAt; !got.Equal(t0) {
		t.Fatalf("first event starts the turn: %s", got)
	}
	st.Apply(Beat{Thread: "x", Vendor: "codex", Host: "h", Event: TurnEnd}, t0.Add(2*time.Minute))
	st.Apply(Beat{Thread: "x", Vendor: "codex", Host: "h", Event: BeforeTool}, t0.Add(3*time.Minute))
	if got := st.Threads[Key("codex", "x")].TurnStartedAt; !got.Equal(t0.Add(3 * time.Minute)) {
		t.Fatalf("event after turn end starts a new turn: %s", got)
	}
	st.Apply(Beat{Thread: "x", Vendor: "codex", Host: "h", Event: TurnEnd}, t0.Add(time.Minute))
	if st.Threads[Key("codex", "x")].Event != BeforeTool {
		t.Fatal("an older event overwrote a newer one")
	}
}

func TestValidate(t *testing.T) {
	ok := Beat{Thread: "0f549e24-a80b-48a6-9799-f23e4c2926e8", Vendor: "cursor", Host: "Vision-MacBook-Pro.local", Event: AfterTool, ToolTimeoutS: 30}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.ToolTimeoutS != 0 {
		t.Fatal("a timeout on a non-before event was kept")
	}
	bad := []Beat{
		{Thread: "", Vendor: "cursor", Host: "h", Event: TurnStart},
		{Thread: "a b", Vendor: "cursor", Host: "h", Event: TurnStart},
		{Thread: "a", Vendor: "Cursor!", Host: "h", Event: TurnStart},
		{Thread: "a", Vendor: "cursor", Host: "", Event: TurnStart},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: "stop"},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: BeforeTool, ToolTimeoutS: -1},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: TurnStart, Work: "fix things"},
	}
	for _, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("accepted %+v", b)
		}
	}
}

func TestHandlers(t *testing.T) {
	clk := &clock{at: t0}
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = clk.now
	h := NewHandler(rec, DefaultConfig(), nil)
	h.now = clk.now

	post := func(body string) int {
		w := httptest.NewRecorder()
		h.HandleBeat(w, httptest.NewRequest(http.MethodPost, "/api/v1/agent/threads/heartbeat", strings.NewReader(body)))
		return w.Code
	}
	if code := post(`{"thread":"t1","vendor":"codex","host":"mbp","event":"before_tool","tool":"Bash","tool_timeout_s":600}`); code != http.StatusAccepted {
		t.Fatalf("POST valid: %d", code)
	}
	if code := post(`{"thread":"t1","vendor":"codex","host":"mbp","event":"nap"}`); code != http.StatusBadRequest {
		t.Fatalf("POST bad event: %d", code)
	}
	if code := post(`not json`); code != http.StatusBadRequest {
		t.Fatalf("POST bad body: %d", code)
	}
	clk.at = t0.Add(13 * time.Minute)
	w := httptest.NewRecorder()
	h.HandleList(w, httptest.NewRequest(http.MethodGet, "/api/v1/agent/threads", nil))
	body := w.Body.String()
	for _, want := range []string{`"status":"silent"`, `"quiet_seconds":780`, `"threshold_seconds":720`, `"silent_after_seconds":600`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET body lacks %s: %s", want, body)
		}
	}
}
