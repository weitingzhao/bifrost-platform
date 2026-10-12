package agentthreads

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sidebarID = "local_4a4cc96c-2607-4ed7-896d-9f9429571ab4"

func (r *rig) beatSession(t *testing.T, thread, sidebar, host string, ev Event) {
	t.Helper()
	r.seq++
	b := Beat{
		Thread: thread, Vendor: "claude", Host: host, Work: "W-54",
		Event: ev, TurnID: "turn-" + thread, Seq: r.seq, Sidebar: sidebar,
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	r.rec.Record(b)
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
}

func viewIDs(views []View) []string {
	out := make([]string, len(views))
	for i, v := range views {
		out[i] = v.Thread.Thread
	}
	return out
}

// Planted violation: reject a beat that omits sidebar, or accept a sidebar
// value that an invalid thread id would reject.
func TestOptionalSidebarValidatedLikeThreadID(t *testing.T) {
	clk := &clock{at: t0}
	rec := NewRecorder(NewStore(""), DefaultConfig())
	rec.now = clk.now
	h := NewHandler(rec, DefaultConfig(), nil, nil)
	h.now = clk.now
	post := func(body string) (int, string) {
		w := httptest.NewRecorder()
		h.HandleBeat(w, httptest.NewRequest(http.MethodPost, "/api/v1/agent/threads/heartbeat", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	without := `{"thread":"sess-old","vendor":"claude","host":"mbp","event":"turn_start","turn_id":"t1","seq":1}`
	if code, body := post(without); code != http.StatusAccepted {
		t.Fatalf("beat without sidebar: %d %s", code, body)
	}
	with := `{"thread":"sess-new","vendor":"claude","host":"mbp","event":"turn_start","turn_id":"t1","seq":1,"sidebar":"` + sidebarID + `"}`
	if code, body := post(with); code != http.StatusAccepted {
		t.Fatalf("beat with sidebar: %d %s", code, body)
	}
	badThread := `{"thread":"bad id","vendor":"claude","host":"mbp","event":"turn_start","turn_id":"t1","seq":1}`
	badSidebar := `{"thread":"sess-bad","vendor":"claude","host":"mbp","event":"turn_start","turn_id":"t1","seq":1,"sidebar":"bad id"}`
	codeThread, bodyThread := post(badThread)
	codeSidebar, bodySidebar := post(badSidebar)
	if codeThread != http.StatusBadRequest || codeSidebar != codeThread {
		t.Fatalf("invalid sidebar status %d, invalid thread %d", codeSidebar, codeThread)
	}
	const want = "want 1-128 of [A-Za-z0-9._:-]"
	if !strings.Contains(bodyThread, want) || !strings.Contains(bodySidebar, want) {
		t.Fatalf("sidebar error %s, thread error %s", bodySidebar, bodyThread)
	}

	var kept Beat
	if err := json.Unmarshal([]byte(without), &kept); err != nil {
		t.Fatal(err)
	}
	if err := kept.Validate(); err != nil {
		t.Fatal(err)
	}
	if kept.Sidebar != "" {
		t.Fatalf("missing sidebar was stored on the beat as %q", kept.Sidebar)
	}
	ok := Beat{Thread: "sess-ok", Vendor: "claude", Host: "mbp", Event: TurnStart, TurnID: "t1", Seq: 1, Sidebar: sidebarID}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}

	r := newRig(t, "")
	r.beatSession(t, "sess-keep", sidebarID, "mbp", TurnStart)
	r.beatSession(t, "sess-keep", "", "mbp", AfterTool)
	th := mustState(t, r).Threads[Key("claude", "sess-keep")]
	if th.Sidebar != sidebarID || !th.OpenedTurn || th.Event != AfterTool {
		t.Fatalf("later beat without sidebar: sidebar %q opened %v event %s", th.Sidebar, th.OpenedTurn, th.Event)
	}
	raw, err := json.Marshal(DefaultConfig().Views(mustState(t, r), r.clk.at)[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"sidebar"`) || strings.Contains(string(raw), `"opened_turn"`) {
		t.Fatalf("list gained a field: %s", raw)
	}
}

// Planted violation: supersede as soon as the other session opens a turn, or
// supersede a session that has merely gone quiet, or treat a wrapped run as
// that other session.
func TestSupersedeNeedsAnotherTurnAndSilence(t *testing.T) {
	const (
		a   = "aaaaaaaa-1111-2222-3333-444455556666"
		b   = "bbbbbbbb-1111-2222-3333-444455556666"
		run = "run-bc611f64-0000-4000-8000-000000000099"
	)
	cfg := DefaultConfig()

	live := newRig(t, "")
	live.beatSession(t, a, sidebarID, "mbp", TurnStart)
	live.at(time.Minute)
	live.beatSession(t, b, sidebarID, "mbp", TurnStart)
	st := mustState(t, live)
	for _, id := range []string{a, b} {
		th := st.Threads[Key("claude", id)]
		if th.Event == TurnEnd || cfg.sessionSuperseded(st, th, live.clk.at) {
			t.Fatalf("live session %s was closed: event %s", id, th.Event)
		}
	}

	quietOnly := newRig(t, "")
	quietOnly.beatSession(t, a, sidebarID, "mbp", TurnStart)
	quietOnly.at(11 * time.Minute)
	quietOnly.beatSession(t, b, sidebarID, "mbp", BeforeTool)
	st = mustState(t, quietOnly)
	if cfg.sessionSuperseded(st, st.Threads[Key("claude", a)], quietOnly.clk.at) {
		t.Fatal("silence alone superseded a session whose sibling never opened a turn")
	}

	both := newRig(t, "")
	both.beatSession(t, a, sidebarID, "mbp", TurnStart)
	both.at(time.Minute)
	both.beatSession(t, b, sidebarID, "mbp", TurnStart)
	both.at(12 * time.Minute)
	st = mustState(t, both)
	if !cfg.sessionSuperseded(st, st.Threads[Key("claude", a)], both.clk.at) {
		t.Fatal("old session was not superseded after the other turn and the silent threshold")
	}
	if cfg.sessionSuperseded(st, st.Threads[Key("claude", b)], both.clk.at) {
		t.Fatal("the later session was superseded by the one it replaced")
	}

	alone := newRig(t, "")
	alone.beatSession(t, a, sidebarID, "mbp", TurnStart)
	alone.at(11 * time.Minute)
	st = mustState(t, alone)
	if cfg.sessionSuperseded(st, st.Threads[Key("claude", a)], alone.clk.at) {
		t.Fatal("the only session of a thread was superseded")
	}

	wrapped := newRig(t, "")
	wrapped.beatSession(t, a, sidebarID, "mbp", TurnStart)
	wrapped.at(11 * time.Minute)
	wrapped.beatSession(t, run, sidebarID, "mbp", TurnStart)
	st = mustState(t, wrapped)
	if cfg.sessionSuperseded(st, st.Threads[Key("claude", a)], wrapped.clk.at) {
		t.Fatal("a wrapped run superseded the desktop session")
	}
}

// Planted violation: list each session, or call the thread silent while one
// of its sessions is still live.
func TestOwnerListsTheThreadOnce(t *testing.T) {
	const (
		a   = "aaaaaaaa-1111-2222-3333-444455556666"
		b   = "bbbbbbbb-1111-2222-3333-444455556666"
		run = "run-bc611f64-0000-4000-8000-000000000099"
	)
	cfg := DefaultConfig()

	live := newRig(t, "")
	live.beatSession(t, a, sidebarID, "mbp", TurnStart)
	live.at(11 * time.Minute)
	live.beatSession(t, b, sidebarID, "mbp", TurnStart)
	views := cfg.Views(mustState(t, live), live.clk.at)
	if len(views) != 1 || views[0].Thread.Thread != sidebarID || views[0].Status == Silent {
		t.Fatalf("live thread: %v", viewIDs(views))
	}
	if strings.Contains(strings.Join(viewIDs(views), " "), a) {
		t.Fatalf("superseded session is a second item: %v", viewIDs(views))
	}
	push, mark := cfg.Due(mustState(t, live), live.clk.at)
	if len(push)+len(mark) != 0 {
		t.Fatalf("live thread was due: push %v mark %v", push, mark)
	}

	both := newRig(t, "")
	both.beatSession(t, a, sidebarID, "mbp", TurnStart)
	both.at(time.Minute)
	both.beatSession(t, b, sidebarID, "mbp", TurnStart)
	both.at(12 * time.Minute)
	views = cfg.Views(mustState(t, both), both.clk.at)
	if len(views) != 1 || views[0].Thread.Thread != sidebarID || views[0].Status != Silent {
		t.Fatalf("quiet thread: %+v", views)
	}
	push, _ = cfg.Due(mustState(t, both), both.clk.at)
	if len(push) != 1 || push[0] != Key("claude", b) {
		t.Fatalf("quiet thread due: %v", push)
	}
	both.tick(t)
	if both.page.count() != 1 {
		t.Fatalf("quiet thread pushes: %d", both.page.count())
	}
	msg := both.page.sent[0]
	if !strings.Contains(msg, "local_4a") || strings.Contains(msg, "aaaaaaaa") || strings.Contains(msg, "bbbbbbbb") {
		t.Fatalf("push named a session, not the thread: %s", msg)
	}

	sideBySide := newRig(t, "")
	sideBySide.beatSession(t, a, sidebarID, "mbp", TurnStart)
	sideBySide.at(time.Minute)
	sideBySide.beatSession(t, b, sidebarID, "mbp", TurnStart)
	views = cfg.Views(mustState(t, sideBySide), sideBySide.clk.at)
	if len(views) != 1 || views[0].Status == Silent || views[0].Thread.Thread != sidebarID {
		t.Fatalf("two live sessions: %v", viewIDs(views))
	}

	runRig := newRig(t, "")
	runRig.beatSession(t, a, sidebarID, "mbp", TurnStart)
	runRig.at(11 * time.Minute)
	runRig.beatSession(t, run, sidebarID, "mbp", TurnStart)
	views = cfg.Views(mustState(t, runRig), runRig.clk.at)
	gotRun, gotThread := false, false
	for _, v := range views {
		if v.Thread.Thread == run {
			gotRun = true
		}
		if v.Thread.Thread == sidebarID && v.Status == Silent {
			gotThread = true
		}
	}
	if !gotRun || !gotThread {
		t.Fatalf("wrapped run joined the sidebar thread: %v", viewIDs(views))
	}
	push, _ = cfg.Due(mustState(t, runRig), runRig.clk.at)
	if len(push) != 1 || push[0] != Key("claude", a) {
		t.Fatalf("desktop silence due: %v", push)
	}
}

// Planted violation: a session with no sidebar id does not become silent, or
// two such sessions close each other.
func TestSessionWithoutSidebarStillGoesSilent(t *testing.T) {
	const (
		c = "cccccccc-1111-2222-3333-444455556666"
		d = "dddddddd-1111-2222-3333-444455556666"
	)
	r := newRig(t, "")
	r.beatSession(t, c, "", "mbp", TurnStart)
	r.at(time.Minute)
	r.beatSession(t, d, "", "mbp", TurnStart)
	r.at(12 * time.Minute)
	cfg := DefaultConfig()
	st := mustState(t, r)
	views := cfg.Views(st, r.clk.at)
	if len(views) != 2 {
		t.Fatalf("old clients listed as %v", viewIDs(views))
	}
	for _, id := range []string{c, d} {
		th := st.Threads[Key("claude", id)]
		if cfg.sessionSuperseded(st, th, r.clk.at) || cfg.StatusOf(th, false, r.clk.at) != Silent {
			t.Fatalf("old session %s: status %s", id, cfg.StatusOf(th, false, r.clk.at))
		}
		found := false
		for _, v := range views {
			if v.Thread.Thread == id && v.Status == Silent {
				found = true
			}
		}
		if !found {
			t.Fatalf("old session %s missing from %v", id, viewIDs(views))
		}
	}
	push, _ := cfg.Due(st, r.clk.at)
	if len(push) != 2 {
		t.Fatalf("old clients due: %v", push)
	}
}
