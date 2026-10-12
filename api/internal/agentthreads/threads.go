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
	"fmt"
	"regexp"
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
)

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
	// keyHash is the server-side sha256 of ThreadKey. The client cannot set it.
	keyHash string
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

var (
	idRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	vendRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hostRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	workRe  = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[0-9A-Za-z-]{1,24}$`)
	toolRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,96}$`)
	turnRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	keyRe   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	maxText = 160
)

// Validate checks a beat before it is stored.
func (b *Beat) Validate() error {
	b.Thread = strings.TrimSpace(b.Thread)
	b.Vendor = strings.TrimSpace(b.Vendor)
	b.Host = strings.TrimSpace(b.Host)
	b.Work = strings.TrimSpace(b.Work)
	b.Tool = strings.TrimSpace(b.Tool)
	b.Title = strings.Join(strings.Fields(b.Title), " ")
	b.TurnID = strings.TrimSpace(b.TurnID)
	b.Reason = strings.TrimSpace(b.Reason)
	b.ThreadKey = strings.TrimSpace(b.ThreadKey)
	b.RegisterNonce = strings.TrimSpace(b.RegisterNonce)
	b.Sidebar = strings.TrimSpace(b.Sidebar)
	switch {
	case !idRe.MatchString(b.Thread):
		return fmt.Errorf("thread: want 1-128 of [A-Za-z0-9._:-]")
	case (b.sidebarPresent || b.Sidebar != "") && !idRe.MatchString(b.Sidebar):
		return fmt.Errorf("sidebar: want 1-128 of [A-Za-z0-9._:-]")
	case !vendRe.MatchString(b.Vendor):
		return fmt.Errorf("vendor: want a lowercase name")
	case !hostRe.MatchString(b.Host):
		return fmt.Errorf("host: want a host name")
	case b.Work != "" && !workRe.MatchString(b.Work):
		return fmt.Errorf("work: want an id like W-54")
	case b.Tool != "" && !toolRe.MatchString(b.Tool):
		return fmt.Errorf("tool: want a tool name")
	case b.ToolTimeoutS < 0 || b.ToolTimeoutS > maxToolTimeout:
		return fmt.Errorf("tool_timeout_s: want 0..%d", maxToolTimeout)
	case !turnRe.MatchString(b.TurnID):
		return fmt.Errorf("turn_id: want 1-128 of [A-Za-z0-9._:-]")
	case b.Seq == 0:
		return fmt.Errorf("seq: want a sequence that starts at 1")
	case b.ThreadKey != "" && !keyRe.MatchString(b.ThreadKey):
		return fmt.Errorf("thread_key: want 64 hex characters")
	case b.RegisterNonce != "" && !keyRe.MatchString(b.RegisterNonce):
		return fmt.Errorf("register_nonce: want 64 hex characters")
	}
	switch b.Event {
	case TurnStart, BeforeTool, AfterTool, TurnEnd, WaitingOwner:
	default:
		return fmt.Errorf("event: want turn_start, before_tool, after_tool, turn_end or waiting_owner")
	}
	if b.Event == WaitingOwner {
		if _, ok := waitingReasons[b.Reason]; !ok {
			return fmt.Errorf("reason: want permission_prompt, idle_prompt or elicitation")
		}
	} else {
		b.Reason = ""
	}
	if b.Event != BeforeTool {
		b.ToolTimeoutS = 0
	}
	if r := []rune(b.Title); len(r) > maxText {
		b.Title = string(r[:maxText])
	}
	return nil
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
	if seen && olderTurn(t, b) {
		return false
	}
	if seen && b.Seq <= t.Seq {
		return false
	}
	if b.keyHash != "" && t.KeyHash != "" && b.keyHash != t.KeyHash {
		return false
	}
	// A headless wrapper's progress beat is not the end of a tool call whose
	// declared timeout is still running. Keep the declared window.
	if seen && b.Event == AfterTool && b.Tool == "headless-progress" && b.TurnID == t.TurnID &&
		t.Event == BeforeTool && t.ToolTimeoutS > 0 && at.Sub(t.At) < time.Duration(t.ToolTimeoutS)*time.Second {
		t.Seq = b.Seq
		st.Threads[k] = t
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
	t.TurnID, t.Seq = b.TurnID, b.Seq
	if b.Sidebar != "" {
		t.Sidebar = b.Sidebar
	}
	if b.Event == TurnStart {
		t.OpenedTurn = true
		t.OpenedTurnAt = at
	}
	t.At = at
	// A new event is a different silence. A claim for the previous one must
	// not block it, and must not be completed against the new timestamp.
	t.SilenceClaimID = ""
	t.SilenceClaimExpires = time.Time{}
	st.Threads[k] = t
	return true
}

// olderTurn reports that b is not the current turn and is not a turn_start
// that opens a turn this thread has not left.
func olderTurn(t Thread, b Beat) bool {
	if t.TurnID == "" || b.TurnID == t.TurnID {
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
		if t.At.Before(cutoff) {
			delete(st.Threads, k)
		}
	}
	st.evictFinishedThreads()
	for k, h := range st.Hosts {
		if h.At.Before(cutoff) && !cfg.hostIsLost(h, now) {
			delete(st.Hosts, k)
		}
	}
}

// evictFinishedThreads removes the oldest finished threads until the cap
// fits. It never removes a thread that has not ended.
func (st *State) evictFinishedThreads() {
	for len(st.Threads) > maxThreads {
		victim := ""
		var oldest time.Time
		for k, t := range st.Threads {
			if t.Event != TurnEnd {
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
	st.evictFinishedThreads()
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

// Validate checks a host heartbeat.
func (b *HostBeat) Validate() error {
	b.Host = strings.TrimSpace(b.Host)
	if !hostRe.MatchString(b.Host) {
		return fmt.Errorf("host: want a host name")
	}
	if b.Vendors == nil {
		b.Vendors = map[string]VendorReport{}
	}
	for name := range b.Vendors {
		if !knownVendor(name) {
			return fmt.Errorf("vendor: want claude, cursor or codex")
		}
	}
	b.Vendors = fillVendors(b.Vendors)
	return nil
}

func knownVendor(name string) bool {
	for _, v := range Vendors {
		if v == name {
			return true
		}
	}
	return false
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
