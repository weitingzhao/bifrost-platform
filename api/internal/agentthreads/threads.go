// Package agentthreads notices an agent session that stopped in the middle of
// a turn (host asleep, process hung, network gone) and pages the Owner once.
//
// Host lost within 3 minutes; session silence after max(10 minutes, declared tool timeout + 2 minutes).
//
// The first event of a thread is issued a random thread key. Only the hash is
// stored, and every later event for that vendor and thread must present the
// key. Each event carries a per-thread turn id and a sequence that only grows;
// a sequence that is not newer is ignored. Silence and host loss are timed
// from the server clock. A waiting_owner thread stays in progress and is not
// paged. A lost host is paged once, and its mid-turn threads are shown as host
// lost; the heartbeat that marks the host back is not paged.
//
// Detection and notice only: nothing here restarts, reassigns or touches a
// session. State is one statefile key. The API pod buffers events and folds
// them in every few seconds; the workers pod reads that key and writes back
// only which silence or host loss it already pushed.
package agentthreads

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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
	// HostAlive and HostLostState are the host list's status strings.
	HostAlive     = "alive"
	HostLostState = "lost"
)

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
	TurnID    string `json:"turn_id"`
	Seq       uint64 `json:"seq"`
	Reason    string `json:"reason,omitempty"`
	// keyHash is the server-side sha256 of ThreadKey. The client cannot set it.
	keyHash string
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
	// NotifiedFor is At of the event whose silence was pushed. A new event
	// moves At, so the next silence is a new one.
	NotifiedFor time.Time `json:"notified_for,omitempty"`
	NotifiedAt  time.Time `json:"notified_at,omitempty"`
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
	// A newer heartbeat does not push; the next time that newer one ages out does.
	LostNotifiedFor time.Time `json:"lost_notified_for,omitempty"`
	LostNotifiedAt  time.Time `json:"lost_notified_at,omitempty"`
}

// State is the statefile document.
type State struct {
	Threads map[string]Thread `json:"threads"`
	Hosts   map[string]Host   `json:"hosts,omitempty"`
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

// ErrKeyRejected means the caller did not present the key that started the thread.
var ErrKeyRejected = fmt.Errorf("thread key rejected")

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
	switch {
	case !idRe.MatchString(b.Thread):
		return fmt.Errorf("thread: want 1-128 of [A-Za-z0-9._:-]")
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
// newer than the one already stored is ignored. The receive time is not an
// order key; it is what silence is measured from. Apply reports whether the
// beat was stored.
func (st *State) Apply(b Beat, at time.Time) bool {
	if st.Threads == nil {
		st.Threads = map[string]Thread{}
	}
	k := Key(b.Vendor, b.Thread)
	t, seen := st.Threads[k]
	if seen && b.Seq <= t.Seq {
		return false
	}
	if b.keyHash != "" && t.KeyHash != "" && b.keyHash != t.KeyHash {
		return false
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
	t.At = at
	st.Threads[k] = t
	return true
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
	st.Hosts[b.Host] = h
	st.boundHosts()
}

// Prune drops threads and hosts last heard before cutoff, then enforces the caps.
func (st *State) Prune(cutoff time.Time) {
	for k, t := range st.Threads {
		if t.At.Before(cutoff) {
			delete(st.Threads, k)
		}
	}
	if len(st.Threads) > maxThreads {
		keys := make([]string, 0, len(st.Threads))
		for k := range st.Threads {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return st.Threads[keys[i]].At.After(st.Threads[keys[j]].At) })
		for _, k := range keys[maxThreads:] {
			delete(st.Threads, k)
		}
	}
	for k, h := range st.Hosts {
		if h.At.Before(cutoff) {
			delete(st.Hosts, k)
		}
	}
	st.boundHosts()
}

func (st *State) boundHosts() {
	if len(st.Hosts) <= maxHosts {
		return
	}
	keys := make([]string, 0, len(st.Hosts))
	for k := range st.Hosts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return st.Hosts[keys[i]].At.After(st.Hosts[keys[j]].At) })
	for _, k := range keys[maxHosts:] {
		delete(st.Hosts, k)
	}
}

// Config holds the thresholds. They move into the rule set when it exists.
type Config struct {
	// SilentAfter is the quiet time after which a mid-turn thread is silent.
	SilentAfter time.Duration
	// ToolGrace is added to a tool's declared timeout.
	ToolGrace time.Duration
	// PushWithin: a silence that began longer ago than this is marked, not
	// pushed (a thread left mid-turn days ago must not page on a restart).
	PushWithin time.Duration
	// Retain drops threads and hosts not heard from for this long.
	Retain time.Duration
	// HostLostAfter is how long a host may go without a heartbeat.
	HostLostAfter time.Duration
}

