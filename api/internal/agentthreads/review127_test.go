package agentthreads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReview127IdleSiblingCannotHideSilent(t *testing.T) {
	r := newRig(t, "")
	r.beatSession(t, "a", sidebarID, "mbp", TurnStart)
	r.at(time.Minute)
	r.beatSession(t, "b", sidebarID, "mbp", TurnEnd)
	r.at(12 * time.Minute)
	st := mustState(t, r)
	cfg := DefaultConfig()
	if cfg.sessionSuperseded(st, st.Threads[Key("claude", "a")], r.clk.at) {
		t.Fatal("a incorrectly superseded")
	}
	views := cfg.Views(st, r.clk.at)
	push, _ := cfg.Due(st, r.clk.at)
	if len(views) != 2 || views[1].Thread.Thread != "a" || views[1].Status != Silent || len(push) != 1 {
		t.Fatalf("silent session hidden: views=%+v push=%v", views, push)
	}
}

func TestReview127PresentEmptySidebarUsesIDRule(t *testing.T) {
	for _, value := range []string{`""`, `" "`, `null`} {
		rec := NewRecorder(NewStore(""), DefaultConfig())
		h := NewHandler(rec, DefaultConfig(), nil, nil)
		body := `{"thread":"a","vendor":"claude","host":"mbp","event":"turn_start","turn_id":"t1","seq":1,"sidebar":` + value + `}`
		w := httptest.NewRecorder()
		h.HandleBeat(w, httptest.NewRequest(http.MethodPost, "/api/v1/agent/threads/heartbeat", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("present sidebar=%s accepted: %d", value, w.Code)
		}
	}
}

func TestReview127RestartBetweenDueAndClaimDoesNotPush(t *testing.T) {
	r := newRig(t, "")
	r.beatSession(t, "a", sidebarID, "mbp", TurnStart)
	r.at(11 * time.Minute)
	cfg := DefaultConfig()
	snapshot := mustState(t, r)
	push, _ := cfg.Due(snapshot, r.clk.at)
	r.beatSession(t, "b", sidebarID, "mbp", TurnStart)
	r.w.pushThreads(context.Background(), snapshot, push, nil, r.clk.at)
	if r.page.count() != 0 {
		t.Fatalf("live restarted thread received %d stale push", r.page.count())
	}
}

func TestReview127SidebarAcrossVendorsIsOneItem(t *testing.T) {
	st := State{}
	for i, vendor := range []string{"claude", "cursor"} {
		b := Beat{Thread: "sess-" + vendor, Vendor: vendor, Host: "mbp", Sidebar: sidebarID, Event: TurnStart, TurnID: "t1", Seq: 1}
		if err := b.Validate(); err != nil {
			t.Fatal(err)
		}
		st.Apply(b, t0.Add(time.Duration(i)*time.Minute))
	}
	cfg := DefaultConfig()
	views := cfg.Views(st, t0.Add(12*time.Minute))
	push, _ := cfg.Due(st, t0.Add(12*time.Minute))
	if len(views) != 2 || views[0].Thread.Thread != "sess-cursor" || views[0].Status != Silent || views[1].Status != Superseded || len(push) != 1 || push[0] != Key("cursor", "sess-cursor") {
		t.Fatalf("one sidebar duplicated: views=%d push=%v", len(views), push)
	}
}

// Planted violation: use sibling's last event instead of its actual turn opening.
func TestSupersedeRequiresOpeningAfterLastEvent(t *testing.T) {
	r := newRig(t, "")
	r.beatSession(t, "b", sidebarID, "mbp", TurnStart)
	r.at(time.Minute)
	r.beatSession(t, "a", sidebarID, "mbp", TurnStart)
	r.at(2 * time.Minute)
	r.beatSession(t, "b", sidebarID, "mbp", AfterTool)
	r.at(13 * time.Minute)
	cfg := DefaultConfig()
	st := mustState(t, r)
	for _, v := range cfg.Views(st, r.clk.at) {
		if v.Status != Silent {
			t.Fatalf("an old opening superseded a newer event: %+v", v)
		}
	}
	// Even turn_end followed by a tool event must not manufacture an opening.
	r.beatSession(t, "b", sidebarID, "mbp", TurnEnd)
	r.at(14 * time.Minute)
	r.beatSession(t, "b", sidebarID, "mbp", BeforeTool)
	st = mustState(t, r)
	if cfg.sessionSuperseded(st, st.Threads[Key("claude", "a")], r.clk.at) {
		t.Fatal("non-start event manufactured an opening")
	}
}

// Planted violation: only validate the snapshot timestamp, not current status.
func TestClaimRechecksLiveSession(t *testing.T) {
	r := newRig(t, "")
	r.beatSession(t, "a", sidebarID, "mbp", TurnStart)
	r.at(11 * time.Minute)
	snapshot := mustState(t, r)
	push, _ := DefaultConfig().Due(snapshot, r.clk.at)
	// A same-clock event can change state without moving At.
	if err := r.store.Update(func(st *State) error {
		th := st.Threads[Key("claude", "a")]
		th.Event = WaitingOwner
		st.Threads[Key("claude", "a")] = th
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.w.pushThreads(context.Background(), snapshot, push, nil, r.clk.at)
	if r.page.count() != 0 {
		t.Fatal("waiting session pushed from stale snapshot")
	}
}

func TestWrappedSessionIsNeverSuperseded(t *testing.T) {
	r := newRig(t, "")
	r.beatSession(t, "run-a", sidebarID, "mbp", TurnStart)
	r.at(time.Minute)
	r.beatSession(t, "b", sidebarID, "mbp", TurnStart)
	r.at(12 * time.Minute)
	for _, v := range DefaultConfig().Views(mustState(t, r), r.clk.at) {
		if v.Status != Silent {
			t.Fatalf("wrapped session superseded: %+v", v)
		}
	}
}
