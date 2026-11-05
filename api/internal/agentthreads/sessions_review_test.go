package agentthreads

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// P-c2 review round 1 (M1–M4): what a side beat may not move, what a key
// keeps across a recovered thread id, an older epoch, and a rollback.

// M1: a side beat moves no turn's clock. A tool's deadline stays where the
// tool started it, a plain turn's silence where its last turn event put it,
// and the claim on a due silence stays; the page is not put off.
func TestSideBeatKeepsToolDeadline(t *testing.T) {
	cfg := DefaultConfig()
	st := State{}
	b := Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, TurnID: "t", Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 3600, Seq: 1}
	st.Apply(b, t0)
	b.Event, b.Reason, b.Seq = PreCompact, "auto", 2
	st.Apply(b, t0.Add(59*time.Minute))
	th := st.Threads[Key("claude", "s")]
	if got := cfg.StatusOf(th, false, t0.Add(61*time.Minute)); got != InTurn {
		t.Fatalf("inside the tool's window: %s", got)
	}
	if got := cfg.StatusOf(th, false, t0.Add(63*time.Minute)); got != Silent {
		t.Fatalf("past a 3600s tool and its grace, after a precompact at minute 59: %s", got)
	}
	if push, _ := cfg.Due(st, t0.Add(63*time.Minute)); len(push) != 1 {
		t.Fatalf("the silence is not paged on time: %v", push)
	}

	plain := State{}
	p := Beat{Thread: "p", Vendor: "claude", Host: "h1", Session: "p", Epoch: 1, TurnID: "t", Event: AfterTool, Tool: "Read", Seq: 1}
	plain.Apply(p, t0)
	th = plain.Threads[Key("claude", "p")]
	th.SilenceClaimID, th.SilenceClaimExpires = "claim", t0.Add(20*time.Minute)
	plain.Threads[Key("claude", "p")] = th
	p.Event, p.Reason, p.Seq = SessionStart, "compact", 2
	plain.Apply(p, t0.Add(9*time.Minute))
	th = plain.Threads[Key("claude", "p")]
	if !th.At.Equal(t0) || th.SilenceClaimID != "claim" || cfg.StatusOf(th, false, t0.Add(11*time.Minute)) != Silent {
		t.Fatalf("a side beat moved the turn's silence or its claim: %+v", th)
	}
}

// M2: a thread id recovered as <session>:n for the same key keeps the key's
// first sighting, its count and the client's word; a new epoch still starts
// its own. Over the handler, as a hook recovers.
func TestRecoveredKeyKeepsSessionMetadata(t *testing.T) {
	r := newRig2(t, nil)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1, "event": "session_start", "reason": "startup",
		"client_bind": map[string]any{"state": "refused", "reason": "no_context"}})
	r.clk.at = t0.Add(time.Minute)
	st := recState(t, r.rec)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 2, "event": "precompact", "reason": "manual", "thread_key": keyOf(t, st, "s")})
	r.clk.at = t0.Add(2 * time.Minute)
	r.ok(reporterP, map[string]any{"thread": "s:1", "session": "s", "epoch": 1, "seq": 1, "event": "after_tool", "tool": "Read"})
	rows, _ := r.list(viewerP, "")
	got := rows["claude/s:1"]
	if got["first_seen_at"] != t0.Format(time.RFC3339) || got["compactions"] != float64(1) || got["client_bind"].(map[string]any)["reason"] != "no_context" {
		t.Fatalf("the recovered row lost its key's state: %v", got)
	}
	r.clk.at = t0.Add(3 * time.Minute)
	r.ok(reporterP, map[string]any{"thread": "s:2", "session": "s", "epoch": 2, "seq": 1, "event": "session_start", "reason": "resume"})
	rows, _ = r.list(viewerP, "")
	if got := rows["claude/s:2"]; got["first_seen_at"] != t0.Add(3*time.Minute).Format(time.RFC3339) || got["compactions"] != nil || got["client_bind"] != nil {
		t.Fatalf("a new epoch took the old one's state: %v", got)
	}
}

// keyOf is the plaintext key a thread was issued, from the registration
// window the state keeps it for.
func keyOf(t *testing.T, st State, thread string) string {
	t.Helper()
	k := st.Threads[Key("claude", thread)].RegisterKey
	if k == "" {
		t.Fatalf("no key for %s", thread)
	}
	return k
}

