package store

import (
	"math"
	"time"
)

// Slot signals (R5 phase 2 step 3b, T5; the Owner 2026-11-06 Q3/Q4): what a
// policy that moves a host's effective slot count reads. agentd's claim
// carries the host's headroom (load1, cpus, mem_avail_bytes,
// mem_total_bytes; optional, with the same unknown-field fallback as the
// disk fields), and SlotSignals puts it with what the platform knows:
// the vendor pools' rate-limit refusals, the leasable queue and the
// host's attempt durations. GET /hosts shows them for every host.
//
// The headroom is recorded on a stream of the host's own (HostHeadroomStream),
// read through its head as the offers are (presence.go): agentrt.host
// takes a write only with a host_registered, host_state_changed or
// host_retired event (0002's event_may_write) and capacity_json is not a
// liveness column, so writing host.capacity_json.headroom needs a migration
// the plan did not reserve a number for (0013 is the rule approval, Q1). A
// claim records its headroom when it moved materially or the last record
// is HeadroomEvery old, not on every poll.

// HeadroomEvery is how old a host's headroom record may get while it does
// not move materially before a claim records it again.
var HeadroomEvery = 5 * time.Minute

// SlotSignalInterval is the interval signals are counted over when the
// caller names none: refusals and durations since one interval ago, the
// durations' previous interval the one before.
const SlotSignalInterval = time.Hour

// Headroom is what a claim says of the host's load and memory; each field
// is nil when the claim did not carry it. At is when the platform recorded
// it.
type Headroom struct {
	Load1    *float64   `json:"load1,omitempty"`
	CPUs     *int       `json:"cpus,omitempty"`
	MemAvail *int64     `json:"mem_avail_bytes,omitempty"`
	MemTotal *int64     `json:"mem_total_bytes,omitempty"`
	At       *time.Time `json:"at,omitempty"`
}

// Empty says whether the claim carried none of it.
func (h Headroom) Empty() bool {
	return h.Load1 == nil && h.CPUs == nil && h.MemAvail == nil && h.MemTotal == nil
}

// Check refuses what no host can report: a negative or not finite load, no
// cpu, negative memory, more available than in total.
func (h Headroom) Check() error {
	switch {
	case h.Load1 != nil && (math.IsNaN(*h.Load1) || math.IsInf(*h.Load1, 0) || *h.Load1 < 0):
		return invalid("load1 is a load, not negative")
	case h.CPUs != nil && *h.CPUs < 1:
		return invalid("cpus is at least 1")
	case (h.MemAvail == nil) != (h.MemTotal == nil):
		return invalid("mem_avail_bytes and mem_total_bytes go together")
	case h.MemAvail != nil && (*h.MemAvail < 0 || *h.MemTotal <= 0 || *h.MemAvail > *h.MemTotal):
		return invalid("memory available is between 0 and the total, and the total is more than 0")
	}
	return nil
}

// PoolRefusals are the rate-limit refusals of one vendor pool (tool:account,
// every window of it) in the interval: the refusals recorded (pool_measured
// limited: an executor's exit text or the Owner's reading), the attempts
// that ended on one (an exit text recorded with the attempt's computed),
// the cooldowns set (pool_cooldown_set to a time after it was set; a lift
// is not one), the cooldown in force now, and the last refusal or cooldown.
type PoolRefusals struct {
	Pool            string     `json:"vendor_pool"`
	Refusals        int        `json:"refusals"`
	LimitedAttempts int        `json:"limited_attempts"`
	Cooldowns       int        `json:"cooldowns"`
	CoolingUntil    *time.Time `json:"cooling_until"`
	LastAt          *time.Time `json:"last_at"`
}

// Durations are the median wall time from start to computed of the host's
// attempts computed in the interval and in the one before it, and how many
// each counts; a median is nil when there is none.
type Durations struct {
	MedianS         *float64 `json:"median_s"`
	Count           int      `json:"count"`
	PreviousMedianS *float64 `json:"previous_median_s"`
	PreviousCount   int      `json:"previous_count"`
}

// HostSlotSignals are a host's slot signals over the interval from Since to At:
// its latest headroom (nil until a claim reported one), the refusals of each
// vendor pool its offered tools route to (a tool's executor under the
// account every host uses, DefaultAccount), the leasable queue, and its
// attempts' durations.
type HostSlotSignals struct {
	HostID     string         `json:"host_id"`
	Since      time.Time      `json:"since"`
	At         time.Time      `json:"at"`
	IntervalS  int64          `json:"interval_s"`
	Headroom   *Headroom      `json:"headroom"`
	Pools      []PoolRefusals `json:"pools"`
	QueueDepth int            `json:"queue_depth"`
	Durations  Durations      `json:"durations"`
}
