package agentthreads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	seq   uint64
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
	r.beatOn(t, "7e939cd5-b3b1-4705-96e4-0a3b9e61648d", "mbp", ev, tool, timeout)
}

func (r *rig) beatOn(t *testing.T, thread, host string, ev Event, tool string, timeout int) {
	t.Helper()
	r.seq++
	b := Beat{
		Thread: thread, Vendor: "claude", Host: host, Work: "W-54",
		Event: ev, Tool: tool, ToolTimeoutS: timeout, TurnID: "turn-1", Seq: r.seq,
	}
	if ev == WaitingOwner {
		b.Reason = "permission_prompt"
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	r.rec.Record(b)
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) host(t *testing.T, name string, vendors map[string]VendorReport) {
	t.Helper()
	b := HostBeat{Host: name, Vendors: vendors}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := r.rec.RecordHost(b); err != nil {
		t.Fatal(err)
	}
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
	if s := c.StatusOf(mid, false, t0.Add(10*time.Minute)); s != InTurn {
		t.Fatalf("at exactly 10m: %s", s)
	}
	if s := c.StatusOf(mid, false, t0.Add(10*time.Minute+time.Second)); s != Silent {
		t.Fatalf("past 10m: %s", s)
	}
	if s := c.StatusOf(Thread{Event: TurnEnd, At: t0}, true, t0.Add(48*time.Hour)); s != Idle {
		t.Fatalf("turn ended: %s", s)
	}
	if s := c.StatusOf(mid, true, t0); s != HostLost {
		t.Fatalf("host lost: %s", s)
	}
	if s := c.StatusOf(Thread{Event: WaitingOwner, At: t0}, false, t0.Add(2*time.Hour)); s != StatusWaiting {
		t.Fatalf("waiting: %s", s)
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
	rec.Record(Beat{Thread: "a1", Vendor: "cursor", Host: "mbp", Event: TurnStart, TurnID: "t", Seq: 1})
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
	rec.Record(Beat{Thread: "b2", Vendor: "codex", Host: "mbp", Event: TurnStart, TurnID: "t", Seq: 1})
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
	b := func(ev Event, seq uint64) Beat {
		return Beat{Thread: "x", Vendor: "codex", Host: "h", Event: ev, TurnID: "t", Seq: seq}
	}
	st.Apply(b(AfterTool, 1), t0)
	st.Apply(b(BeforeTool, 2), t0.Add(time.Minute))
	if got := st.Threads[Key("codex", "x")].TurnStartedAt; !got.Equal(t0) {
		t.Fatalf("first event starts the turn: %s", got)
	}
	st.Apply(b(TurnEnd, 3), t0.Add(2*time.Minute))
	st.Apply(b(BeforeTool, 4), t0.Add(3*time.Minute))
	if got := st.Threads[Key("codex", "x")].TurnStartedAt; !got.Equal(t0.Add(3 * time.Minute)) {
		t.Fatalf("event after turn end starts a new turn: %s", got)
	}
	// A delayed older sequence arrives after the newer one. Receive time is later; it must not win.
	if st.Apply(b(TurnEnd, 2), t0.Add(4*time.Minute)) {
		t.Fatal("an older sequence was stored")
	}
	if st.Threads[Key("codex", "x")].Event != BeforeTool {
		t.Fatal("an older event overwrote a newer one")
	}
}

func TestValidate(t *testing.T) {
	ok := Beat{Thread: "0f549e24-a80b-48a6-9799-f23e4c2926e8", Vendor: "cursor", Host: "Vision-MacBook-Pro.local", Event: AfterTool, ToolTimeoutS: 30, TurnID: "t1", Seq: 1}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.ToolTimeoutS != 0 {
		t.Fatal("a timeout on a non-before event was kept")
	}
	bad := []Beat{
		{Thread: "", Vendor: "cursor", Host: "h", Event: TurnStart, TurnID: "t1", Seq: 1},
		{Thread: "a b", Vendor: "cursor", Host: "h", Event: TurnStart, TurnID: "t1", Seq: 1},
		{Thread: "a", Vendor: "Cursor!", Host: "h", Event: TurnStart, TurnID: "t1", Seq: 1},
		{Thread: "a", Vendor: "cursor", Host: "", Event: TurnStart, TurnID: "t1", Seq: 1},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: "stop", TurnID: "t1", Seq: 1},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: BeforeTool, ToolTimeoutS: -1, TurnID: "t1", Seq: 1},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: TurnStart, Work: "fix things", TurnID: "t1", Seq: 1},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: TurnStart, TurnID: "t1"},
		{Thread: "a", Vendor: "cursor", Host: "h", Event: WaitingOwner, Reason: "auth_success", TurnID: "t1", Seq: 1},
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
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	h.now = clk.now

	post := func(body string) int {
		w := httptest.NewRecorder()
		h.HandleBeat(w, httptest.NewRequest(http.MethodPost, "/api/v1/agent/threads/heartbeat", strings.NewReader(body)))
		return w.Code
	}
	if code := post(`{"thread":"t1","vendor":"codex","host":"mbp","event":"before_tool","tool":"Bash","tool_timeout_s":600,"turn_id":"t1","seq":1}`); code != http.StatusAccepted {
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
	for _, want := range []string{`"status":"silent"`, `"quiet_seconds":780`, `"threshold_seconds":720`, `"silent_after_seconds":600`, `"host_lost_after_seconds":180`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET body lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "key_hash") || strings.Contains(body, "thread_key") || strings.Contains(body, "register_key") || strings.Contains(body, "register_nonce") || strings.Contains(body, `"turn_id"`) || strings.Contains(body, `"seq"`) {
		t.Fatalf("list returned a key, hash, turn id or sequence: %s", body)
	}
}