// M3: a beat of an older epoch of the row's own session, side or turn, with
// any newer sequence, is ignored: the row's epoch, turn and binding stay the
// current life's. A beat naming no session keeps the row's identity.
func TestOlderEpochCannotRegress(t *testing.T) {
	st := State{}
	b := Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, TurnID: "old", Event: TurnStart, Seq: 1}
	st.Apply(b, t0)
	b.Epoch, b.TurnID, b.Seq = 2, "new", 2
	st.Apply(b, t0.Add(time.Minute))
	b.Epoch, b.TurnID, b.Seq, b.Event, b.Reason = 1, "old", 3, PreCompact, "auto"
	if st.Apply(b, t0.Add(2*time.Minute)) {
		t.Fatal("an older epoch's side beat was folded in")
	}
	b.Epoch, b.TurnID, b.Seq, b.Event, b.Reason = 1, "older-still", 4, TurnStart, ""
	if st.Apply(b, t0.Add(3*time.Minute)) {
		t.Fatal("an older epoch's turn was folded in")
	}
	b.Session, b.Epoch, b.TurnID, b.Seq, b.Event = "", 0, "new", 5, AfterTool
	st.Apply(b, t0.Add(4*time.Minute))
	got := st.Threads[Key("claude", "s")]
	if got.Epoch != 2 || got.Session != "s" || got.TurnID != "new" || got.Compactions != 0 || got.Event != AfterTool {
		t.Fatalf("the row: %+v", got)
	}
	w := indexWorks([]Work{{ID: "ew_1", Host: "h1", Vendor: "claude", Session: "s", Epoch: 1}, {ID: "ew_2", Host: "h1", Vendor: "claude", Session: "s", Epoch: 2}}, nil)
	if b, _ := w.bind(got); b.ExternalWorkID != "ew_2" || b.History != 1 {
		t.Fatalf("bound to %+v", b)
	}
	// Over the handler: the late beat is answered as ignored, not refused.
	r := newRig2(t, nil)
	r.ok(reporterP, map[string]any{"thread": "h", "session": "h", "epoch": 2, "seq": 1})
	k := keyOf(t, recState(t, r.rec), "h")
	if out := r.ok(reporterP, map[string]any{"thread": "h", "session": "h", "epoch": 1, "seq": 2, "event": "precompact", "reason": "auto", "thread_key": k}); out["ignored"] != true {
		t.Fatalf("a late epoch's beat: %v", out)
	}
	if th := recState(t, r.rec).Threads[Key("claude", "h")]; th.Epoch != 2 || th.Compactions != 0 {
		t.Fatalf("the row after a late epoch: %+v", th)
	}
}

// p1Thread is a thread as the binary before P-c2 (71441e4b) stores and reads
// it: none of P-c2's fields.
type p1Thread struct {
	previousThread
	Sidebar      string    `json:"sidebar,omitempty"`
	OpenedTurn   bool      `json:"opened_turn,omitempty"`
	OpenedTurnAt time.Time `json:"opened_turn_at,omitempty"`
	HostBound    bool      `json:"host_bound,omitempty"`
}

