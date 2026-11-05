// Package agentthreads notices an agent session that stopped in the middle of
// a turn (host asleep, process hung, network gone) and pages the Owner.
//
// A host is shown as lost about 3 to 4 minutes after its last heartbeat: the
// threshold itself is 3 minutes, and the API may still take one flush interval
// (10 seconds) plus one watch interval (30 seconds) to notice. That is not a
// hard "within 3 minutes". Session silence is max(10 minutes, declared tool
// timeout + 2 minutes), measured on the server clock.
//
// What is stored for a thread is the sha256 of its key, plus the plaintext
// key during the registration window. The first event is issued a random
// key. The client also sends a registration nonce it generated. For
// registerWindow (2 minutes) that nonce is a recovery credential: a holder
// of the shared reporter token who also has the nonce can replay it and
// receive the same key. The nonce is not bound to a reporter principal.
// Once the window has passed, the plaintext key is removed. The periodic
// flush does that even when no event is buffered, and it writes only when a
// key has actually expired. A later event must present the key. A record
// that has no hash (it was created before keys existed) is migrated by
// issuing a key on its next event. A thread the server has already pruned
// accepts a stale key only as a tombstone: it re-registers with a new key
// and never accepts the stale one again. A live thread whose key the caller
// does not hold answers 409 "unknown key" and is left in place. The client
// then opens a new thread record and keeps the old key as superseded; it
// does not reuse that key, and this server does not let the reporter token
// delete or replace the old record.
//
// Each event carries a per-thread turn id and a sequence that only grows. A
// sequence that is not newer is ignored. An event from an older turn is
// refused even when its sequence is newer: a new turn is accepted only as
// turn_start, and a turn id that has already been left is refused. That
// refusal stays strict for a finished turn; a later event does not reopen
// it. Silence and host loss are timed from the server clock. A
// waiting_owner thread stays in progress and is not paged. A lost host is
// paged, and its mid-turn threads are shown as host lost; the heartbeat
// that marks the host back is not paged. Sending is claimed with an id and
// an expiry, and marked sent only after the push returns. A claim whose
// worker died is retried once it expires. The push may arrive twice: the
// relay is not given a stable dedup id, so a crash after the push returns
// and before the sent mark is written sends again when the claim expires.
//
// When every thread slot is a live thread, the new registration is refused
// and not stored. The statefile keeps a counter of those refusals and the
// time of the last one, and the thread list returns both.
//
// Trust boundary: there is one shared reporter token, not a credential per
// host. Every holder of that token can report a heartbeat for any host and
// can register any new thread. They can mark a host alive, or a vendor wired
// or token-ready, and so hide a lost host or an unwired vendor. They can also
// open a new thread id when they do not hold an existing thread's key. They
// cannot update a thread whose key they do not hold, and they cannot reuse a
// key this server has retired. Binding a reporter to a host is a separate lane.
//
// Detection and notice only: nothing here restarts, reassigns or touches a
// session. State is one statefile key. The API pod buffers events and folds
// them in every few seconds; the workers pod reads that key and writes back
// only which silence or host loss it has claimed or already pushed.
package agentthreads

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is what a session reports.
type Event string

const (
	TurnStart    Event = "turn_start"
	BeforeTool   Event = "before_tool"
	AfterTool    Event = "after_tool"
	TurnEnd      Event = "turn_end"
	WaitingOwner Event = "waiting_owner"
	// SessionStart and PreCompact (R5:5.5-12 §4.1) are a session's side
	// beats: one began (reason the source), one is about to compact (reason
	// the trigger). Neither opens nor ends a turn, nor changes a tool's
	// window or a wait; each is heard from and shown as the last event.
	SessionStart Event = "session_start"
	PreCompact   Event = "precompact"
)

// side says whether e is a side beat: it belongs to no turn.
func (e Event) side() bool { return e == SessionStart || e == PreCompact }

// idle says whether a stored thread is in no turn: it ended one, or it has
// sent side beats only (a session started and never prompted).
func (t Thread) idle() bool { return t.Event == TurnEnd || t.Event.side() }