type memAudit struct {
	rows []string
}

func (m *memAudit) Record(_ *http.Request, action, target, status, detail string) {
	m.rows = append(m.rows, action+" "+target+" "+status+" "+detail)
}

func postBeat(h *Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.HandleBeat(w, httptest.NewRequest(http.MethodPost, "/api/v1/agent/threads/heartbeat", strings.NewReader(body)))
	return w
}

func TestSecondReporterCannotEndOrRevive(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	audit := &memAudit{}
	h := NewHandler(rec, DefaultConfig(), nil, audit)
	h.now = rec.now
	const thread = `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a"`
	first := postBeat(h, thread+`,"event":"turn_start","seq":1}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", first.Code, first.Body.String())
	}
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil || len(issued.Key) != 64 {
		t.Fatalf("key: %s", first.Body.String())
	}
	end := postBeat(h, thread+`,"event":"turn_end","seq":2}`)
	if end.Code != http.StatusConflict {
		t.Fatalf("end without key: %d %s", end.Code, end.Body.String())
	}
	wrong := postBeat(h, thread+`,"event":"turn_end","seq":2,"thread_key":"`+strings.Repeat("ab", 32)+`"}`)
	if wrong.Code != http.StatusConflict {
		t.Fatalf("end with a foreign key: %d %s", wrong.Code, wrong.Body.String())
	}
	st, err := rec.State()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Threads[Key("codex", "t1")]; got.Event != TurnStart {
		t.Fatalf("thread changed without the key: %+v", got)
	}
	ok := postBeat(h, thread+`,"event":"turn_end","seq":2,"thread_key":"`+issued.Key+`"}`)
	if ok.Code != http.StatusAccepted {
		t.Fatalf("holder end: %d %s", ok.Code, ok.Body.String())
	}
	st, _ = rec.State()
	if st.Threads[Key("codex", "t1")].Event != TurnEnd {
		t.Fatal("holder could not end the thread")
	}
	revive := postBeat(h, thread+`,"event":"before_tool","tool":"Bash","seq":3}`)
	if revive.Code != http.StatusConflict {
		t.Fatalf("revive without key: %d %s", revive.Code, revive.Body.String())
	}
	st, _ = rec.State()
	if st.Threads[Key("codex", "t1")].Event != TurnEnd {
		t.Fatal("a second reporter revived the thread")
	}
	if len(audit.rows) != 3 {
		t.Fatalf("audit rows: %v", audit.rows)
	}
	for _, row := range audit.rows {
		if strings.Contains(row, issued.Key) || strings.Contains(row, "key_hash") {
			t.Fatalf("audit leaked a key: %s", row)
		}
	}
	// A presented key is not adopted as the thread's key. Registration issues a
	// different one and retires the presented key.
	fresh := postBeat(h, `{"thread":"t2","vendor":"codex","host":"mbp","turn_id":"turn-b","event":"turn_start","seq":1,"thread_key":"`+issued.Key+`"}`)
	if fresh.Code != http.StatusAccepted {
		t.Fatalf("stale key re-register: %d %s", fresh.Code, fresh.Body.String())
	}
	var again struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(fresh.Body.Bytes(), &again); err != nil || again.Key == "" || again.Key == issued.Key {
		t.Fatalf("stale key was reused: %s", fresh.Body.String())
	}
	st, _ = rec.State()
	got := st.Threads[Key("codex", "t2")]
	if got.KeyHash != HashKey(again.Key) {
		t.Fatalf("stored hash is not the issued key: %s", got.KeyHash)
	}
	retired := false
	for _, h := range got.SupersededKeys {
		if h == HashKey(issued.Key) {
			retired = true
		}
	}
	if !retired {
		t.Fatal("presented key was not retired")
	}
}

func TestOutOfOrderEventIsIgnored(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	h.now = rec.now
	const head = `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-a","thread_key":"`
	first := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":1}`)
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil || first.Code != http.StatusAccepted {
		t.Fatal(first.Body.String())
	}
	// seq 3 ends the turn. A delayed seq 2 must not reopen it, even though it arrives later.
	rec.now = func() time.Time { return t0.Add(2 * time.Minute) }
	end := postBeat(h, head+issued.Key+`","event":"turn_end","seq":3}`)
	if end.Code != http.StatusAccepted {
		t.Fatal(end.Body.String())
	}
	rec.now = func() time.Time { return t0.Add(3 * time.Minute) }
	late := postBeat(h, head+issued.Key+`","event":"before_tool","tool":"Bash","tool_timeout_s":3600,"seq":2}`)
	if late.Code != http.StatusAccepted || !strings.Contains(late.Body.String(), `"ignored":true`) {
		t.Fatalf("late event: %d %s", late.Code, late.Body.String())
	}
	st, _ := rec.State()
	got := st.Threads[Key("claude", "t1")]
	if got.Event != TurnEnd || got.Seq != 3 || got.ToolTimeoutS != 0 {
		t.Fatalf("older event applied: %+v", got)
	}
}

