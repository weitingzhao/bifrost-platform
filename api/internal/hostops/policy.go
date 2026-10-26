package hostops

import (
	"errors"
	"fmt"
	"reflect"
	"time"
	"unicode/utf8"
)

type Limits struct {
	GlobalWrites     int `json:"global_writes"`
	PerHostWrites    int `json:"per_host_writes"`
	CooldownS        int `json:"cooldown_s"`
	RecoveryAttempts int `json:"max_automatic_recovery_attempts"`
}
type RepeatedSuccess struct {
	WindowS  int `json:"window_s"`
	MaxCount int `json:"max_count"`
}
type Calendar struct {
	Timezone  string `json:"timezone"`
	Weekday   string `json:"weekday"`
	LocalTime string `json:"local_time"`
}
type Clause struct {
	ID              string              `json:"clause_id"`
	QuotaID         string              `json:"quota_id"`
	Action          string              `json:"action"`
	Hosts           []string            `json:"hosts"`
	ExecutorIDs     []string            `json:"executor_ids"`
	Params          map[string][]string `json:"params"`
	HandlerDigest   string              `json:"handler_digest"`
	BundleDigests   []string            `json:"bundle_digests"`
	Conditions      []string            `json:"conditions"`
	TimeoutS        int                 `json:"timeout_s"`
	WatchdogS       int                 `json:"watchdog_s"`
	Rollback        string              `json:"rollback"`
	MaxPerDay       int                 `json:"max_per_day"`
	MaxPerWeek      int                 `json:"max_per_week"`
	RepeatedSuccess RepeatedSuccess     `json:"repeated_success"`
	Disabled        bool                `json:"disabled"`
	Tombstone       bool                `json:"tombstone"`
}
type Rule struct {
	ID              string   `json:"rule_id"`
	Version         uint64   `json:"rule_version"`
	Direction       string   `json:"direction"`
	SchemaVersion   int      `json:"schema_version"`
	Environment     string   `json:"environment"`
	EffectiveAt     string   `json:"effective_at"`
	ExpiresAt       string   `json:"expires_at"`
	InventoryDigest string   `json:"inventory_digest"`
	CatalogDigest   string   `json:"catalog_digest"`
	ExecutorIDs     []string `json:"executor_ids"`
	Clauses         []Clause `json:"clauses"`
	Limits          Limits   `json:"limits"`
	RemindersDays   []int    `json:"reminders_days"`
	Digest          Calendar `json:"digest"`
	Disabled        bool     `json:"disabled"`
	Tombstone       bool     `json:"tombstone"`
}

var conditionNames = []string{"identity_matches", "config_unchanged", "no_conflicting_run", "dependencies_healthy", "maintenance_window", "rollback_available", "audit_durable", "notification_healthy", "clock_healthy", "lifecycle_new", "stateless"}

func (r Rule) validate() error {
	if r.SchemaVersion != 1 || r.ID == "" || r.Version == 0 || !contains([]string{"initial", "replace", "tighten"}, r.Direction) || !contains([]string{"test", "dev", "stg", "prod"}, r.Environment) || !validDigest(r.InventoryDigest) || !validDigest(r.CatalogDigest) || !setOK(r.ExecutorIDs, true) || len(r.Clauses) == 0 {
		return errors.New("invalid rule header")
	}
	if _, _, e := times(r.EffectiveAt, r.ExpiresAt); e != nil {
		return e
	}
	if r.Limits.GlobalWrites < 0 || r.Limits.GlobalWrites > 1 || r.Limits.PerHostWrites < 0 || r.Limits.PerHostWrites > 1 || r.Limits.CooldownS < 0 || r.Limits.RecoveryAttempts < 0 || r.Limits.RecoveryAttempts > 1 {
		return errors.New("invalid limits")
	}
	if !reflect.DeepEqual(r.RemindersDays, []int{14, 3, 1}) || r.Digest.Timezone != "America/Chicago" || r.Digest.Weekday != "monday" || r.Digest.LocalTime != "09:00" {
		return errors.New("unsupported reminder/calendar contract")
	}
	ids := map[string]bool{}
	for _, c := range r.Clauses {
		if !validParams(c.Action, c.Params) {
			return errors.New("unknown or missing action parameter")
		}
		if c.ID == "" || ids[c.ID] || c.QuotaID == "" || !knownM(c.Action) || !setOK(c.Hosts, true) || !setOK(c.ExecutorIDs, true) || !subset(c.ExecutorIDs, r.ExecutorIDs) || !validDigest(c.HandlerDigest) || !setOK(c.BundleDigests, true) || !setOK(c.Conditions, true) || !subset(c.Conditions, conditionNames) || c.TimeoutS < 1 || c.WatchdogS < 0 || c.Rollback == "" || c.MaxPerDay < 0 || c.MaxPerWeek < 0 || c.RepeatedSuccess.WindowS < 1 || c.RepeatedSuccess.MaxCount < 0 {
			return errors.New("invalid clause")
		}
		ids[c.ID] = true
		for _, d := range c.BundleDigests {
			if !validDigest(d) {
				return errors.New("invalid bundle digest")
			}
		}
		for k, v := range c.Params {
			if k == "" || !setOK(v, true) {
				return errors.New("invalid parameter set")
			}
		}
	}
	return nil
}