// sideReasons are the reasons a side beat may carry (§4.1; design §12 D2
// adds fork, a forked session, which the hook treats as a startup).
var sideReasons = map[Event]map[string]struct{}{
	SessionStart: {"startup": {}, "resume": {}, "clear": {}, "compact": {}, "fork": {}},
	PreCompact:   {"manual": {}, "auto": {}},
}

// ClientBind is what a client says of its session's binding (§3.1): a
// diagnosis it reports, never a permission and never the server's
// judgement. Its reason is from a closed set.
type ClientBind struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	At     string `json:"at,omitempty"`
}

var (
	clientBindStates  = map[string]struct{}{"unlinked": {}, "refused": {}, "linked": {}}
	clientBindReasons = map[string]struct{}{"no_context": {}, "no_description": {}, "node_rejected": {}, "not_dev_tree": {},
		"register_failed": {}, "token_missing": {}, "identity_fallback": {}, "no_vendor_session": {}}
	hostIDSources = map[string]struct{}{"file": {}, "env": {}, "hostname": {}}
)

// maxEpoch bounds a beat's epoch, a client's count of its session's lives.
const maxEpoch = 1 << 30

// Status is how a thread looks from here.
type Status string

const (
	// Idle: the last event was turn end.
	Idle Status = "idle"
	// Superseded is a quiet session replaced by another sidebar session turn.
	Superseded Status = "superseded"
	// InTurn: mid-turn and heard from recently enough.
	InTurn Status = "in_turn"
	// Silent: mid-turn and quiet for longer than its threshold.
	Silent Status = "silent"
	// StatusWaiting: the session is waiting for the person. Not silent, not paged.
	StatusWaiting Status = "waiting_owner"
	// HostLost: the host's last heartbeat is older than the host threshold.
	HostLost Status = "host_lost"
)

const (
	maxToolTimeout = 24 * 60 * 60
	maxThreads     = 300
	maxHosts       = 50
	// registerWindow is how long a lost first response can be retried with
	// the same nonce and receive the same key. The plaintext key is kept
	// only for this long. Two minutes is the stricter bound: long enough
	// for the next hook event, short enough that the key does not sit in
	// the statefile.
	registerWindow = 2 * time.Minute
	maxPriorTurns  = 32
	// HostAlive, HostLostState and HostNeverReported are the host list's status strings.
	HostAlive         = "alive"
	HostLostState     = "lost"
	HostNeverReported = "never_reported"
)

// ErrUnknownKey means this caller does not hold the live key. The thread is
// unchanged. The client may open a different thread record; it must not reuse
// the key it presented.
var ErrUnknownKey = fmt.Errorf("unknown key")

// ErrOlderTurn means the event belongs to a turn this thread has already left,
// or to a turn that was never started, even if the sequence is newer.
var ErrOlderTurn = fmt.Errorf("older turn refused")

// ErrThreadFull means every thread slot is a live (not finished) thread.
// The new registration was not stored.
var ErrThreadFull = fmt.Errorf("thread capacity full")

// ErrHostFull means every host slot is taken. Lost hosts and hosts that are
// still alive are both kept. The new host was not stored.
var ErrHostFull = fmt.Errorf("host capacity full")

// Vendors is the set a host heartbeat must describe.
var Vendors = []string{"claude", "cursor", "codex"}

// waitingReasons are the only notification types that mean the person has to act.
var waitingReasons = map[string]struct{}{
	"permission_prompt": {},
	"idle_prompt":       {},
	"elicitation":       {},
}