func TestAfterToolClearsDeclaredTimeout(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, BeforeTool, "Bash", 3600)
	r.at(time.Second)
	r.beat(t, AfterTool, "Bash", 3600)
	st, _ := r.store.Load()
	got := st.Threads[Key("claude", "7e939cd5-b3b1-4705-96e4-0a3b9e61648d")]
	if got.Event != AfterTool || got.ToolTimeoutS != 0 {
		t.Fatalf("timeout not cleared: %+v", got)
	}
	if d := DefaultConfig().Threshold(got); d != 10*time.Minute {
		t.Fatalf("threshold after the tool ended: %s", d)
	}
}

func TestAuthSuccessDoesNotChangeThreadState(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, TurnStart, "", 0)
	b := Beat{
		Thread: "7e939cd5-b3b1-4705-96e4-0a3b9e61648d", Vendor: "claude", Host: "mbp",
		Event: WaitingOwner, Reason: "auth_success", TurnID: "turn-1", Seq: r.seq + 1,
	}
	if err := b.Validate(); err == nil {
		t.Fatal("auth_success was accepted as waiting")
	}
	st, _ := r.store.Load()
	if st.Threads[Key("claude", "7e939cd5-b3b1-4705-96e4-0a3b9e61648d")].Event != TurnStart {
		t.Fatal("auth_success changed the thread")
	}
}

func TestWaitingThreadIsNotPushed(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, TurnStart, "", 0)
	r.at(time.Minute)
	r.beat(t, WaitingOwner, "", 0)
	r.at(2 * time.Hour)
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatalf("waiting thread pushed: %v", r.page.sent)
	}
	views := DefaultConfig().Views(mustState(t, r), r.clk.at)
	if len(views) != 1 || views[0].Status != StatusWaiting || views[0].Reason != "permission_prompt" {
		t.Fatalf("view: %+v", views)
	}
	if views[0].QuietSeconds < 3600 {
		t.Fatalf("wait age: %d", views[0].QuietSeconds)
	}
}