// PolicyBundle signs the COMPLETE baseline set, not just independently
// downloadable rules. Replacements explicitly bind the prior full chain head.
type PolicyBundle struct {
	Meta           Metadata          `json:"meta"`
	ReplacesDigest string            `json:"replaces_digest"`
	ReplacesRules  map[string]string `json:"replaces_rules"`
	Rules          []Rule            `json:"rules"`
}
type Tightening struct {
	ParentDigest string `json:"parent_digest"`
	RuleDigest   string `json:"rule_digest"`
	Version      uint64 `json:"version"`
	Rule         Rule   `json:"rule"`
}
type PolicySet struct {
	Baseline Signed       `json:"baseline"`
	Chain    []Tightening `json:"chain"`
}

// Policy is an immutable verified snapshot. The original baseline remains
// private and is ALWAYS used for domain matching, including after deletion.
type Policy struct {
	bundle  PolicyBundle
	base    []Rule
	rules   []Rule
	head    string
	version uint64
}

func (p *Policy) Head() string    { return p.head }
func (p *Policy) Version() uint64 { return p.version }
func (p *Policy) RuleDigests() map[string]string {
	out := map[string]string{}
	for _, r := range p.rules {
		out[r.ID] = Digest(canonical(r))
	}
	return out
}

// Load verifies all bytes and every parent link against a persisted head. It
// never silently evaluates a partial set or falls back to an older baseline.
func (t Trust) Load(s PolicySet, expectedHead string, minVersion uint64) (*Policy, error) {
	// Verify the supplied bytes before JSON can normalize malformed UTF-8.
	if err := t.verify(s.Baseline, NamespacePolicy); err != nil {
		return nil, err
	}
	if !utf8.ValidString(s.Baseline.Text) {
		return nil, errors.New("policy text must be UTF-8")
	}
	// Own the complete chain: callers must not be able to modify slices/maps
	// after verification and change the supposedly immutable effective rules.
	var owned PolicySet
	if err := Decode(canonical(s), &owned); err != nil {
		return nil, err
	}
	s = owned
	var b PolicyBundle
	if err := Decode([]byte(s.Baseline.Text), &b); err != nil {
		return nil, err
	}
	if err := b.Meta.validate(); err != nil {
		return nil, err
	}
	if len(b.Rules) == 0 {
		return nil, errors.New("empty policy bundle")
	}
	p := &Policy{bundle: b, base: b.Rules, rules: append([]Rule(nil), b.Rules...), head: Digest([]byte(s.Baseline.Text)), version: b.Meta.Version}
	ids := map[string]bool{}
	for _, r := range b.Rules {
		if err := r.validate(); err != nil {
			return nil, err
		}
		if ids[r.ID] || r.Environment != b.Meta.Environment || r.Direction == "tighten" {
			return nil, errors.New("invalid baseline rule identity")
		}
		ids[r.ID] = true
	}
	for _, delta := range s.Chain {
		if delta.ParentDigest != p.head || delta.Version <= p.version {
			return nil, errors.New("missing, stale or replayed tightening")
		}
		idx := -1
		for i, r := range p.rules {
			if r.ID == delta.Rule.ID {
				idx = i
			}
		}
		if idx < 0 || Digest(canonical(p.rules[idx])) != delta.RuleDigest {
			return nil, errors.New("rule parent mismatch")
		}
		if err := Tightens(p.rules[idx], delta.Rule); err != nil {
			return nil, err
		}
		p.rules[idx] = delta.Rule
		p.head = Digest(canonical(delta))
		p.version = delta.Version
	}
	if p.head != expectedHead || p.version < minVersion {
		return nil, errors.New("incomplete policy set or version rollback")
	}
	return p, nil
}

