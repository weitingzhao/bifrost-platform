package agentthreads

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	if !strings.Contains(r.page.sent[0], "Look · mbp has not reported") || !strings.Contains(r.page.sent[0], "mbp") {
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

func TestPeriodicFlushWipesRegisterKeyWhenNothingIsBuffered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-threads")
	rec := NewRecorder(NewStore(path), DefaultConfig())
	now := t0
	rec.now = func() time.Time { return now }
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	h.now = func() time.Time { return now }
	const nonce = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	res := postBeat(h, `{"thread":"t1","vendor":"codex","host":"mbp","turn_id":"turn-a","event":"turn_start","seq":1,"register_nonce":"`+nonce+`"}`)
	if res.Code != http.StatusAccepted {
		t.Fatal(res.Body.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte(`"register_key"`)) {
		t.Fatalf("register key not stored: %v %s", err, raw)
	}
	// Every reporter has stopped. A flush inside the window must not write.
	now = t0.Add(time.Minute)
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) {
		t.Fatal("periodic flush wrote while the registration window was still open")
	}
	now = t0.Add(registerWindow + time.Second)
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	got := st.Threads[Key("codex", "t1")]
	if got.RegisterKey != "" {
		t.Fatal("plaintext key survived a quiet periodic flush")
	}
	if got.KeyHash == "" {
		t.Fatal("the quiet flush removed the key hash")
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