func TestHostLostPushesOnceAndReturnPushesNone(t *testing.T) {
	r := newRig(t, "")
	r.beatOn(t, "thread-a", "mbp", TurnStart, "", 0)
	r.beatOn(t, "thread-b", "mbp", BeforeTool, "Bash", 30)
	r.beatOn(t, "thread-c", "mbp", TurnEnd, "", 0)
	wired := map[string]VendorReport{
		"claude": {Wired: true, Token: true},
		"cursor": {Wired: true, Token: false},
		"codex":  {Wired: false, Token: true},
	}
	r.host(t, "mbp", wired)
	r.at(3 * time.Minute)
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatal("pushed at exactly the host threshold")
	}
	r.at(3*time.Minute + time.Second)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("want one host push, got %d (%v)", r.page.count(), r.page.sent)
	}
	if !strings.Contains(r.page.sent[0], "Host lost") || !strings.Contains(r.page.sent[0], "mbp") {
		t.Fatalf("push: %s", r.page.sent[0])
	}
	views := DefaultConfig().Views(mustState(t, r), r.clk.at)
	byID := map[string]Status{}
	for _, v := range views {
		byID[v.Thread.Thread] = v.Status
	}
	if byID["thread-a"] != HostLost || byID["thread-b"] != HostLost || byID["thread-c"] != Idle {
		t.Fatalf("statuses: %v", byID)
	}
	hosts := DefaultConfig().HostViews(mustState(t, r), r.clk.at)
	if len(hosts) != 1 || hosts[0].Status != HostLostState {
		t.Fatalf("host view: %+v", hosts)
	}
	monitored := map[string]bool{}
	for _, v := range hosts[0].Vendors {
		monitored[v.Vendor] = v.Monitored
	}
	if monitored["claude"] != true || monitored["cursor"] || monitored["codex"] {
		t.Fatalf("monitored: %v", monitored)
	}
	r.tick(t)
	r.at(15 * time.Minute)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("lost host pushed again, or its threads paged as silent: %v", r.page.sent)
	}
	r.at(16 * time.Minute)
	r.host(t, "mbp", wired)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("returning host pushed: %v", r.page.sent)
	}
	back := DefaultConfig().Views(mustState(t, r), r.clk.at)
	for _, v := range back {
		if v.Thread.Thread == "thread-a" && v.Status != Silent {
			t.Fatalf("returned thread is quiet and listed silent: %+v", v)
		}
	}
	r.at(16*time.Minute + 3*time.Minute + time.Second)
	r.tick(t)
	if r.page.count() != 2 {
		t.Fatalf("a later loss should page once more, got %d", r.page.count())
	}
}

func TestHostLostAfterComesFromEnv(t *testing.T) {
	t.Setenv("PLATFORM_AGENT_THREAD_HOST_LOST", "90s")
	if got := ConfigFromEnv().HostLostAfter; got != 90*time.Second {
		t.Fatalf("host threshold: %s", got)
	}
}

func TestLostFirstResponseReplaysNonce(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	h.now = rec.now
	const nonce = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const head = `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":1,"register_nonce":"` + nonce + `"}`
	first := postBeat(h, head)
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil || first.Code != http.StatusAccepted || len(issued.Key) != 64 {
		t.Fatalf("register: %d %s", first.Code, first.Body.String())
	}
	// The client never saw the key. The same nonce, still inside the window, returns it.
	retry := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"before_tool","tool":"Bash","seq":2,"register_nonce":"`+nonce+`"}`)
	var again struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &again); err != nil || retry.Code != http.StatusAccepted || again.Key != issued.Key {
		t.Fatalf("replay: %d %s", retry.Code, retry.Body.String())
	}
	st, _ := rec.State()
	if st.Threads[Key("codex", "t1")].Event != BeforeTool || st.Threads[Key("codex", "t1")].Seq != 2 {
		t.Fatalf("replay did not store the newer event: %+v", st.Threads[Key("codex", "t1")])
	}
	rec.now = func() time.Time { return t0.Add(registerWindow + time.Second) }
	h.now = rec.now
	late := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"after_tool","tool":"Bash","seq":3,"register_nonce":"`+nonce+`"}`)
	if late.Code != http.StatusConflict || !strings.Contains(late.Body.String(), "unknown key") {
		t.Fatalf("expired nonce: %d %s", late.Code, late.Body.String())
	}
	st, _ = rec.State()
	if st.Threads[Key("codex", "t1")].RegisterKey != "" {
		t.Fatal("plaintext key lived past the window")
	}
	if st.Threads[Key("codex", "t1")].Seq != 2 {
		t.Fatal("expired retry changed the thread")
	}
}

