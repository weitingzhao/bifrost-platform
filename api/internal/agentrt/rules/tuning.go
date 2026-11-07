package rules

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// The tuning rule (R5 phase 2 step 3b, T4; the Owner 2026-11-06 Q5): the
// runtime's timeouts, retry counts and limits that are capacity, not
// authority, as configuration with the code's defaults. The registry below is
// closed: a rule names only keys it lists, each a whole number between its
// floor and its ceiling. A key the rule does not name, and every key while no
// tuning rule is in force or reads, keeps the code's default; a consumer
// reads the rule once per pass or at the lease. Nothing tunes itself: a
// value changes only with a new version of the rule. The rule changes only
// capacity, so a card may approve it (Q5); it has no tightening comparison,
// so it never takes effect unsigned.
//
//	rule_id: tuning
//	values:
//	  lease_ttl_s: 180
//	  assess_per_pass: 5
const KindTuning = "tuning"

// The units of a tuning key: a count, or seconds.
const (
	TuningCount   = "count"
	TuningSeconds = "seconds"
)

// TuningKey is one key of the registry: its default (the code's value before
// the key existed), floor, ceiling and unit, and the code that reads it.
type TuningKey struct {
	Name                  string
	Default, Min, Ceiling int
	Unit                  string
	ReadBy                string
}

// The keys, by name. A key's name ends in _s when it is in seconds.
const (
	TuneLeaseTTL             = "lease_ttl_s"              // store.LeaseTTL: a lease's life from its last heartbeat
	TuneAssessPerPass        = "assess_per_pass"          // store.AssessPerPass: assessments one pass asks for
	TuneFailuresToAlert      = "failures_to_alert"        // store.FailuresToAlert: a loop's failures in a row before an alert
	TuneThreadsPerDay        = "threads_per_day"          // store.ThreadsPerDay: threads registered in a day
	TuneRetryBackoff         = "retry_backoff_s"          // store retryBackoff: an executor's wait after its first failure
	TuneNudgeAfter           = "nudge_after_s"            // store.NudgeAfter: a decision's wait before the chief of staff is told
	TuneNudgesPerPass        = "nudges_per_pass"          // store nudgesPerPass: nudges one pass leaves
	TuneGateAttemptsEach     = "gate_attempts_each"       // store gateAttemptsEach: a gate batch's attempts per task
	TuneUnknownResetCooldown = "unknown_reset_cooldown_s" // store unknownResetCooldown: a refusal naming no reset cools its pool
)

// tuningKeys is the registry, sorted by name. A ceiling above the default
// is room a card may give; a floor keeps a value from stopping the work
// (a lease shorter than agentd's heartbeats, no assessment at all).
var tuningKeys = []TuningKey{
	{TuneAssessPerPass, 3, 1, 20, TuningCount, "controller.Assess"},
	{TuneFailuresToAlert, 4, 2, 100, TuningCount, "controller loop alerts"},
	{TuneGateAttemptsEach, 2, 1, 5, TuningCount, "store gate batches"},
	{TuneLeaseTTL, 120, 60, 900, TuningSeconds, "store claim, pinned claim and heartbeat"},
	{TuneNudgeAfter, 7200, 600, 86400, TuningSeconds, "store brief nudges"},
	{TuneNudgesPerPass, 3, 1, 20, TuningCount, "store brief nudges"},
	{TuneRetryBackoff, 30, 5, 600, TuningSeconds, "store retry of a failing executor"},
	{TuneThreadsPerDay, 200, 10, 2000, TuningCount, "store thread registration"},
	{TuneUnknownResetCooldown, 21600, 600, 172800, TuningSeconds, "store vendor pools"},
}

// TuningKeys is the registry, sorted by name.
func TuningKeys() []TuningKey { return slices.Clone(tuningKeys) }

func tuningKey(name string) (TuningKey, bool) {
	i := slices.IndexFunc(tuningKeys, func(k TuningKey) bool { return k.Name == name })
	if i < 0 {
		return TuningKey{}, false
	}
	return tuningKeys[i], true
}

// Tuning is a tuning rule's part: the values it states, by key. A nil
// *Tuning is no rule in force: every key has its default.
type Tuning struct {
	Values map[string]int
}

// Int is key's value: the rule's, or the default. A name the registry does
// not list is a programming error.
func (t *Tuning) Int(name string) int {
	k, ok := tuningKey(name)
	if !ok {
		panic("rules: no tuning key " + name)
	}
	if t != nil {
		if v, ok := t.Values[name]; ok {
			return v
		}
	}
	return k.Default
}

// Duration is a key in seconds as a duration.
func (t *Tuning) Duration(name string) time.Duration {
	if k, _ := tuningKey(name); k.Unit != TuningSeconds {
		panic("rules: tuning key " + name + " is not in seconds")
	}
	return time.Duration(t.Int(name)) * time.Second
}

// tuning reads values: a mapping of registry keys, each within its bounds.
func (r *Rule) tuning(o *object) error {
	t := &Tuning{Values: map[string]int{}}
	v, err := o.object("values")
	if err != nil {
		return err
	}
	for _, name := range v.order {
		k, ok := tuningKey(name)
		if !ok {
			names := make([]string, 0, len(tuningKeys))
			for _, k := range tuningKeys {
				names = append(names, k.Name)
			}
			return fmt.Errorf("%s: %q is no tuning key; the keys are %s", v.where, name, strings.Join(names, ", "))
		}
		if t.Values[name], err = v.integer(name, k.Min, k.Ceiling); err != nil {
			return err
		}
	}
	r.Tuning = t
	return v.done()
}

func (t *Tuning) doc() map[string]any {
	values := map[string]any{}
	for k, v := range t.Values {
		values[k] = v
	}
	return map[string]any{"values": values}
}
