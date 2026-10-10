package agentthreads

import (
	"os"
	"sort"
	"strings"
	"time"
)

func (c Config) hostIsLost(h Host, now time.Time) bool {
	if h.At.IsZero() {
		return false
	}
	return now.Sub(h.At) > c.HostLostAfter
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
	// Retain drops threads not heard from for this long, including one still
	// mid-turn. A lost host is not dropped by retain.
	Retain time.Duration
	// HostLostAfter is how long a host may go without a heartbeat.
	HostLostAfter time.Duration
	// ExpectedHosts are machines that should report. One that never has is
	// listed as never reported. Names come from the environment; invalid
	// names are dropped.
	ExpectedHosts []string
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
// (Go durations); unset or invalid keeps the default. It also reads
// PLATFORM_AGENT_THREAD_EXPECTED_HOSTS, a comma-separated host list. A blank
// entry, a duplicate, or a name that is not a host name is dropped.
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
	c.ExpectedHosts = expectedHostsFromEnv()
	return c
}

func expectedHostsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("PLATFORM_AGENT_THREAD_EXPECTED_HOSTS"))
	if raw == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" || !hostRe.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
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
// host-loss push, or that a live claim is about to announce it. A later
// event, with At after that heartbeat, can page again.
func (c Config) silenceCovered(st State, t Thread, now time.Time) bool {
	h, ok := st.Hosts[t.Host]
	if !ok {
		return false
	}
	if !h.LostNotifiedFor.IsZero() && !t.At.After(h.LostNotifiedFor) {
		return true
	}
	return claimPending(h.LossClaimID, h.LossClaimExpires, now) && !t.At.After(h.At)
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
// A claim that has not completed is not pushed.
func (t Thread) Pushed() bool { return !t.NotifiedFor.IsZero() && t.NotifiedFor.Equal(t.At) }

func (t Thread) silenceClaimed(now time.Time) bool {
	return claimPending(t.SilenceClaimID, t.SilenceClaimExpires, now)
}

// claimPending reports a send that was claimed and has not expired.
func claimPending(id string, exp, now time.Time) bool {
	return id != "" && !exp.IsZero() && now.Before(exp)
}

// Due lists the keys of silent threads whose silence has not been pushed, and
// whether each is fresh enough to push (false = mark only). A live claim is
// left to the worker that holds it.
func (c Config) Due(st State, now time.Time) (push, mark []string) {
	for k, t := range st.Threads {
		if c.StatusOf(t, c.HostDown(st, t.Host, now), now) != Silent || t.Pushed() || t.silenceClaimed(now) || c.silenceCovered(st, t, now) {
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
// pushed, and whether each is fresh enough to push (false = mark only). A live
// claim is left to the worker that holds it.
func (c Config) HostsDue(st State, now time.Time) (push, mark []string) {
	for name, h := range st.Hosts {
		if !c.HostDown(st, name, now) || h.LostNotifiedFor.Equal(h.At) || claimPending(h.LossClaimID, h.LossClaimExpires, now) {
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