// DefaultConfig is what the environment overrides.
func DefaultConfig() Config {
	return Config{
		SilentAfter:   10 * time.Minute,
		ToolGrace:     2 * time.Minute,
		PushWithin:    6 * time.Hour,
		Retain:        72 * time.Hour,
		HostLostAfter: 3 * time.Minute,
	}
}

// ConfigFromEnv reads PLATFORM_AGENT_THREAD_{SILENT_AFTER,TOOL_GRACE,PUSH_WITHIN,RETAIN,HOST_LOST}
// (Go durations); unset or invalid keeps the default.
func ConfigFromEnv() Config {
	c := DefaultConfig()
	read := func(name string, into *time.Duration) {
		if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name))); err == nil && d > 0 {
			*into = d
		}
	}
	read("PLATFORM_AGENT_THREAD_SILENT_AFTER", &c.SilentAfter)
	read("PLATFORM_AGENT_THREAD_TOOL_GRACE", &c.ToolGrace)
	read("PLATFORM_AGENT_THREAD_PUSH_WITHIN", &c.PushWithin)
	read("PLATFORM_AGENT_THREAD_RETAIN", &c.Retain)
	read("PLATFORM_AGENT_THREAD_HOST_LOST", &c.HostLostAfter)
	return c
}

// Threshold is how long t may stay quiet before it is silent.
func (c Config) Threshold(t Thread) time.Duration {
	d := c.SilentAfter
	if t.Event == BeforeTool && t.ToolTimeoutS > 0 {
		if tool := time.Duration(t.ToolTimeoutS)*time.Second + c.ToolGrace; tool > d {
			d = tool
		}
	}
	return d
}

// StatusOf classifies t at now. hostLost forces every non-idle thread to host lost.
func (c Config) StatusOf(t Thread, hostLost bool, now time.Time) Status {
	if t.Event == TurnEnd {
		return Idle
	}
	if hostLost {
		return HostLost
	}
	if t.Event == WaitingOwner {
		return StatusWaiting
	}
	if now.Sub(t.At) > c.Threshold(t) {
		return Silent
	}
	return InTurn
}

// silenceCovered reports that this quiet stretch was already announced by a
// host-loss push. A later event, with At after that heartbeat, can page again.
func (c Config) silenceCovered(st State, t Thread) bool {
	h, ok := st.Hosts[t.Host]
	if !ok || h.LostNotifiedFor.IsZero() {
		return false
	}
	return !t.At.After(h.LostNotifiedFor)
}

// HostDown reports whether host has a heartbeat older than the host threshold.
// A host that has never reported is not down.
func (c Config) HostDown(st State, host string, now time.Time) bool {
	h, ok := st.Hosts[host]
	if !ok || h.At.IsZero() {
		return false
	}
	return now.Sub(h.At) > c.HostLostAfter
}

// Pushed reports whether this silence of t was already pushed (or marked).
func (t Thread) Pushed() bool { return !t.NotifiedFor.IsZero() && t.NotifiedFor.Equal(t.At) }

// Due lists the keys of silent threads whose silence has not been pushed, and
// whether each is fresh enough to push (false = mark only).
func (c Config) Due(st State, now time.Time) (push, mark []string) {
	for k, t := range st.Threads {
		if c.StatusOf(t, c.HostDown(st, t.Host, now), now) != Silent || t.Pushed() || c.silenceCovered(st, t) {
			continue
		}
		if now.Sub(t.At.Add(c.Threshold(t))) > c.PushWithin {
			mark = append(mark, k)
		} else {
			push = append(push, k)
		}
	}
	sort.Strings(push)
	sort.Strings(mark)
	return push, mark
}

// HostsDue lists hosts whose heartbeat has aged out and whose loss has not been
// pushed, and whether each is fresh enough to push (false = mark only).
func (c Config) HostsDue(st State, now time.Time) (push, mark []string) {
	for name, h := range st.Hosts {
		if !c.HostDown(st, name, now) || h.LostNotifiedFor.Equal(h.At) {
			continue
		}
		if now.Sub(h.At.Add(c.HostLostAfter)) > c.PushWithin {
			mark = append(mark, name)
		} else {
			push = append(push, name)
		}
	}
	sort.Strings(push)
	sort.Strings(mark)
	return push, mark
}

// MidTurn returns the host's threads that have not ended, sorted by id.
func (c Config) MidTurn(st State, host string) []Thread {
	out := make([]Thread, 0)
	for _, t := range st.Threads {
		if t.Host != host || t.Event == TurnEnd {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Thread < out[j].Thread })
	return out
}

// View is one thread as GET /api/v1/agent/threads returns it.
type View struct {
	Thread
	Status           Status `json:"status"`
	QuietSeconds     int64  `json:"quiet_seconds"`
	InTurnSeconds    int64  `json:"in_turn_seconds"`
	ThresholdSeconds int64  `json:"threshold_seconds"`
}