// AdmitBaseline requires an explicit replacement of the entire current set;
// the caller atomically persists returned bytes/head with existing history.
func (t Trust) AdmitBaseline(s Signed, old *Policy, environment string, now time.Time) (*Policy, error) {
	p, err := t.Load(PolicySet{s, []Tightening{}}, Digest([]byte(s.Text)), 0)
	if err != nil {
		return nil, err
	}
	if p.bundle.Meta.Environment != environment || !current(p.bundle.Meta.EffectiveAt, p.bundle.Meta.ExpiresAt, now) {
		return nil, errors.New("baseline outside validity")
	}
	if old == nil {
		if p.bundle.ReplacesDigest != "" || len(p.bundle.ReplacesRules) != 0 {
			return nil, errors.New("initial baseline has replacement")
		}
		return p, nil
	}
	if p.version <= old.version || p.bundle.Meta.ID != old.bundle.Meta.ID || p.bundle.Meta.Environment != old.bundle.Meta.Environment || p.bundle.ReplacesDigest != old.head || !reflect.DeepEqual(p.bundle.ReplacesRules, old.RuleDigests()) {
		return nil, errors.New("replacement must bind complete previous policy and chain")
	}
	for _, r := range p.rules {
		for _, prev := range old.rules {
			if r.ID == prev.ID && r.Version <= prev.Version {
				return nil, errors.New("rule version rollback")
			}
		}
	}
	// Quota identities are stable even across rename/replacement. New targets
	// may have new IDs; overlapping target/action domains keep all old quotas.
	for _, prev := range old.base {
		for _, pc := range prev.Clauses {
			for _, r := range p.base {
				for _, c := range r.Clauses {
					if pc.Action == c.Action && intersects(pc.Hosts, c.Hosts) && pc.QuotaID != c.QuotaID {
						return nil, errors.New("overlapping target cannot reset quota identity")
					}
				}
			}
		}
	}
	return p, nil
}
func intersects(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}

// Tightens compares every field, never the claimed direction. Exact-equality
// projection makes future fields default to requiring a new Owner signature.
func Tightens(prev, next Rule) error {
	if err := prev.validate(); err != nil {
		return err
	}
	if err := next.validate(); err != nil {
		return err
	}
	if next.Version <= prev.Version || next.Direction != "tighten" || len(next.Clauses) != len(prev.Clauses) {
		return errors.New("tightening must preserve clause identities and increase version")
	}
	pa, pb, _ := times(prev.EffectiveAt, prev.ExpiresAt)
	na, nb, _ := times(next.EffectiveAt, next.ExpiresAt)
	if na.Before(pa) || nb.After(pb) || !subset(next.ExecutorIDs, prev.ExecutorIDs) || prev.Disabled && !next.Disabled || prev.Tombstone && !next.Tombstone {
		return errors.New("rule broadens")
	}
	pl, nl := prev.Limits, next.Limits
	if nl.GlobalWrites > pl.GlobalWrites || nl.PerHostWrites > pl.PerHostWrites || nl.RecoveryAttempts > pl.RecoveryAttempts || nl.CooldownS < pl.CooldownS {
		return errors.New("limit broadens")
	}
	projection := next
	projection.Version = prev.Version
	projection.Direction = prev.Direction
	projection.EffectiveAt = prev.EffectiveAt
	projection.ExpiresAt = prev.ExpiresAt
	projection.ExecutorIDs = prev.ExecutorIDs
	projection.Disabled = prev.Disabled
	projection.Tombstone = prev.Tombstone
	projection.Limits = prev.Limits
	projection.Clauses = prev.Clauses
	if !reflect.DeepEqual(projection, prev) {
		return errors.New("incomparable rule field changed")
	}
	for i, n := range next.Clauses {
		p := prev.Clauses[i]
		if n.ID != p.ID || !subset(n.Hosts, p.Hosts) || !subset(n.ExecutorIDs, p.ExecutorIDs) || !subset(p.Conditions, n.Conditions) || n.MaxPerDay > p.MaxPerDay || n.MaxPerWeek > p.MaxPerWeek || n.RepeatedSuccess.MaxCount > p.RepeatedSuccess.MaxCount || p.Disabled && !n.Disabled || p.Tombstone && !n.Tombstone || len(n.Params) != len(p.Params) {
			return errors.New("clause broadens or loses identity")
		}
		for k, v := range p.Params {
			nv, ok := n.Params[k]
			if !ok || !subset(nv, v) {
				return errors.New("parameter predicate removed or broadened")
			}
		}
		q := n
		q.Hosts = p.Hosts
		q.ExecutorIDs = p.ExecutorIDs
		q.Params = p.Params
		q.Conditions = p.Conditions
		q.MaxPerDay = p.MaxPerDay
		q.MaxPerWeek = p.MaxPerWeek
		q.RepeatedSuccess.MaxCount = p.RepeatedSuccess.MaxCount
		q.Disabled = p.Disabled
		q.Tombstone = p.Tombstone
		if !reflect.DeepEqual(q, p) {
			return fmt.Errorf("incomparable clause field changed: %s", p.ID)
		}
	}
	return nil
}
