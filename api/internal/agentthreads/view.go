package agentthreads

import (
	"encoding/json"
	"sort"
	"time"
)

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

// HostViews lists hosts that have reported and expected hosts that never have.
// Never-reported comes first, then lost, then alive. A never-reported host has
// no heartbeat time and every vendor is not monitored.
func (c Config) HostViews(st State, now time.Time) []HostView {
	out := make([]HostView, 0, len(st.Hosts)+len(c.ExpectedHosts))
	seen := map[string]bool{}
	for _, h := range st.Hosts {
		status := HostAlive
		if c.HostDown(st, h.Host, now) {
			status = HostLostState
		}
		seen[h.Host] = true
		out = append(out, HostView{Host: h.Host, At: h.At, AgeSeconds: secs(now.Sub(h.At)), Status: status, Vendors: vendorViews(h.Vendors)})
	}
	for _, name := range c.ExpectedHosts {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, HostView{Host: name, Status: HostNeverReported, Vendors: vendorViews(nil)})
	}
	sort.Slice(out, func(i, j int) bool {
		if hostRank(out[i].Status) != hostRank(out[j].Status) {
			return hostRank(out[i].Status) < hostRank(out[j].Status)
		}
		if out[i].AgeSeconds != out[j].AgeSeconds {
			return out[i].AgeSeconds > out[j].AgeSeconds
		}
		return out[i].Host < out[j].Host
	})
	return out
}

func hostRank(status string) int {
	switch status {
	case HostNeverReported:
		return 0
	case HostLostState:
		return 1
	default:
		return 2
	}
}

func vendorViews(in map[string]VendorReport) []VendorView {
	vendors := make([]VendorView, 0, len(Vendors))
	filled := fillVendors(in)
	for _, name := range Vendors {
		r := filled[name]
		vendors = append(vendors, VendorView{Vendor: name, Wired: r.Wired, Token: r.Token, Monitored: r.Monitored()})
	}
	return vendors
}

func secs(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d / time.Second)
}