// Beat is one reported event (POST body).
type Beat struct {
	Thread       string `json:"thread"`
	Vendor       string `json:"vendor"`
	Host         string `json:"host"`
	Work         string `json:"work,omitempty"`
	Title        string `json:"title,omitempty"`
	Event        Event  `json:"event"`
	Tool         string `json:"tool,omitempty"`
	ToolTimeoutS int    `json:"tool_timeout_s,omitempty"`
	// ThreadKey is presented by the holder. It is never stored and never listed.
	ThreadKey string `json:"thread_key,omitempty"`
	// RegisterNonce is the client's registration id. The same nonce returns
	// the same key for registerWindow. It is never listed.
	RegisterNonce string `json:"register_nonce,omitempty"`
	TurnID        string `json:"turn_id"`
	Seq           uint64 `json:"seq"`
	Reason        string `json:"reason,omitempty"`
	// Sidebar is the desktop thread id, when the client sends one. Empty for
	// an old client. It is not listed.
	Sidebar        string `json:"sidebar,omitempty"`
	sidebarPresent bool
	// Session is the vendor's raw session id and Epoch the client's count of
	// that session's lives (R5:5.5-12 §3.1): with the host and vendor, the
	// key a session is bound by. An old client sends neither; its key is its
	// thread id and epoch 0.
	Session string `json:"session,omitempty"`
	Epoch   int    `json:"epoch,omitempty"`
	// ClientBind and HostIDSource are the client's diagnoses (§3.1, §4.3).
	ClientBind   *ClientBind `json:"client_bind,omitempty"`
	HostIDSource string      `json:"host_id_source,omitempty"`
	// tokenKind is the kind of token that sent the beat, set by the server
	// (collector, or the principal's role).
	tokenKind string

	// titlePresent: the body names a title, in any letter case
	// encoding/json would read as one, whatever its value. A collector's
	// beat names none (R5:5.5-12 §1.3).
	titlePresent bool
	// keyHash is the server-side sha256 of ThreadKey. The client cannot set it.
	keyHash string
	// boundHost is the host the token that sent the beat is bound to (a
	// collector, R5:5.5-12 §1.4); "" for a token bound to none. The server
	// sets it, never the client: such a beat writes only a thread of that
	// host, new or already there (Apply, Recorder.Admit).
	boundHost string
}

// UnmarshalJSON distinguishes an omitted sidebar from an explicit empty/null value.
func (b *Beat) UnmarshalJSON(raw []byte) error {
	type plain Beat
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*b = Beat(decoded)
	_, b.sidebarPresent = fields["sidebar"]
	for name := range fields {
		if strings.EqualFold(name, "title") {
			b.titlePresent = true
		}
	}
	return nil
}