// Views classifies every thread, newest first.
func (c Config) Views(st State, now time.Time) []View {
	out := make([]View, 0, len(st.Threads))
	for _, t := range st.Threads {
		lost := c.HostDown(st, t.Host, now)
		v := View{Thread: t, Status: c.StatusOf(t, lost, now), QuietSeconds: secs(now.Sub(t.At)), ThresholdSeconds: secs(c.Threshold(t))}
		if v.Status != Idle {
			v.InTurnSeconds = secs(now.Sub(t.TurnStartedAt))
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return Key(out[i].Vendor, out[i].Thread.Thread) < Key(out[j].Vendor, out[j].Thread.Thread)
	})
	return out
}

// MarshalJSON omits the thread key, its hash, the turn id and the sequence.
func (v View) MarshalJSON() ([]byte, error) {
	t := v.Thread
	out := struct {
		Thread           string    `json:"thread"`
		Vendor           string    `json:"vendor"`
		Host             string    `json:"host"`
		Work             string    `json:"work,omitempty"`
		Title            string    `json:"title,omitempty"`
		Event            Event     `json:"event"`
		Tool             string    `json:"tool,omitempty"`
		ToolTimeoutS     int       `json:"tool_timeout_s,omitempty"`
		Reason           string    `json:"reason,omitempty"`
		At               time.Time `json:"at"`
		TurnStartedAt    time.Time `json:"turn_started_at"`
		NotifiedFor      time.Time `json:"notified_for,omitempty"`
		NotifiedAt       time.Time `json:"notified_at,omitempty"`
		Status           Status    `json:"status"`
		QuietSeconds     int64     `json:"quiet_seconds"`
		InTurnSeconds    int64     `json:"in_turn_seconds"`
		ThresholdSeconds int64     `json:"threshold_seconds"`
	}{
		Thread: t.Thread, Vendor: t.Vendor, Host: t.Host, Work: t.Work, Title: t.Title,
		Event: t.Event, Tool: t.Tool, ToolTimeoutS: t.ToolTimeoutS, Reason: t.Reason,
		At: t.At, TurnStartedAt: t.TurnStartedAt, NotifiedFor: t.NotifiedFor, NotifiedAt: t.NotifiedAt,
		Status: v.Status, QuietSeconds: v.QuietSeconds, InTurnSeconds: v.InTurnSeconds, ThresholdSeconds: v.ThresholdSeconds,
	}
	return json.Marshal(out)
}

// VendorView is one vendor as the list shows it.
type VendorView struct {
	Vendor    string `json:"vendor"`
	Wired     bool   `json:"wired"`
	Token     bool   `json:"token"`
	Monitored bool   `json:"monitored"`
}

// HostView is one host as the list shows it. It carries no thread key.
type HostView struct {
	Host       string       `json:"host"`
	At         time.Time    `json:"at"`
	AgeSeconds int64        `json:"age_seconds"`
	Status     string       `json:"status"`
	Vendors    []VendorView `json:"vendors"`
}

// HostViews lists hosts, lost first, then the ones quiet the longest.
func (c Config) HostViews(st State, now time.Time) []HostView {
	out := make([]HostView, 0, len(st.Hosts))
	for _, h := range st.Hosts {
		status := HostAlive
		if c.HostDown(st, h.Host, now) {
			status = HostLostState
		}
		vendors := make([]VendorView, 0, len(Vendors))
		filled := fillVendors(h.Vendors)
		for _, name := range Vendors {
			r := filled[name]
			vendors = append(vendors, VendorView{Vendor: name, Wired: r.Wired, Token: r.Token, Monitored: r.Monitored()})
		}
		out = append(out, HostView{Host: h.Host, At: h.At, AgeSeconds: secs(now.Sub(h.At)), Status: status, Vendors: vendors})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Status == HostLostState) != (out[j].Status == HostLostState) {
			return out[i].Status == HostLostState
		}
		if out[i].AgeSeconds != out[j].AgeSeconds {
			return out[i].AgeSeconds > out[j].AgeSeconds
		}
		return out[i].Host < out[j].Host
	})
	return out
}

func secs(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d / time.Second)
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

// Message is the push for a silent thread.
func (c Config) Message(t Thread, now time.Time) (title, body string) {
	quiet := now.Sub(t.At)
	title = fmt.Sprintf("Agent thread silent %s: %s", human(quiet), t.Name())
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
	body = strings.Join(parts, " · ") + ". Nothing was restarted."
	return title, body
}

// HostMessage is the one push for a host that stopped heartbeating.
func (c Config) HostMessage(h Host, mid []Thread, now time.Time) (title, body string) {
	age := now.Sub(h.At)
	title = fmt.Sprintf("Host lost %s: %s", human(age), h.Host)
	names := make([]string, 0, len(mid))
	for _, t := range mid {
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
