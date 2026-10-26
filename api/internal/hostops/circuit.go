package hostops

import (
	"errors"
	"sort"
	"time"
)

const NamespaceCircuitRelease = "bifrost-host-ops-circuit-release"

// Circuit is durable authorization state, keyed by environment (SafetyState),
// host and action, never rule name/version/executor/service alias. Cleared IDs
// are retained tombstones: only an Owner release excludes those successes.
type Circuit struct {
	Host              string   `json:"host"`
	Action            string   `json:"action"`
	Trip              uint64   `json:"trip"`
	Open              bool     `json:"open"`
	EvidenceDigest    string   `json:"evidence_digest"`
	ClearedExecutions []string `json:"cleared_executions"`
}

func validateCircuits(records []Circuit) error {
	if records == nil {
		return errors.New("missing circuit ledger; recovery required")
	}
	seen := map[[2]string]bool{}
	for _, c := range records {
		key := [2]string{c.Host, c.Action}
		if c.Host == "" || !knownM(c.Action) || seen[key] || c.Trip == 0 || !validDigest(c.EvidenceDigest) || c.ClearedExecutions == nil || !setOK(c.ClearedExecutions, true) {
			return errors.New("invalid circuit ledger")
		}
		seen[key] = true
	}
	return nil
}

func validateHistory(events []Event, now time.Time) error {
	if events == nil {
		return errors.New("missing history")
	}
	seen := map[string]bool{}
	for _, e := range events {
		at, err := time.Parse(time.RFC3339, e.At)
		if err != nil || at.After(now) || e.ExecutionID == "" || seen[e.ExecutionID] || e.Host == "" || !knownM(e.Action) || !setOK(e.QuotaIDs, false) || !contains([]string{"reserved", "running", "succeeded", "failed", "unknown"}, e.Outcome) {
			return errors.New("history incomplete or invalid")
		}
		seen[e.ExecutionID] = true
	}
	return nil
}

// ObserveSuccesses proposes the complete next safety snapshot. S2/S3 must run
// this in the successful-result transaction for every executed request, under
// the policy/state/history CAS, before acknowledging the result or scheduling
// again. Allow also invokes it so elapsed time cannot hide an uncommitted trip.
// Existing latches survive policy replacement, history compaction and restart.
func (p *Policy) ObserveSuccesses(x Request, now time.Time, s SafetyState, f Facts) (SafetyState, error) {
	if p == nil || s.integrity(x.Environment, p.version, p.head) != nil || !f.Fresh || !f.AuditDurable || f.clock().Check(now) != nil || validateHistory(f.History, now) != nil {
		return s, errors.New("circuit observation requires intact state, history and clock")
	}
	idx := -1
	c := Circuit{Host: x.Host, Action: x.Action, ClearedExecutions: []string{}}
	for i, record := range s.Circuits {
		if record.Host == x.Host && record.Action == x.Action {
			idx, c = i, record
		}
	}
	if c.Open {
		return s, nil
	}
	events := []Event{}
	for _, e := range f.History {
		if e.Host == x.Host && e.Action == x.Action && e.Outcome == "succeeded" && !contains(c.ClearedExecutions, e.ExecutionID) {
			events = append(events, e)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339, events[i].At)
		b, _ := time.Parse(time.RFC3339, events[j].At)
		if a.Equal(b) {
			return events[i].ExecutionID < events[j].ExecutionID
		}
		return a.Before(b)
	})
	for ri, baseline := range p.base {
		for ci, bc := range baseline.Clauses {
			if !domain(baseline, bc, x) {
				continue
			}
			limit := p.rules[ri].Clauses[ci].RepeatedSuccess
			tripped := limit.MaxCount == 0
			left := 0
			for right, e := range events {
				end, _ := time.Parse(time.RFC3339, e.At)
				for left < right {
					start, _ := time.Parse(time.RFC3339, events[left].At)
					if end.Sub(start).Seconds() < float64(limit.WindowS) {
						break
					}
					left++
				}
				if right-left+1 >= limit.MaxCount {
					tripped = true
					break
				}
			}
			if !tripped {
				continue
			}
			if c.Trip == ^uint64(0) {
				return s, errors.New("circuit generation exhausted; recovery required")
			}
			c.Trip++
			c.Open = true
			c.EvidenceDigest = Digest(canonical(struct {
				Head   string
				Rule   Rule
				Events []Event
			}{p.head, p.rules[ri], events}))
			n := s
			n.Circuits = append([]Circuit{}, s.Circuits...)
			c.ClearedExecutions = append([]string{}, c.ClearedExecutions...)
			if idx < 0 {
				n.Circuits = append(n.Circuits, c)
			} else {
				n.Circuits[idx] = c
			}
			return n, nil
		}
	}
	return s, nil
}

