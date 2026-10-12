package agentthreads

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// Frozen from 2441e87, the version before optional sidebar metadata.
type previousThread struct {
	Thread        string    `json:"thread"`
	Vendor        string    `json:"vendor"`
	Host          string    `json:"host"`
	Work          string    `json:"work,omitempty"`
	Title         string    `json:"title,omitempty"`
	Event         Event     `json:"event"`
	Tool          string    `json:"tool,omitempty"`
	ToolTimeoutS  int       `json:"tool_timeout_s,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	At            time.Time `json:"at"`
	TurnStartedAt time.Time `json:"turn_started_at"`
	// KeyHash is the hex sha256 of the thread key. It stays in the statefile
	// and is omitted from the list.
	KeyHash string `json:"key_hash,omitempty"`
	TurnID  string `json:"turn_id,omitempty"`
	Seq     uint64 `json:"seq,omitempty"`
	// RegisterNonce and RegisterKey let a lost first response retry. The
	// plaintext key is wiped after registerWindow and is never listed.
	RegisterNonce string    `json:"register_nonce,omitempty"`
	RegisterKey   string    `json:"register_key,omitempty"`
	RegisteredAt  time.Time `json:"registered_at,omitempty"`
	// SupersededKeys are hashes that must never be accepted again, including
	// a stale key presented after the server had pruned the thread.
	SupersededKeys []string `json:"superseded_keys,omitempty"`
	// PriorTurns are turn ids this thread has left. An event for one of them
	// is refused even when its sequence is newer.
	PriorTurns []string `json:"prior_turns,omitempty"`
	// NotifiedFor is At of the event whose silence was pushed. It is set
	// only after the push returns. A new event moves At, so the next silence
	// is a new one.
	NotifiedFor time.Time `json:"notified_for,omitempty"`
	NotifiedAt  time.Time `json:"notified_at,omitempty"`
	// SilenceClaimID is a send that has been claimed and not yet marked sent.
	// SilenceClaimExpires is when a dead worker's claim may be taken again.
	SilenceClaimID      string    `json:"silence_claim_id,omitempty"`
	SilenceClaimExpires time.Time `json:"silence_claim_expires,omitempty"`
}

// Planted violation: make sidebar required on disk or rename/drop an old field.
func TestSidebarStateUpgradeRollback(t *testing.T) {
	old := previousThread{Thread: "legacy", Vendor: "claude", Host: "mbp", Work: "W-54", Title: "Title", Event: BeforeTool, Tool: "Bash", ToolTimeoutS: 120, Reason: "reason", At: t0, TurnStartedAt: t0, KeyHash: "hash", TurnID: "turn", Seq: 7, RegisterNonce: "nonce", RegisterKey: "key", RegisteredAt: t0, SupersededKeys: []string{"old-hash"}, PriorTurns: []string{"old-turn"}, NotifiedFor: t0, NotifiedAt: t0, SilenceClaimID: "claim", SilenceClaimExpires: t0.Add(time.Minute)}
	raw, err := json.Marshal(map[string]any{"threads": map[string]previousThread{Key("claude", "legacy"): old}, "hosts": map[string]Host{"mbp": {Host: "mbp", At: t0}}, "threads_refused": 3, "last_thread_refusal": t0})
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := decodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	th := upgraded.Threads[Key("claude", "legacy")]
	if th.Sidebar != "" || th.OpenedTurn || !th.OpenedTurnAt.IsZero() {
		t.Fatalf("old record gained metadata: %+v", th)
	}
	th.Sidebar = sidebarID
	th.OpenedTurn = true
	th.OpenedTurnAt = t0
	upgraded.Threads[Key("claude", "legacy")] = th
	raw, err = json.Marshal(upgraded)
	if err != nil {
		t.Fatal(err)
	}
	var previous struct {
		Threads           map[string]previousThread `json:"threads"`
		Hosts             map[string]Host           `json:"hosts"`
		ThreadsRefused    int                       `json:"threads_refused"`
		LastThreadRefusal time.Time                 `json:"last_thread_refusal"`
	}
	if err = json.Unmarshal(raw, &previous); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous.Threads[Key("claude", "legacy")], old) {
		t.Fatalf("old fields changed: %+v", previous)
	}
	raw, err = json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	th = upgraded.Threads[Key("claude", "legacy")]
	th.Sidebar = ""
	th.OpenedTurn = false
	th.OpenedTurnAt = time.Time{}
	upgraded.Threads[Key("claude", "legacy")] = th
	if !reflect.DeepEqual(again, upgraded) {
		t.Fatalf("rollback lost more than metadata: got %+v want %+v", again, upgraded)
	}
}

// 8a87ccd already knew sidebar/opened_turn, but not the actual opening time.
func TestSidebarStateRoundOneRollback(t *testing.T) {
	type roundOneThread struct {
		previousThread
		Sidebar    string `json:"sidebar,omitempty"`
		OpenedTurn bool   `json:"opened_turn,omitempty"`
	}
	st := State{}
	b := Beat{Thread: "session", Vendor: "claude", Host: "mbp", Event: TurnStart, TurnID: "turn", Seq: 1, Sidebar: sidebarID}
	st.Apply(b, t0)
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var prior struct {
		Threads map[string]roundOneThread `json:"threads"`
	}
	if err = json.Unmarshal(raw, &prior); err != nil {
		t.Fatal(err)
	}
	old := prior.Threads[Key("claude", "session")]
	if old.Sidebar != sidebarID || !old.OpenedTurn || old.Seq != 1 {
		t.Fatalf("round-one metadata lost: %+v", old)
	}
	raw, err = json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := decodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := upgraded.Threads[Key("claude", "session")]
	want := st.Threads[Key("claude", "session")]
	want.OpenedTurnAt = time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-one rollback lost old data: %+v", got)
	}
}