func TestUnknownKeyLeavesTheThread(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	first := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":1}`)
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil || first.Code != http.StatusAccepted {
		t.Fatal(first.Body.String())
	}
	missing := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"turn_end","seq":2}`)
	if missing.Code != http.StatusConflict || !strings.Contains(missing.Body.String(), `"error":"unknown key"`) {
		t.Fatalf("missing key: %d %s", missing.Code, missing.Body.String())
	}
	st, _ := rec.State()
	if st.Threads[Key("codex", "t1")].Event != TurnStart || st.Threads[Key("codex", "t1")].KeyHash != HashKey(issued.Key) {
		t.Fatal("unknown key replaced the thread")
	}
}

func TestLegacyRecordMigratesOnNextEvent(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	if err := rec.store.Update(func(st *State) error {
		st.Threads[Key("codex", "old")] = Thread{
			Thread: "old", Vendor: "codex", Host: "mbp", Event: TurnStart,
			TurnID: "turn-a", Seq: 1, At: t0, TurnStartedAt: t0,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	const presented = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	res := postBeat(h, `{"thread":"old","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"before_tool","tool":"Bash","seq":2,"thread_key":"`+presented+`"}`)
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil || res.Code != http.StatusAccepted || issued.Key == "" || issued.Key == presented {
		t.Fatalf("migrate: %d %s", res.Code, res.Body.String())
	}
	st, _ := rec.State()
	got := st.Threads[Key("codex", "old")]
	if got.Event != BeforeTool || got.KeyHash != HashKey(issued.Key) || got.KeyHash == HashKey(presented) {
		t.Fatalf("legacy record was not migrated: %+v", got)
	}
	denied := postBeat(h, `{"thread":"old","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"turn_end","seq":3}`)
	if denied.Code != http.StatusConflict {
		t.Fatalf("migrated thread accepted a missing key: %d", denied.Code)
	}
}

func TestPrunedThreadReregistersWithoutReusingKey(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	first := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":1}`)
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil || first.Code != http.StatusAccepted {
		t.Fatal(first.Body.String())
	}
	if err := rec.store.Update(func(st *State) error {
		delete(st.Threads, Key("codex", "t1"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	again := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-b","event":"turn_start","seq":1,"thread_key":"`+issued.Key+`"}`)
	var next struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &next); err != nil || again.Code != http.StatusAccepted || next.Key == "" || next.Key == issued.Key {
		t.Fatalf("re-register: %d %s", again.Code, again.Body.String())
	}
	stale := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-b","event":"turn_end","seq":2,"thread_key":"`+issued.Key+`"}`)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "unknown key") {
		t.Fatalf("stale key still worked: %d %s", stale.Code, stale.Body.String())
	}
	ok := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-b","event":"turn_end","seq":2,"thread_key":"`+next.Key+`"}`)
	if ok.Code != http.StatusAccepted {
		t.Fatalf("new key: %d %s", ok.Code, ok.Body.String())
	}
}

func TestOlderTurnRefusedEvenWithNewerSeq(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	first := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":1}`)
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil || first.Code != http.StatusAccepted {
		t.Fatal(first.Body.String())
	}
	next := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-b","event":"turn_start","seq":2,"thread_key":"`+issued.Key+`"}`)
	if next.Code != http.StatusAccepted {
		t.Fatal(next.Body.String())
	}
	older := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-a","event":"before_tool","tool":"Bash","seq":9,"thread_key":"`+issued.Key+`"}`)
	if older.Code != http.StatusConflict || !strings.Contains(older.Body.String(), "older turn refused") {
		t.Fatalf("older turn: %d %s", older.Code, older.Body.String())
	}
	st, _ := rec.State()
	got := st.Threads[Key("claude", "t1")]
	if got.TurnID != "turn-b" || got.Seq != 2 || got.Event != TurnStart {
		t.Fatalf("older turn was stored: %+v", got)
	}
	reopen := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":10,"thread_key":"`+issued.Key+`"}`)
	if reopen.Code != http.StatusConflict {
		t.Fatalf("closed turn reopened: %d %s", reopen.Code, reopen.Body.String())
	}
}

func TestExpiredSilenceClaimIsRetried(t *testing.T) {
	r := newRig(t, "")
	r.beat(t, AfterTool, "Edit", 0)
	r.at(11 * time.Minute)
	k := Key("claude", "7e939cd5-b3b1-4705-96e4-0a3b9e61648d")
	th := mustState(t, r).Threads[k]
	id, ok, err := r.w.claim(k, th.At, r.clk.at)
	if err != nil || !ok || id == "" {
		t.Fatalf("claim: %v %v %s", ok, err, id)
	}
	if mustState(t, r).Threads[k].Pushed() {
		t.Fatal("claim marked the silence sent")
	}
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatal("a live claim was pushed")
	}
	r.at(11*time.Minute + claimTTL + time.Second)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("expired claim was not retried: %d", r.page.count())
	}
	if !mustState(t, r).Threads[k].Pushed() {
		t.Fatal("retry did not mark the silence sent")
	}
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("sent twice: %d", r.page.count())
	}
}

