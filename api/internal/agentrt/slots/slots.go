// Package slots decides how many attempts a host is given at once (R5 phase
// 2 step 3b, elastic executor slots; the Owner 2026-11-06 Q3, Q4). A policy
// takes the host's headroom signals, which its agentd sends with each claim,
// and the count it has now, and says the count it should have; the
// concurrency rule's slots block names the policy and its parameters, so a
// policy is switched by changing the rule, never by a release. The
// controller asks for a decision every interval and records each move
// (host_slots_set); the claim leases up to the count recorded.
//
// The count is always clamped to min..max, max being the rule's per_host
// held under Ceiling, whatever a policy says.
package slots

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Ceiling is the most slots any host is given, whatever a rule says: the
// rules' own bound on per_host.
const Ceiling = rules.PerHostCeiling

// Signals are a host's headroom as its latest claim reported it, and how
// many attempts it runs now. Known is false when the host sent none, or
// none recent enough to act on.
type Signals struct {
	Known    bool
	Load1    float64 // the one-minute load average
	CPUs     int
	MemAvail int64 // bytes
	MemTotal int64 // bytes
	Live     int   // the host's leased, running and stale attempts
}

// LoadPercent is the one-minute load per CPU in percent; ok false when not
// known.
func (s Signals) LoadPercent() (float64, bool) {
	if !s.Known || s.CPUs <= 0 || s.Load1 < 0 {
		return 0, false
	}
	return s.Load1 * 100 / float64(s.CPUs), true
}

// MemPercent is the share of memory available in percent; ok false when not
// known.
func (s Signals) MemPercent() (float64, bool) {
	if !s.Known || s.MemTotal <= 0 || s.MemAvail < 0 {
		return 0, false
	}
	return float64(s.MemAvail) * 100 / float64(s.MemTotal), true
}

// Params are a policy's configuration: the range, where a host starts, how
// far one move goes, and the headroom thresholds.
type Params struct {
	Min, Start, Max int
	Step            int
	IntervalS       int
	Thresholds      rules.SlotThresholds
}

// Doc is p as an event body has it.
func (p Params) Doc() map[string]any {
	return map[string]any{"min": p.Min, "start": p.Start, "max": p.Max, "step": p.Step, "interval_s": p.IntervalS,
		"thresholds": map[string]any{"load_high_percent": p.Thresholds.LoadHighPercent,
			"load_low_percent": p.Thresholds.LoadLowPercent, "mem_low_percent": p.Thresholds.MemLowPercent}}
}

// Config is the policy a concurrency rule names and its parameters. A rule
// without a slots block is fixed at per_host (Elastic false: the claim then
// reads no recorded count, as before the block existed).
type Config struct {
	Policy  string
	Params  Params
	Elastic bool
}

// ConfigOf is r's configuration (r a concurrency rule).
func ConfigOf(r rules.Rule) Config {
	per := 0
	if r.PerHost != nil {
		per = *r.PerHost
	}
	max := min(per, Ceiling)
	s := r.Slots
	if s == nil {
		return Config{Policy: rules.SlotPolicyFixed, Params: Params{Min: max, Start: max, Max: max, Step: rules.DefaultSlotStep,
			IntervalS: rules.DefaultSlotIntervalS, Thresholds: rules.SlotThresholds{LoadHighPercent: rules.DefaultLoadHighPercent,
				LoadLowPercent: rules.DefaultLoadLowPercent, MemLowPercent: rules.DefaultMemLowPercent}}}
	}
	return Config{Policy: s.Policy, Elastic: true, Params: Params{Min: min(s.Min, max), Start: Clamp(s.Start, s.Min, max), Max: max,
		Step: s.Step, IntervalS: s.IntervalS, Thresholds: s.Thresholds}}
}

// Policy decides a host's count: from the signals and the count it has
// now, the count it should have and why, in words for the Owner. What it
// says is clamped afterwards (Decide).
type Policy interface {
	Target(current int, s Signals, p Params) (target int, reason string)
}

var registry = map[string]Policy{rules.SlotPolicyAIMD: AIMD{}, rules.SlotPolicyFixed: Fixed{}}

// Lookup is the policy registered under name.
func Lookup(name string) (Policy, error) {
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("slots: no policy %q; the policies are %v", name, Names())
	}
	return p, nil
}

