package agentthreads

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
	if st.ThreadsRefused != 1 || !st.LastThreadRefusal.Equal(t0) {
		t.Fatalf("refusal record: %d %s", st.ThreadsRefused, st.LastThreadRefusal)
	}
	listed := httptest.NewRecorder()
	h.HandleList(listed, httptest.NewRequest(http.MethodGet, "/api/v1/agent/threads", nil))
	if !strings.Contains(listed.Body.String(), `"threads_refused":1`) || !strings.Contains(listed.Body.String(), `"last_thread_refusal":"2026-10-10T04:03:00Z"`) {
		t.Fatalf("list hid the refusal: %s", listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "register_key") || strings.Contains(listed.Body.String(), "overflow") {
		t.Fatalf("list stored the refused thread or a key: %s", listed.Body.String())
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
	if st.ThreadsRefused != 1 {
		t.Fatalf("a later registration cleared the refusal count: %d", st.ThreadsRefused)
	}
}

func TestUnstartedTurnStaysRefusedUntilTurnStart(t *testing.T) {
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
	// The turn_start for turn-b never arrived. A later event of that turn is refused.
	missed := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-b","event":"before_tool","tool":"Bash","seq":2,"thread_key":"`+issued.Key+`"}`)
	if missed.Code != http.StatusConflict || !strings.Contains(missed.Body.String(), "older turn refused") {
		t.Fatalf("unstarted turn was accepted: %d %s", missed.Code, missed.Body.String())
	}
	st, _ := rec.State()
	if st.Threads[Key("claude", "t1")].TurnID != "turn-a" {
		t.Fatal("a refused event moved the turn")
	}
	opened := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-b","event":"turn_start","seq":2,"thread_key":"`+issued.Key+`"}`)
	if opened.Code != http.StatusAccepted {
		t.Fatal(opened.Body.String())
	}
	next := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-b","event":"before_tool","tool":"Bash","seq":3,"thread_key":"`+issued.Key+`"}`)
	if next.Code != http.StatusAccepted {
		t.Fatal(next.Body.String())
	}
	st, _ = rec.State()
	got := st.Threads[Key("claude", "t1")]
	if got.TurnID != "turn-b" || got.Event != BeforeTool {
		t.Fatalf("server did not end on the new turn: %+v", got)
	}
	// A finished turn stays refused. turn-a has been left.
	back := postBeat(h, `{"thread":"t1","vendor":"claude","host":"mbp","turn_id":"turn-a","event":"before_tool","tool":"Bash","seq":4,"thread_key":"`+issued.Key+`"}`)
	if back.Code != http.StatusConflict || !strings.Contains(back.Body.String(), "older turn refused") {
		t.Fatalf("finished turn was reopened: %d %s", back.Code, back.Body.String())
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