// CircuitRelease is a scoped Owner acknowledgement of one exact trip and
// history snapshot. It is not an unfreeze, policy renewal or unknown resolver.
type CircuitRelease struct {
	SchemaVersion int    `json:"schema_version"`
	Environment   string `json:"environment"`
	Host          string `json:"host"`
	Action        string `json:"action"`
	CircuitDigest string `json:"circuit_digest"`
	HistoryDigest string `json:"history_digest"`
	Epoch         uint64 `json:"epoch"`
	Generation    uint64 `json:"generation"`
	RuleVersion   uint64 `json:"rule_version"`
	PolicyHead    string `json:"policy_head"`
	Reason        string `json:"reason"`
	EffectiveAt   string `json:"effective_at"`
	ExpiresAt     string `json:"expires_at"`
}

func (t Trust) ReleaseCircuit(s SafetyState, signed Signed, f Facts, now time.Time) (SafetyState, error) {
	if s.Check(s.Environment, s.MaxRuleVersion, s.PolicyHead) != nil || !f.Fresh || !f.AuditDurable || !f.LockAvailable || f.HostActiveWrites != 0 || f.clock().Check(now) != nil || validateHistory(f.History, now) != nil {
		return s, errors.New("circuit release requires healthy, quiescent state")
	}
	if err := t.verify(signed, NamespaceCircuitRelease); err != nil {
		return s, err
	}
	var u CircuitRelease
	if err := Decode([]byte(signed.Text), &u); err != nil {
		return s, err
	}
	if u.SchemaVersion != 1 || u.Environment != s.Environment || u.Epoch != s.Epoch || u.Generation != s.Generation || u.RuleVersion != s.MaxRuleVersion || u.PolicyHead != s.PolicyHead || u.Reason == "" || u.HistoryDigest != Digest(canonical(f.History)) || !current(u.EffectiveAt, u.ExpiresAt, now) {
		return s, errors.New("invalid or stale circuit release")
	}
	for i, c := range s.Circuits {
		if c.Host != u.Host || c.Action != u.Action {
			continue
		}
		if !c.Open || u.CircuitDigest != Digest(canonical(c)) {
			return s, errors.New("circuit release does not bind current trip")
		}
		c.ClearedExecutions = append([]string{}, c.ClearedExecutions...)
		for _, e := range f.History {
			if e.Host != c.Host || e.Action != c.Action {
				continue
			}
			if contains([]string{"reserved", "running", "unknown"}, e.Outcome) {
				return s, errors.New("circuit target has unsettled execution")
			}
			if e.Outcome == "succeeded" && !contains(c.ClearedExecutions, e.ExecutionID) {
				c.ClearedExecutions = append(c.ClearedExecutions, e.ExecutionID)
			}
		}
		sort.Strings(c.ClearedExecutions)
		c.Open = false
		n := s
		n.Circuits = append([]Circuit{}, s.Circuits...)
		n.Circuits[i] = c
		return n, nil
	}
	return s, errors.New("circuit target not found")
}