// Names are the registered policies, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Clamp holds n to lo..hi (hi wins when they cross).
func Clamp(n, lo, hi int) int {
	return max(min(n, hi), min(lo, hi))
}

// Decision is one decision of a host's count: the policy and its parameters,
// what it read, the count before and after, and why.
type Decision struct {
	Policy   string
	Params   Params
	Signals  Signals
	Old, New int
	Reason   string
}

// Moved says whether the count changes.
func (d Decision) Moved() bool { return d.Old != d.New }

// Decide asks c's policy for the count of a host that has current (-1: none
// recorded yet; it then has the start) and clamps the answer to the range.
func Decide(c Config, current int, s Signals) (Decision, error) {
	p, err := Lookup(c.Policy)
	if err != nil {
		return Decision{}, err
	}
	old := current
	if old < 0 {
		old = c.Params.Start
	}
	held := Clamp(old, c.Params.Min, c.Params.Max)
	target, reason := p.Target(held, s, c.Params)
	n := Clamp(target, c.Params.Min, c.Params.Max)
	var why []string
	if held != old {
		why = append(why, fmt.Sprintf("the rule's range is now %d..%d", c.Params.Min, c.Params.Max))
	}
	why = append(why, reason)
	if n != target {
		why = append(why, fmt.Sprintf("held to %d..%d", c.Params.Min, c.Params.Max))
	}
	return Decision{Policy: c.Policy, Params: c.Params, Signals: s, Old: old, New: n, Reason: strings.Join(why, "; ")}, nil
}

// SignalsDoc is s as an event body has it: the readings it has, and the live
// attempts. An event body holds no float (package canon): the load is in
// hundredths.
func SignalsDoc(s Signals) map[string]any {
	doc := map[string]any{"known": s.Known, "live": s.Live}
	if !s.Known {
		return doc
	}
	if s.CPUs > 0 {
		doc["load1_hundredths"], doc["cpus"] = int64(math.Round(s.Load1*100)), s.CPUs
	}
	if s.MemTotal > 0 {
		doc["mem_avail_bytes"], doc["mem_total_bytes"] = s.MemAvail, s.MemTotal
	}
	return doc
}

// Fixed holds a host at the start.
type Fixed struct{}

// Target is p.Start.
func (Fixed) Target(_ int, _ Signals, p Params) (int, string) {
	return p.Start, fmt.Sprintf("fixed at %d", p.Start)
}

// AIMD moves a host's count by its headroom, as TCP moves its window: a step
// up when the host is easy and uses every slot it has, halved when it is
// overloaded (its load per CPU above the high threshold, or its available
// memory below the low one), held otherwise. Without recent signals it holds.
type AIMD struct{}

// Target is the count AIMD gives.
func (AIMD) Target(current int, s Signals, p Params) (int, string) {
	load, haveLoad := s.LoadPercent()
	mem, haveMem := s.MemPercent()
	t := p.Thresholds
	switch {
	case !haveLoad && !haveMem:
		return current, fmt.Sprintf("no recent headroom signals from the host: held at %d", current)
	case haveMem && mem < float64(t.MemLowPercent):
		n := current / 2
		return n, fmt.Sprintf("memory available %.0f%% is below %d%%: halved from %d to %d", mem, t.MemLowPercent, current, n)
	case haveLoad && load > float64(t.LoadHighPercent):
		n := current / 2
		return n, fmt.Sprintf("load per CPU %.0f%% is above %d%%: halved from %d to %d", load, t.LoadHighPercent, current, n)
	case !haveLoad:
		return current, fmt.Sprintf("the host's load is not known: held at %d", current)
	case load >= float64(t.LoadLowPercent):
		return current, fmt.Sprintf("load per CPU %.0f%% is between %d%% and %d%%: held at %d", load, t.LoadLowPercent, t.LoadHighPercent, current)
	case s.Live < current:
		return current, fmt.Sprintf("the host runs %d attempts of its %d slots: held", s.Live, current)
	}
	n := current + p.Step
	return n, fmt.Sprintf("load per CPU %.0f%% is below %d%% with every slot busy: up from %d to %d", load, t.LoadLowPercent, current, n)
}