// M4: rolled back to the binary before P-c2, a session that only ever sent
// side beats reads as finished (turn_end, the event every binary reads as
// idle): no silence, no page, no mid-turn thread on a lost host. A row in a
// turn that side beats reached keeps the turn's clock there too, so the old
// binary pages it when its own rule says.
func TestRollbackSideOnlyStaysIdle(t *testing.T) {
	st := State{}
	for i, ev := range []Event{SessionStart, PreCompact} {
		st.Apply(Beat{Thread: "side", Vendor: "claude", Host: "h1", Session: "side", Epoch: 1, TurnID: "t", Event: ev, Reason: map[Event]string{SessionStart: "startup", PreCompact: "auto"}[ev], Seq: uint64(i + 1)}, t0.Add(time.Duration(i)*time.Minute))
	}
	st.Apply(Beat{Thread: "turn", Vendor: "claude", Host: "h1", Session: "turn", Epoch: 1, TurnID: "t", Event: AfterTool, Tool: "Read", Seq: 1}, t0)
	st.Apply(Beat{Thread: "turn", Vendor: "claude", Host: "h1", Session: "turn", Epoch: 1, TurnID: "t", Event: PreCompact, Reason: "auto", Seq: 2}, t0.Add(9*time.Minute))
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"event":"turn_end"`) || strings.Contains(string(raw), `"event":"session_start"`) || strings.Contains(string(raw), `"event":"precompact"`) {
		t.Fatalf("a side event in the field old binaries read: %s", raw)
	}
	var old struct {
		Threads map[string]p1Thread `json:"threads"`
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	back, _ := json.Marshal(map[string]any{"threads": old.Threads, "hosts": map[string]Host{"h1": {Host: "h1", At: t0.Add(10 * time.Minute)}}})
	rolled, err := decodeState(back)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	now := t0.Add(11 * time.Minute)
	side, turn := rolled.Threads[Key("claude", "side")], rolled.Threads[Key("claude", "turn")]
	if side.Event != TurnEnd || side.Mark != "" || cfg.StatusOf(side, false, now) != Idle {
		t.Fatalf("the side-only row as the old binary has it: %+v", side)
	}
	push, mark := cfg.Due(rolled, now)
	if len(push) != 1 || push[0] != Key("claude", "turn") || len(mark) != 0 {
		t.Fatalf("due: push %v mark %v; want only the silent turn", push, mark)
	}
	if !turn.At.Equal(t0) {
		t.Fatalf("the turn's clock moved: %v", turn.At)
	}
	if mid := cfg.MidTurn(rolled, "h1", now); len(mid) != 1 || mid[0].Thread != "turn" {
		t.Fatalf("mid-turn on a lost host: %+v", mid)
	}
	// And as P-c2 reads its own row: the start shown, still idle.
	views := cfg.Views(st, now)
	for _, v := range views {
		if v.Thread.Thread == "side" {
			doc, _ := json.Marshal(v)
			if v.Status != Idle || !strings.Contains(string(doc), `"event":"precompact"`) {
				t.Fatalf("P-c2's view of the side-only row: %s", doc)
			}
		}
	}
}

// P-c2 review round 2. A sync write (a nonce replay, a registration, a
// migration) folds the beats still buffered first and judges on that, the
// state Admit read.

// M3, the nonce path: a newer epoch still buffered is not replaced by a
// nonce replay of the older one; the replay is answered with its key, as
// ignored, and the newer epoch's turn stays.
func TestNonceReplayAgainstBufferedEpoch(t *testing.T) {
	r := newRig2(t, nil)
	nonce := strings.Repeat("a", 64)
	first := r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1, "register_nonce": nonce})
	r.clk.at = t0.Add(time.Second)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 2, "seq": 2, "turn_id": "new", "thread_key": first["thread_key"]})
	r.clk.at = t0.Add(2 * time.Second)
	out := r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 3, "event": "precompact", "reason": "auto", "register_nonce": nonce})
	if out["thread_key"] != first["thread_key"] || out["ignored"] != true {
		t.Fatalf("the nonce replay's answer: %v", out)
	}
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
	th := recState(t, r.rec).Threads[Key("claude", "s")]
	if th.Epoch != 2 || th.TurnID != "new" || th.Seq != 2 || th.Compactions != 0 {
		t.Fatalf("epoch %d turn %s seq %d compactions %d; want 2, new, 2, 0", th.Epoch, th.TurnID, th.Seq, th.Compactions)
	}
}

// M2, the count: a precompact buffered on a row whose key it changes, then
// a row recovered under the same key registered, then the first row going
// on: the one precompact is counted once, on both rows, and the key's
// latest row says one.
func TestRecoveredBufferedEpochCountsOnce(t *testing.T) {
	r := newRig2(t, nil)
	first := r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1})
	r.clk.at = t0.Add(time.Second)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 2, "seq": 2, "event": "precompact", "reason": "auto", "thread_key": first["thread_key"]})
	r.clk.at = t0.Add(2 * time.Second)
	r.ok(reporterP, map[string]any{"thread": "s:1", "session": "s", "epoch": 2, "seq": 1, "event": "session_start", "reason": "resume"})
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
	r.clk.at = t0.Add(3 * time.Second)
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 2, "seq": 3, "event": "after_tool", "thread_key": first["thread_key"]})
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
	st := recState(t, r.rec)
	src, rec := st.Threads[Key("claude", "s")], st.Threads[Key("claude", "s:1")]
	if src.Compactions != 1 || rec.Compactions != 1 || !src.FirstSeenAt.Equal(t0.Add(time.Second)) || !rec.FirstSeenAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("one precompact: the row %d (%v), the recovered row %d (%v)", src.Compactions, src.FirstSeenAt, rec.Compactions, rec.FirstSeenAt)
	}
	rows, _ := r.list(viewerP, "")
	if rows["claude/s"]["compactions"] != float64(1) {
		t.Fatalf("the key's latest row: %v", rows["claude/s"])
	}
}

// A sync write that is refused writes nothing, the buffer included: with
// every slot a live thread, a new registration is refused (429), and the
// beat buffered before it is written by the next flush, once.
func TestRefusedSyncWriteKeepsTheBuffer(t *testing.T) {
	r := newRig2(t, nil)
	first := r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 1})
	if err := r.rec.store.Update(func(st *State) error {
		for i := 0; len(st.Threads) < maxThreads; i++ {
			id := fmt.Sprintf("live-%d", i)
			st.Threads[Key("claude", id)] = Thread{Thread: id, Vendor: "claude", Host: "h1", Event: BeforeTool, TurnID: "t", Seq: 1, At: t0, TurnStartedAt: t0}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.ok(reporterP, map[string]any{"thread": "s", "session": "s", "epoch": 1, "seq": 2, "event": "precompact", "reason": "auto", "thread_key": first["thread_key"]})
	if w := r.beat(reporterP, map[string]any{"thread": "one-too-many", "seq": 1}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("a registration past the cap: %d %s", w.Code, w.Body)
	}
	if n := len(r.rec.pending); n != 1 {
		t.Fatalf("the refused write emptied the buffer: %d left", n)
	}
	if th := recState(t, r.rec).Threads[Key("claude", "s")]; th.Compactions != 1 {
		t.Fatalf("before the flush, the snapshot: %+v", th)
	}
	if err := r.rec.Flush(); err != nil {
		t.Fatal(err)
	}
	if th := recState(t, r.rec).Threads[Key("claude", "s")]; th.Compactions != 1 || th.Seq != 2 {
		t.Fatalf("after the flush: %+v", th)
	}
}

// Round-1 landing review (P-c2): a recovered row takes the key's newest
// metadata, ordered by when the metadata changed, not by the turn's silence
// clock (side beats leave At alone in a turn). s counts one compaction in a
// turn; s:1 recovers it; s counts a second and reports a refused binding,
// still in its turn; s:2 must recover two compactions and the refusal, not
// s:1's older copy.
func TestRecoveredKeyTakesNewestMetadata(t *testing.T) {
	st := State{}
	b := Beat{Thread: "s", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1}
	b.Event, b.Seq = SessionStart, 1
	st.Apply(b, t0)
	b.Event, b.Seq, b.TurnID = TurnStart, 2, "t1"
	st.Apply(b, t0.Add(10*time.Second))
	b.Event, b.Seq, b.Reason = PreCompact, 3, "auto"
	st.Apply(b, t0.Add(time.Minute))
	r1 := Beat{Thread: "s:1", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: AfterTool, Seq: 1, TurnID: "u1"}
	st.Apply(r1, t0.Add(2*time.Minute))
	if got := st.Threads[Key("claude", "s:1")]; got.Compactions != 1 {
		t.Fatalf("s:1 did not recover the first compaction: %+v", got)
	}
	b.Event, b.Seq, b.ClientBind = PreCompact, 4, &ClientBind{State: BindRefused, Reason: "no_context"}
	st.Apply(b, t0.Add(3*time.Minute))
	if got := st.Threads[Key("claude", "s")]; got.Compactions != 2 || got.At != t0.Add(10*time.Second) {
		t.Fatalf("s: %+v", got)
	}
	r2 := Beat{Thread: "s:2", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: AfterTool, Seq: 1, TurnID: "u2"}
	st.Apply(r2, t0.Add(4*time.Minute))
	got := st.Threads[Key("claude", "s:2")]
	if got.Compactions != 2 || got.ClientBind == nil || got.ClientBind.State != BindRefused || !got.FirstSeenAt.Equal(t0) {
		t.Fatalf("s:2 recovered stale metadata: compactions=%d client_bind=%v first_seen=%v", got.Compactions, got.ClientBind, got.FirstSeenAt)
	}
	// A later beat that changes no metadata on s:1 does not make its older
	// copy the newest.
	r1.Event, r1.Seq = AfterTool, 2
	st.Apply(r1, t0.Add(5*time.Minute))
	r3 := Beat{Thread: "s:3", Vendor: "claude", Host: "h1", Session: "s", Epoch: 1, Event: AfterTool, Seq: 1, TurnID: "u3"}
	st.Apply(r3, t0.Add(6*time.Minute))
	if got := st.Threads[Key("claude", "s:3")]; got.Compactions != 2 || got.ClientBind == nil {
		t.Fatalf("s:3 recovered stale metadata after a plain beat on s:1: %+v", got)
	}
}
