package rules

import (
	"errors"
	"fmt"
	"slices"
)

// Elastic executor slots (R5 phase 2 step 3b; the Owner 2026-11-06 Q3-Q5):
// the concurrency rule may carry a slots block. per_host is then the most a
// host is given, and the block says how the platform moves each host's count
// between min and that: a policy (package slots: aimd moves it by the host's
// headroom, fixed holds it at start), where it starts, how often it may move
// and by how much, and the headroom thresholds. A rule without the block is
// fixed at per_host, as before, and its doc is what it was.

// The slot policies a slots block may name; package slots registers one of
// each (TestRegistryIsTheRulesPolicies holds the two together).
const (
	SlotPolicyAIMD  = "aimd"
	SlotPolicyFixed = "fixed"
)

// SlotPolicies are the policy names a slots block takes.
var SlotPolicies = []string{SlotPolicyAIMD, SlotPolicyFixed}

// Bounds and defaults of a slots block.
const (
	MinSlotIntervalS     = 60
	MaxSlotIntervalS     = 86400
	DefaultSlotIntervalS = 300
	DefaultSlotStep      = 1
	// The headroom thresholds, in percent: the one-minute load per CPU
	// above which the count comes down and below which it may go up, and
	// the share of memory available below which it comes down.
	DefaultLoadHighPercent = 100
	DefaultLoadLowPercent  = 70
	DefaultMemLowPercent   = 10
	maxLoadPercent         = 1000
)

// SlotThresholds are the headroom thresholds a policy reads (aimd).
type SlotThresholds struct {
	LoadHighPercent int
	LoadLowPercent  int
	MemLowPercent   int
}

// Slots is a concurrency rule's slots block, its defaults filled in.
type Slots struct {
	Policy     string
	Min, Start int
	IntervalS  int
	Step       int
	Thresholds SlotThresholds
}

// slots reads the concurrency rule's optional slots block against per_host:
// policy, min and start are required (min <= start <= per_host); interval_s,
// step and thresholds take their defaults.
func (r *Rule) slots(o *object) error {
	n, err := o.node("slots", true)
	if err != nil || n == nil {
		return err
	}
	b, err := mapping(n, o.where+".slots")
	if err != nil {
		return err
	}
	s := &Slots{IntervalS: DefaultSlotIntervalS, Step: DefaultSlotStep,
		Thresholds: SlotThresholds{DefaultLoadHighPercent, DefaultLoadLowPercent, DefaultMemLowPercent}}
	if s.Policy, err = b.str("policy", false); err != nil {
		return err
	}
	if !slices.Contains(SlotPolicies, s.Policy) {
		return fmt.Errorf("%s.policy is one of %v, not %q", b.where, SlotPolicies, s.Policy)
	}
	max := *r.PerHost
	if s.Min, err = b.integer("min", 0, max); err != nil {
		return err
	}
	if s.Start, err = b.integer("start", s.Min, max); err != nil {
		return err
	}
	if _, given := b.fields["interval_s"]; given {
		if s.IntervalS, err = b.integer("interval_s", MinSlotIntervalS, MaxSlotIntervalS); err != nil {
			return err
		}
	}
	if _, given := b.fields["step"]; given {
		if s.Step, err = b.integer("step", 1, PerHostCeiling); err != nil {
			return err
		}
	}
	if _, given := b.fields["thresholds"]; given {
		t, err := b.object("thresholds")
		if err != nil {
			return err
		}
		for key, at := range map[string]*int{"load_high_percent": &s.Thresholds.LoadHighPercent,
			"load_low_percent": &s.Thresholds.LoadLowPercent, "mem_low_percent": &s.Thresholds.MemLowPercent} {
			if _, given := t.fields[key]; !given {
				continue
			}
			hi := maxLoadPercent
			if key == "mem_low_percent" {
				hi = 100
			}
			if *at, err = t.integer(key, 0, hi); err != nil {
				return err
			}
		}
		if err = t.done(); err != nil {
			return err
		}
		if s.Thresholds.LoadLowPercent >= s.Thresholds.LoadHighPercent {
			return fmt.Errorf("%s.thresholds: load_low_percent is below load_high_percent", b.where)
		}
	}
	if err = b.done(); err != nil {
		return err
	}
	r.Slots = s
	return nil
}

// doc is the block as the rule's doc has it: every field, defaults filled.
func (s *Slots) doc() map[string]any {
	return map[string]any{"policy": s.Policy, "min": s.Min, "start": s.Start, "interval_s": s.IntervalS, "step": s.Step,
		"thresholds": map[string]any{"load_high_percent": s.Thresholds.LoadHighPercent,
			"load_low_percent": s.Thresholds.LoadLowPercent, "mem_low_percent": s.Thresholds.MemLowPercent}}
}

// SlotRange is the least and the most slots a concurrency rule may give a
// host: without a block both per_host; fixed, start; aimd, min to per_host.
func SlotRange(r Rule) (lo, hi int) {
	switch s := r.Slots; {
	case r.PerHost == nil:
		return 0, 0
	case s == nil:
		return *r.PerHost, *r.PerHost
	case s.Policy == SlotPolicyFixed:
		return s.Start, s.Start
	default:
		return s.Min, *r.PerHost
	}
}

// slotsTighten: per_host does not go up, and the range of counts the rule may
// give a host neither reaches higher nor holds a host at more than before.
// Interval, step and thresholds only shape how the count moves inside that
// range.
func slotsTighten(prev, next Rule) error {
	if *next.PerHost > *prev.PerHost {
		return errors.New("per_host goes up")
	}
	plo, phi := SlotRange(prev)
	nlo, nhi := SlotRange(next)
	switch {
	case nhi > phi:
		return fmt.Errorf("the most slots a host may be given goes up from %d to %d", phi, nhi)
	case nlo > plo:
		return fmt.Errorf("the least slots a host is held at goes up from %d to %d", plo, nlo)
	}
	return nil
}