// Thread is the stored view of one session.
type Thread struct {
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
	// Sidebar is the desktop thread this session belongs to. Empty when the
	// client did not send one. It stays in the statefile and is omitted from
	// the list. Session identity is preserved.
	Sidebar string `json:"sidebar,omitempty"`
	// OpenedTurn is set once this session has sent turn_start. It is not listed.
	OpenedTurn bool `json:"opened_turn,omitempty"`
	// OpenedTurnAt is the last actual turn_start, independent of later events.
	OpenedTurnAt time.Time `json:"opened_turn_at,omitempty"`
	// Mark and MarkReason are the last side beat since the last turn event
	// (§4.1): listed as the event and its reason, never read for the turn.
	Mark       Event  `json:"mark,omitempty"`
	MarkReason string `json:"mark_reason,omitempty"`
	// Session and Epoch are the session's key with Host and Vendor (§3.1):
	// the vendor's session id, the thread id for an old client.
	Session string `json:"session,omitempty"`
	Epoch   int    `json:"epoch,omitempty"`
	// FirstSeenAt is when the server first heard this key, each epoch its
	// own; Compactions the precompact beats it counted for the key, by
	// sequence, so a resend is not counted again. Design §12 D3: PreCompact
	// fires for a refused compaction too, so these are attempts.
	FirstSeenAt time.Time `json:"first_seen_at,omitempty"`
	Compactions int       `json:"compactions,omitempty"`
	// ClientBind and HostIDSource are the client's last diagnoses, kept
	// with the key and cleared with it; TokenKind the kind of token that
	// sent the last beat.
	ClientBind   *ClientBind `json:"client_bind,omitempty"`
	HostIDSource string      `json:"host_id_source,omitempty"`
	TokenKind    string      `json:"token_kind,omitempty"`
	// MetaAt is when this row's copy of the key's state (FirstSeenAt,
	// Compactions, ClientBind) last changed: its first sighting, a copy
	// taken from another row, a counted precompact, a client diagnosis. A
	// recovered row takes the copy with the newest MetaAt. At cannot order
	// them: a side beat in a turn leaves At alone (review M1).
	MetaAt time.Time `json:"meta_at,omitempty"`
	// HeardAt is when the row's last beat was stored, a side beat included.
	// At is the turn's clock, which a side beat in a turn or a wait leaves
	// alone; retention reads the later of the two (lastHeard).
	HeardAt time.Time `json:"heard_at,omitempty"`
	// HostBound is set once a host-bound token (a collector) has written
	// this session. Such a session supersedes sessions of its own host only
	// (sessionSuperseded): a shared sidebar id does not reach another
	// host's threads. It is not listed.
	HostBound bool `json:"host_bound,omitempty"`
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

// VendorReport is one vendor on a host heartbeat.
type VendorReport struct {
	Wired bool `json:"wired"`
	Token bool `json:"token"`
}

// Monitored is true only when the hook wiring is present and the reporter token is readable.
func (v VendorReport) Monitored() bool { return v.Wired && v.Token }

// HostBeat is POST /api/v1/agent/hosts/heartbeat.
type HostBeat struct {
	Host    string                  `json:"host"`
	Vendors map[string]VendorReport `json:"vendors"`
}

// Host is the stored view of one machine.
type Host struct {
	Host    string                  `json:"host"`
	At      time.Time               `json:"at"`
	Vendors map[string]VendorReport `json:"vendors"`
	// LostNotifiedFor is At of the heartbeat whose later silence was pushed.
	// It is set only after the push returns. A newer heartbeat does not push;
	// the next time that newer one ages out does.
	LostNotifiedFor time.Time `json:"lost_notified_for,omitempty"`
	LostNotifiedAt  time.Time `json:"lost_notified_at,omitempty"`
	// LossClaimID is a host-loss send that has been claimed and not yet marked
	// sent. LossClaimExpires is when a dead worker's claim may be taken again.
	LossClaimID      string    `json:"loss_claim_id,omitempty"`
	LossClaimExpires time.Time `json:"loss_claim_expires,omitempty"`
}

// State is the statefile document.
type State struct {
	Threads map[string]Thread `json:"threads"`
	Hosts   map[string]Host   `json:"hosts,omitempty"`
	// ThreadsRefused counts new thread registrations that were refused
	// because the live cap was full. LastThreadRefusal is when the last one
	// happened. Neither is a thread row, so a refusal does not consume a slot.
	// The list returns both. Needs You counts the condition while the last
	// refusal is younger than one hour.
	ThreadsRefused    int       `json:"threads_refused,omitempty"`
	LastThreadRefusal time.Time `json:"last_thread_refusal,omitempty"`
}

// Key is the map key of a thread: vendor and thread id.
func Key(vendor, thread string) string { return vendor + "/" + thread }

// ErrOtherHost: a host-bound beat names a thread another host holds. The
// thread is not changed, whatever key or nonce the beat presents.
var ErrOtherHost = errors.New("the thread is another host's")

// foreignThread says whether a host-bound beat would write a thread not of
// its host: one it names for another host, or one stored for another host
// under the same vendor and id (the key is global).
func foreignThread(t Thread, seen bool, b Beat) bool {
	return b.boundHost != "" && (b.Host != b.boundHost || seen && t.Host != b.boundHost)
}

// Apply folds one beat received at `at` into st. A sequence that is not strictly
// newer than the one already stored is ignored. An event from an older turn is
// not stored even when its sequence is newer. The receive time is not an
// order key; it is what silence is measured from. Apply reports whether the
// beat was stored.
func (st *State) Apply(b Beat, at time.Time) bool {
	if st.Threads == nil {
		st.Threads = map[string]Thread{}
	}
	k := Key(b.Vendor, b.Thread)
	t, seen := st.Threads[k]
	if foreignThread(t, seen, b) {
		return false
	}
	if seen && olderTurn(t, b) {
		return false
	}
	if seen && b.Seq <= t.Seq {
		return false
	}
	if b.keyHash != "" && t.KeyHash != "" && b.keyHash != t.KeyHash {
		return false
	}
	// The session's key (§3.1): a new one starts its own first sighting,
	// count and client diagnosis.
	// An older epoch of the row's own session never replaces the current
	// one, whatever kind of beat it is (review M3).
	if seen && olderEpoch(t, b) {
		return false
	}
	// The session's key (§3.1). A beat naming no session (an old client's)
	// keeps the row's; a key new to the row takes what the platform already
	// holds for it on another row (a thread id recovered as <session>:n,
	// review M2), and only a key heard nowhere starts its own first sighting,
	// count and client diagnosis.
	session, epoch := beatKey(t, seen, b)
	if !seen || t.Session != session || t.Epoch != epoch || t.Host != b.Host {
		// The key is the beat's, host included: a side beat stores no host of
		// its own, so the row takes it here, before the key's state is read
		// or shared (P-c2 pre-review M3).
		t.Host = b.Host
		t.Session, t.Epoch, t.FirstSeenAt, t.Compactions, t.ClientBind, t.MetaAt = session, epoch, at, 0, nil, at
		// What the state holds of the key: every beat admitted before this
		// one is in it, and none after (a sync write folds the buffer first).
		if held := st.heldKey(k, b.Host, b.Vendor, session, epoch); held != nil {
			t.FirstSeenAt, t.Compactions, t.ClientBind, t.MetaAt = held.FirstSeenAt, held.Compactions, held.ClientBind, held.MetaAt
		}
	}
	shared := false
	if b.ClientBind != nil {
		cb := *b.ClientBind
		t.ClientBind = &cb
		t.MetaAt = at
		shared = true
	}
	if b.HostIDSource != "" {
		t.HostIDSource = b.HostIDSource
	}
	if b.tokenKind != "" {
		t.TokenKind = b.tokenKind
	}
	if b.Event.side() {
		return st.applySide(k, t, seen, b, at, shared)
	}
	t.Mark, t.MarkReason = "", ""
	// A headless wrapper's progress beat is not the end of a tool call whose
	// declared timeout is still running. Keep the declared window.
	if seen && b.Event == AfterTool && b.Tool == "headless-progress" && b.TurnID == t.TurnID &&
		t.Event == BeforeTool && t.ToolTimeoutS > 0 && at.Sub(t.At) < time.Duration(t.ToolTimeoutS)*time.Second {
		t.Seq = b.Seq
		t.HeardAt = at
		st.Threads[k] = t
		if shared {
			st.shareKeyMeta(k, t)
		}
		return true
	}
	if seen && t.TurnID != "" && b.TurnID != t.TurnID {
		t.PriorTurns = rememberTurn(t.PriorTurns, t.TurnID)
	}
	if !seen || t.Event == TurnEnd || b.Event == TurnStart {
		t.TurnStartedAt = at
	}
	t.Thread, t.Vendor, t.Host = b.Thread, b.Vendor, b.Host
	if b.Work != "" {
		t.Work = b.Work
	}
	if b.Title != "" {
		t.Title = b.Title
	}
	t.Event, t.Tool, t.ToolTimeoutS = b.Event, b.Tool, b.ToolTimeoutS
	if b.Event == WaitingOwner {
		t.Reason = b.Reason
	} else {
		t.Reason = ""
	}
	if b.keyHash != "" {
		t.KeyHash = b.keyHash
	}
	if b.boundHost != "" {
		t.HostBound = true
	}
	t.TurnID, t.Seq = b.TurnID, b.Seq
	if b.Sidebar != "" {
		t.Sidebar = b.Sidebar
	}
	if b.Event == TurnStart {
		t.OpenedTurn = true
		t.OpenedTurnAt = at
	}
	t.At = at
	t.HeardAt = at
	// A new event is a different silence. A claim for the previous one must
	// not block it, and must not be completed against the new timestamp.
	t.SilenceClaimID = ""
	t.SilenceClaimExpires = time.Time{}
	st.Threads[k] = t
	if shared {
		st.shareKeyMeta(k, t)
	}
	return true
}

// olderTurn reports that b is not the current turn and is not a turn_start
// that opens a turn this thread has not left. A side beat belongs to no
// turn, so it is never an older turn's.
func olderTurn(t Thread, b Beat) bool {
	if t.TurnID == "" || b.TurnID == t.TurnID || b.Event.side() {
		return false
	}
	for _, id := range t.PriorTurns {
		if id == b.TurnID {
			return true
		}
	}
	return b.Event != TurnStart
}

func rememberTurn(prior []string, id string) []string {
	if id == "" {
		return prior
	}
	for _, p := range prior {
		if p == id {
			return prior
		}
	}
	prior = append(prior, id)
	if len(prior) > maxPriorTurns {
		prior = append([]string(nil), prior[len(prior)-maxPriorTurns:]...)
	}
	return prior
}

// ApplyHost folds one host heartbeat. An older receive time does not move the
// host backwards. The loss marker is kept, so a return is not a new loss.
func (st *State) ApplyHost(b HostBeat, at time.Time) {
	if st.Hosts == nil {
		st.Hosts = map[string]Host{}
	}
	h := st.Hosts[b.Host]
	if !h.At.IsZero() && at.Before(h.At) {
		return
	}
	h.Host = b.Host
	h.At = at
	h.Vendors = cloneVendors(fillVendors(b.Vendors))
	// A new heartbeat retires a claim for the previous loss. The sent mark
	// stays, so the return itself is not a new loss.
	h.LossClaimID = ""
	h.LossClaimExpires = time.Time{}
	st.Hosts[b.Host] = h
}

// Prune drops threads last heard before cutoff. That retention bound includes
// a thread still mid-turn; a client that still holds the key re-registers
// when it next reports (see Admit). It then drops finished threads until the
// thread cap fits. A mid-turn thread is never removed to make room. A lost
// host is never removed, by retention or by the cap. An alive host is a live
// entry and is not removed either; a new host that does not fit is refused
// by fitNewHost.
func (st *State) Prune(cutoff time.Time, cfg Config, now time.Time) {
	for k, t := range st.Threads {
		if lastHeard(t).Before(cutoff) {
			delete(st.Threads, k)
		}
	}
	st.evictFinishedThreads("")
	for k, h := range st.Hosts {
		if h.At.Before(cutoff) && !cfg.hostIsLost(h, now) {
			delete(st.Hosts, k)
		}
	}
}

// evictFinishedThreads removes the oldest finished threads until the cap
// fits. It never removes a thread that has not ended.
func (st *State) evictFinishedThreads(keep string) {
	for len(st.Threads) > maxThreads {
		victim := ""
		var oldest time.Time
		for k, t := range st.Threads {
			if !t.idle() || k == keep {
				continue
			}
			if victim == "" || t.At.Before(oldest) {
				victim, oldest = k, t.At
			}
		}
		if victim == "" {
			return
		}
		delete(st.Threads, victim)
	}
}

// fitNewThread evicts finished threads to make room for newKey. The new
// thread stays. When every other slot is mid-turn, it returns ErrThreadFull
// and leaves the map unchanged apart from any finished threads it removed.
func (st *State) fitNewThread(newKey string) error {
	// The new thread is never its own victim: a row opened by a side beat is
	// stored finished, and evicting it would confirm a registration whose
	// key, nonce and sequence are not kept, so a retry counts again (P-c2
	// pre-review M2). It is refused instead and the update rolls back.
	st.evictFinishedThreads(newKey)
	if len(st.Threads) <= maxThreads {
		return nil
	}
	if _, ok := st.Threads[newKey]; ok && len(st.Threads) > maxThreads {
		return ErrThreadFull
	}
	return nil
}

// fitNewHost reports whether name can be added. An existing host always
// fits. A new host is refused when the cap is already full: lost hosts and
// alive hosts are both kept.
func (st *State) fitNewHost(name string) error {
	if _, ok := st.Hosts[name]; ok {
		return nil
	}
	if len(st.Hosts) >= maxHosts {
		return ErrHostFull
	}
	return nil
}

// Name is how a thread is called in a push: its title, else vendor and a short id.
func (t Thread) Name() string {
	if t.Title != "" {
		return t.Title
	}
	id := t.Thread
	if len(id) > 8 {
		id = id[:8]
	}
	return t.Vendor + " " + id
}

// Message is the push for a silent thread. The title asks the Owner to look.
func (c Config) Message(t Thread, now time.Time) (title, body string) {
	quiet := now.Sub(t.At)
	title = fmt.Sprintf("Look · %s stopped %s ago", t.Name(), human(quiet))
	last := string(t.Event)
	if t.Tool != "" {
		last += " " + t.Tool
	}
	if t.Event == BeforeTool && t.ToolTimeoutS > 0 {
		last += fmt.Sprintf(" (timeout %ds)", t.ToolTimeoutS)
	}
	parts := []string{t.Vendor + " on " + t.Host}
	if t.Work != "" {
		parts = append(parts, t.Work)
	}
	parts = append(parts,
		"silent "+human(quiet),
		"last event "+last+" at "+t.At.UTC().Format("15:04")+" UTC",
		"in turn "+human(now.Sub(t.TurnStartedAt)),
	)
	body = "Nothing is working on it. Open the thread.\n" + strings.Join(parts, " · ") + ". Nothing was restarted."
	return title, body
}

// HostMessage is the one push for a host that stopped heartbeating.
// Wrapped runs stay on the coordinator's list, outside this Owner summary.
func (c Config) HostMessage(h Host, mid []Thread, now time.Time) (title, body string) {
	age := now.Sub(h.At)
	title = fmt.Sprintf("Look · %s has not reported for %s", h.Host, human(age))
	names := make([]string, 0, len(mid))
	for _, t := range mid {
		if strings.HasPrefix(t.Thread, "run-") {
			continue
		}
		names = append(names, t.Name())
	}
	sort.Strings(names)
	parts := []string{h.Host, "last heartbeat " + human(age) + " ago"}
	if len(names) == 0 {
		parts = append(parts, "no thread mid-turn")
	} else {
		parts = append(parts, "mid-turn "+strings.Join(names, ", "))
	}
	var dark []string
	for _, name := range Vendors {
		r := h.Vendors[name]
		if !r.Monitored() {
			dark = append(dark, name)
		}
	}
	if len(dark) > 0 {
		parts = append(parts, "not monitored "+strings.Join(dark, ", "))
	}
	body = strings.Join(parts, " · ") + ". Nothing was restarted."
	return title, body
}

func fillVendors(in map[string]VendorReport) map[string]VendorReport {
	out := make(map[string]VendorReport, len(Vendors))
	for _, name := range Vendors {
		if in != nil {
			out[name] = in[name]
		}
	}
	return out
}

func cloneVendors(in map[string]VendorReport) map[string]VendorReport {
	out := make(map[string]VendorReport, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// NewThreadKey returns a random key and the hex sha256 stored in its place.
func NewThreadKey() (plain, hash string, err error) {
	var buf [32]byte
	if _, err = rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(buf[:])
	return plain, HashKey(plain), nil
}

// HashKey is the hex sha256 of a thread key.
func HashKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// KeyMatches reports whether presented is the key whose hash was stored.
func KeyMatches(storedHash, presented string) bool {
	if storedHash == "" || !keyRe.MatchString(presented) {
		return false
	}
	want, err := hex.DecodeString(storedHash)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	sum := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(want, sum[:]) == 1
}

func human(d time.Duration) string {
	m := int64(d / time.Minute)
	switch {
	case m < 1:
		return "<1m"
	case m < 120:
		return fmt.Sprintf("%dm", m)
	case m < 48*60:
		return fmt.Sprintf("%dh%02dm", m/60, m%60)
	default:
		return fmt.Sprintf("%dd", m/(24*60))
	}
}
