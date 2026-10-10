// Package agentthreads notices an agent session that stopped in the middle of
// a turn (host asleep, process hung, network gone) and pages the Owner once.
//
// Each session's hooks report four events through the reporter role: turn
// start, before a tool call (with the timeout the tool declared), after a tool
// call, and turn end. A thread whose last event is not turn end, and whose last
// event is older than max(SilentAfter, declared timeout + ToolGrace), is
// silent. The workers loop pushes once when a thread goes silent; a later event
// ends the silence without a push. Detection and notice only: nothing here
// restarts, reassigns or touches a session.
//
// State is one statefile key. The API pod buffers events and folds them in every
// few seconds; the workers pod reads that key and writes back only which
// silence it already pushed.
package agentthreads

import (
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
	TurnStart  Event = "turn_start"
	BeforeTool Event = "before_tool"
	AfterTool  Event = "after_tool"
	TurnEnd    Event = "turn_end"
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
)

const (
	maxToolTimeout = 24 * 60 * 60
	maxThreads     = 300
)

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
	At            time.Time `json:"at"`
	TurnStartedAt time.Time `json:"turn_started_at"`
	// NotifiedFor is At of the event whose silence was pushed. A new event
	// moves At, so the next silence is a new one.
	NotifiedFor time.Time `json:"notified_for,omitempty"`
	NotifiedAt  time.Time `json:"notified_at,omitempty"`
}

// State is the statefile document.
type State struct {
	Threads map[string]Thread `json:"threads"`
}

// Key is the map key of a thread: vendor and thread id.
func Key(vendor, thread string) string { return vendor + "/" + thread }

var (
	idRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	vendRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hostRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	workRe  = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[0-9A-Za-z-]{1,24}$`)
	toolRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,96}$`)
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
	}
	switch b.Event {
	case TurnStart, BeforeTool, AfterTool, TurnEnd:
	default:
		return fmt.Errorf("event: want turn_start, before_tool, after_tool or turn_end")
	}
	if b.Event != BeforeTool {
		b.ToolTimeoutS = 0
	}
	if r := []rune(b.Title); len(r) > maxText {
		b.Title = string(r[:maxText])
	}
	return nil
}

// Apply folds one beat received at `at` into st.
func (st *State) Apply(b Beat, at time.Time) {
	if st.Threads == nil {
		st.Threads = map[string]Thread{}
	}
	k := Key(b.Vendor, b.Thread)
	t, seen := st.Threads[k]
	if seen && at.Before(t.At) {
		return
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
	t.At = at
	st.Threads[k] = t
}

// Prune drops threads last heard before cutoff and keeps the newest maxThreads.
func (st *State) Prune(cutoff time.Time) {
	for k, t := range st.Threads {
		if t.At.Before(cutoff) {
			delete(st.Threads, k)
		}
	}
	if len(st.Threads) <= maxThreads {
		return
	}
	keys := make([]string, 0, len(st.Threads))
	for k := range st.Threads {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return st.Threads[keys[i]].At.After(st.Threads[keys[j]].At) })
	for _, k := range keys[maxThreads:] {
		delete(st.Threads, k)
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
	// Retain drops threads not heard from for this long.
	Retain time.Duration
}

// DefaultConfig is the Owner's 2026-10-10 spec (STEP0-PLAN 0.9).
func DefaultConfig() Config {
	return Config{SilentAfter: 10 * time.Minute, ToolGrace: 2 * time.Minute, PushWithin: 6 * time.Hour, Retain: 72 * time.Hour}
}

// ConfigFromEnv reads PLATFORM_AGENT_THREAD_{SILENT_AFTER,TOOL_GRACE,PUSH_WITHIN,RETAIN}
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
	return c
}

// Threshold is how long t may stay quiet: max(SilentAfter, declared timeout + ToolGrace)
// while a tool with a declared timeout runs, SilentAfter otherwise.
func (c Config) Threshold(t Thread) time.Duration {
	d := c.SilentAfter
	if t.Event == BeforeTool && t.ToolTimeoutS > 0 {
		if tool := time.Duration(t.ToolTimeoutS)*time.Second + c.ToolGrace; tool > d {
			d = tool
		}
	}
	return d
}

// StatusOf classifies t at now.
func (c Config) StatusOf(t Thread, now time.Time) Status {
	if t.Event == TurnEnd {
		return Idle
	}
	if now.Sub(t.At) > c.Threshold(t) {
		return Silent
	}
	return InTurn
}

// Pushed reports whether this silence of t was already pushed (or marked).
func (t Thread) Pushed() bool { return !t.NotifiedFor.IsZero() && t.NotifiedFor.Equal(t.At) }

// Due lists the keys of silent threads whose silence has not been pushed, and
// whether each is fresh enough to push (false = mark only).
func (c Config) Due(st State, now time.Time) (push, mark []string) {
	for k, t := range st.Threads {
		if c.StatusOf(t, now) != Silent || t.Pushed() {
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
		v := View{Thread: t, Status: c.StatusOf(t, now), QuietSeconds: secs(now.Sub(t.At)), ThresholdSeconds: secs(c.Threshold(t))}
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