func TestExpiredHostClaimIsRetried(t *testing.T) {
	r := newRig(t, "")
	r.host(t, "mbp", map[string]VendorReport{"claude": {Wired: true, Token: true}})
	r.at(3*time.Minute + time.Second)
	h := mustState(t, r).Hosts["mbp"]
	id, ok, err := r.w.claimHost("mbp", h.At, r.clk.at)
	if err != nil || !ok || id == "" {
		t.Fatalf("claim: %v %v", ok, err)
	}
	r.tick(t)
	if r.page.count() != 0 {
		t.Fatal("a live host claim was pushed")
	}
	r.at(3*time.Minute + time.Second + claimTTL + time.Second)
	r.tick(t)
	if r.page.count() != 1 {
		t.Fatalf("expired host claim was not retried: %d", r.page.count())
	}
	if !mustState(t, r).Hosts["mbp"].LostNotifiedFor.Equal(h.At) {
		t.Fatal("retry did not mark the loss sent")
	}
}

func TestLiveThreadsFillTheCap(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = func() time.Time { return t0 }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	before, _ := RegistrationRefusals()
	keys := make([]string, maxThreads)
	for i := 0; i < maxThreads; i++ {
		res := postBeat(h, fmt.Sprintf(`{"thread":"t%d","vendor":"codex","host":"mbp","turn_id":"turn","event":"turn_start","seq":1}`, i))
		var issued struct {
			Key string `json:"thread_key"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &issued); err != nil || res.Code != http.StatusAccepted || issued.Key == "" {
			t.Fatalf("seed %d: %d %s", i, res.Code, res.Body.String())
		}
		keys[i] = issued.Key
	}
	refused := postBeat(h, `{"thread":"overflow","vendor":"codex","host":"mbp","turn_id":"turn","event":"turn_start","seq":1}`)
	if refused.Code != http.StatusTooManyRequests || !strings.Contains(refused.Body.String(), "thread capacity full") {
		t.Fatalf("full: %d %s", refused.Code, refused.Body.String())
	}
	threads, _ := RegistrationRefusals()
	if threads != before+1 {
		t.Fatalf("refusal count %d, before %d", threads, before)
	}
	st, _ := rec.State()
	if _, ok := st.Threads[Key("codex", "overflow")]; ok || len(st.Threads) != maxThreads {
		t.Fatalf("overflow stored, len %d", len(st.Threads))
	}
	// A finished thread is the one that makes room. The oldest mid-turn stays.
	end := postBeat(h, `{"thread":"t0","vendor":"codex","host":"mbp","turn_id":"turn","event":"turn_end","seq":2,"thread_key":"`+keys[0]+`"}`)
	if end.Code != http.StatusAccepted {
		t.Fatal(end.Body.String())
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	ok := postBeat(h, `{"thread":"overflow","vendor":"codex","host":"mbp","turn_id":"turn","event":"turn_start","seq":1}`)
	if ok.Code != http.StatusAccepted {
		t.Fatalf("after eviction: %d %s", ok.Code, ok.Body.String())
	}
	st, _ = rec.State()
	if _, kept := st.Threads[Key("codex", "t0")]; kept {
		t.Fatal("finished thread was kept past the cap")
	}
	if _, kept := st.Threads[Key("codex", "t1")]; !kept {
		t.Fatal("a mid-turn thread was evicted")
	}
	if _, kept := st.Threads[Key("codex", "overflow")]; !kept || len(st.Threads) != maxThreads {
		t.Fatalf("new thread not kept, len %d", len(st.Threads))
	}
}

func TestFullHostListRefusesAndKeepsLost(t *testing.T) {
	rec := NewRecorder(NewStore(""), DefaultConfig())
	clk := &clock{at: t0}
	rec.now = clk.now
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	postHost := func(name string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		body := `{"host":"` + name + `","vendors":{"claude":{"wired":true,"token":true}}}`
		h.HandleHost(w, httptest.NewRequest(http.MethodPost, "/api/v1/agent/hosts/heartbeat", strings.NewReader(body)))
		return w
	}
	for i := 0; i < maxHosts; i++ {
		res := postHost(fmt.Sprintf("h%d", i))
		if res.Code != http.StatusAccepted {
			t.Fatalf("seed %d: %d %s", i, res.Code, res.Body.String())
		}
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	_, before := RegistrationRefusals()
	extra := postHost("overflow")
	if extra.Code != http.StatusTooManyRequests || !strings.Contains(extra.Body.String(), "host capacity full") {
		t.Fatalf("full: %d %s", extra.Code, extra.Body.String())
	}
	_, hosts := RegistrationRefusals()
	if hosts != before+1 {
		t.Fatalf("host refusal count %d, before %d", hosts, before)
	}
	clk.at = t0.Add(4 * time.Minute)
	extra = postHost("overflow")
	if extra.Code != http.StatusTooManyRequests {
		t.Fatalf("lost hosts were evicted: %d %s", extra.Code, extra.Body.String())
	}
	st, _ := rec.State()
	if len(st.Hosts) != maxHosts {
		t.Fatalf("host len %d", len(st.Hosts))
	}
	if _, ok := st.Hosts["h0"]; !ok {
		t.Fatal("a lost host was removed")
	}
	if _, ok := st.Hosts["overflow"]; ok {
		t.Fatal("refused host was stored")
	}
}

func TestExpectedHostNeverReported(t *testing.T) {
	t.Setenv("PLATFORM_AGENT_THREAD_EXPECTED_HOSTS", "mini, mbp, mini, bad name,")
	cfg := ConfigFromEnv()
	if len(cfg.ExpectedHosts) != 2 || cfg.ExpectedHosts[0] != "mini" || cfg.ExpectedHosts[1] != "mbp" {
		t.Fatalf("expected hosts: %#v", cfg.ExpectedHosts)
	}
	st := State{Hosts: map[string]Host{
		"mbp": {Host: "mbp", At: t0.Add(-time.Hour), Vendors: map[string]VendorReport{"claude": {Wired: true, Token: true}}},
	}}
	views := cfg.HostViews(st, t0)
	if len(views) != 2 {
		t.Fatalf("views: %+v", views)
	}
	if views[0].Host != "mini" || views[0].Status != HostNeverReported || !views[0].At.IsZero() {
		t.Fatalf("never reported: %+v", views[0])
	}
	for _, v := range views[0].Vendors {
		if v.Monitored {
			t.Fatalf("never reported vendor looks monitored: %+v", v)
		}
	}
	if views[1].Host != "mbp" || views[1].Status != HostLostState {
		t.Fatalf("reported host: %+v", views[1])
	}
}

func TestHostLostNoticeIsThreeToFourMinutes(t *testing.T) {
	notice := DefaultConfig().HostLostAfter + FlushInterval + WatchInterval
	if notice < 3*time.Minute || notice > 4*time.Minute {
		t.Fatalf("host lost notice %s, want 3 to 4 minutes", notice)
	}
}

func mustState(t *testing.T, r *rig) State {
	t.Helper()
	st, err := r.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	return st
}
