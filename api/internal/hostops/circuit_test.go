package hostops

import (
	"reflect"
	"testing"
	"time"
)

func TestCircuitDurableLifecycle(t *testing.T) {
	r := testRule()
	r.Clauses[0].MaxPerDay, r.Clauses[0].MaxPerWeek = 100, 100
	r.Clauses[0].RepeatedSuccess.MaxCount = 2
	tr, key, _, p := baseline(t, []Rule{r})
	state := testState(p)
	f := testFacts()
	first := event("succeeded")
	second := first
	second.ExecutionID = "second"
	second.At = testNow.Add(-30 * time.Second).Format(time.RFC3339)
	f.History = []Event{second, first} // input ordering must not affect detection
	x := testRequest()
	decision := p.Allow(x, testNow, state, f)
	if decision.Allowed || decision.Circuit == nil || !decision.Circuit.Open {
		t.Fatalf("no latch proposal: %+v", decision)
	}
	latched, err := p.ObserveSuccesses(x, testNow, state, f)
	if err != nil || len(latched.Circuits) != 1 || !latched.Circuits[0].Open || len(state.Circuits) != 0 {
		t.Fatalf("latch/ownership: %+v %v", latched, err)
	}
	// Restart round-trip, elapsed window, removed output/history, rule rename,
	// signed version/threshold increase and ordinary thaw all preserve the latch.
	var restarted SafetyState
	if err := Decode(canonical(latched), &restarted); err != nil {
		t.Fatal(err)
	}
	later := testNow.Add(2 * time.Hour)
	laterFacts := testFacts()
	laterFacts.ClockSyncedAt = later
	if p.Allow(x, later, restarted, laterFacts).Allowed {
		t.Fatal("restart/compaction/time cleared latch")
	}
	renamed := cloneRule(r)
	renamed.ID = "renamed"
	renamed.Version = 2
	renamed.Direction = "replace"
	renamed.Clauses[0].ID = "renamed-clause"
	renamed.Clauses[0].RepeatedSuccess.MaxCount = 100
	b := PolicyBundle{metadata("test-policy"), p.Head(), p.RuleDigests(), []Rule{renamed}}
	b.Meta.Version = 2
	replacement, err := tr.AdmitBaseline(signed(t, key, b, NamespacePolicy), p, "test", later)
	if err != nil {
		t.Fatal(err)
	}
	upgraded := restarted
	upgraded.PolicyHead = replacement.Head()
	upgraded.MaxRuleVersion = replacement.Version()
	if replacement.Allow(x, later, upgraded, laterFacts).Allowed {
		t.Fatal("signed replacement/rename cleared latch")
	}
	frozen, _ := latched.Freeze(true, true)
	thaw := Unfreeze{1, "test", frozen.Epoch, frozen.Generation, p.Version(), p.Head(), "ordinary thaw", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}
	thawed, err := tr.Unfreeze(frozen, signed(t, key, thaw, NamespaceUnfreeze), p, testNow, f.clock())
	if err != nil || p.Allow(x, testNow, thawed, f).Allowed {
		t.Fatal("ordinary thaw cleared circuit", err)
	}
	other := x
	other.Host = "host-b"
	if !p.Allow(other, later, restarted, laterFacts).Allowed {
		t.Fatal("unrelated host blocked")
	}

	release := CircuitRelease{1, "test", x.Host, x.Action, Digest(canonical(latched.Circuits[0])), Digest(canonical(f.History)), latched.Epoch, latched.Generation, p.Version(), p.Head(), "investigated repeated successes", "2026-10-26T11:00:00Z", "2026-10-26T13:00:00Z"}
	for _, tc := range []struct {
		name string
		edit func(*CircuitRelease)
	}{
		{"environment", func(u *CircuitRelease) { u.Environment = "prod" }},
		{"host", func(u *CircuitRelease) { u.Host = "host-b" }},
		{"action", func(u *CircuitRelease) { u.Action = "host_service_state" }},
		{"trip", func(u *CircuitRelease) { u.CircuitDigest = testDigest }},
		{"history", func(u *CircuitRelease) { u.HistoryDigest = testDigest }},
		{"epoch", func(u *CircuitRelease) { u.Epoch++ }},
		{"generation", func(u *CircuitRelease) { u.Generation++ }},
		{"version", func(u *CircuitRelease) { u.RuleVersion++ }},
		{"head", func(u *CircuitRelease) { u.PolicyHead = testDigest }},
		{"reason", func(u *CircuitRelease) { u.Reason = "" }},
		{"expired", func(u *CircuitRelease) { u.ExpiresAt = "2026-10-26T12:00:00Z" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := release
			tc.edit(&u)
			got, err := tr.ReleaseCircuit(latched, signed(t, key, u, NamespaceCircuitRelease), f, testNow)
			if err == nil || !reflect.DeepEqual(got, latched) {
				t.Fatal("bad release changed state")
			}
		})
	}
	sig := signed(t, key, release, NamespaceCircuitRelease)
	for _, tc := range []struct {
		name string
		edit func(*SafetyState, *Facts)
	}{
		{"missing-ledger", func(s *SafetyState, f *Facts) { s.Circuits = nil }},
		{"freeze", func(s *SafetyState, f *Facts) { s.Frozen = true }},
		{"stale", func(s *SafetyState, f *Facts) { s.Fresh = false }},
		{"clock-unknown", func(s *SafetyState, f *Facts) { f.ClockSyncedAt = time.Time{} }},
		{"clock-stale", func(s *SafetyState, f *Facts) { f.ClockSyncedAt = testNow.Add(-6 * time.Minute) }},
		{"clock-skew", func(s *SafetyState, f *Facts) { f.ClockOffset = 6 * time.Second }},
		{"clock-rollback", func(s *SafetyState, f *Facts) { f.ClockRolledBack = true }},
		{"active", func(s *SafetyState, f *Facts) { f.HostActiveWrites = 1 }},
		{"lock", func(s *SafetyState, f *Facts) { f.LockAvailable = false }},
		{"audit", func(s *SafetyState, f *Facts) { f.AuditDurable = false }},
		{"new-history", func(s *SafetyState, f *Facts) {
			e := event("unknown")
			e.ExecutionID = "unknown"
			f.History = append(append([]Event{}, f.History...), e)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, g := latched, f
			tc.edit(&s, &g)
			if _, err := tr.ReleaseCircuit(s, sig, g, testNow); err == nil {
				t.Fatal("unsafe release")
			}
		})
	}
	unresolved := f
	pending := event("unknown")
	pending.ExecutionID = "pending"
	unresolved.History = append(append([]Event{}, f.History...), pending)
	u := release
	u.HistoryDigest = Digest(canonical(unresolved.History))
	if _, err := tr.ReleaseCircuit(latched, signed(t, key, u, NamespaceCircuitRelease), unresolved, testNow); err == nil {
		t.Fatal("signed release resolved unknown")
	}
	if _, err := tr.ReleaseCircuit(latched, signed(t, key, release, NamespaceUnfreeze), f, testNow); err == nil {
		t.Fatal("wrong signature domain")
	}
	cleared, err := tr.ReleaseCircuit(latched, sig, f, testNow)
	if err != nil || cleared.Circuits[0].Open || !p.Allow(x, testNow, cleared, f).Allowed {
		t.Fatal("valid release", err)
	}
	if !latched.Circuits[0].Open || len(latched.Circuits[0].ClearedExecutions) != 0 || len(f.History) != 2 {
		t.Fatal("release mutated prior state/history")
	}
	if _, err := tr.ReleaseCircuit(cleared, sig, f, testNow); err == nil {
		t.Fatal("release replay")
	}
	// A late success with an old timestamp is NOT covered by the signed history;
	// exact execution tombstones, rather than a time cutoff, keep it countable.
	third := first
	third.ExecutionID = "late-third"
	f.History = append(f.History, third)
	if !p.Allow(x, testNow, cleared, f).Allowed {
		t.Fatal("one new success should remain below threshold")
	}
	fourth := second
	fourth.ExecutionID = "fourth"
	f.History = append(f.History, fourth)
	retripped, err := p.ObserveSuccesses(x, testNow, cleared, f)
	if err != nil || !retripped.Circuits[0].Open || retripped.Circuits[0].Trip != 2 || p.Allow(x, testNow, retripped, f).Allowed {
		t.Fatal("failed to retrip", err)
	}
	if _, err := tr.ReleaseCircuit(retripped, sig, f, testNow); err == nil {
		t.Fatal("old release cleared new trip")
	}
}

func TestCircuitHistoricalWindowsAndMissingState(t *testing.T) {
	r := testRule()
	r.Clauses[0].MaxPerDay = 100
	r.Clauses[0].MaxPerWeek = 100
	r.Clauses[0].RepeatedSuccess = RepeatedSuccess{3600, 2}
	_, _, _, p := baseline(t, []Rule{r})
	s := testState(p)
	f := testFacts()
	x := testRequest()
	first := event("succeeded")
	first.At = testNow.Add(-4 * time.Hour).Format(time.RFC3339)
	second := first
	second.ExecutionID = "second"
	second.At = testNow.Add(-3 * time.Hour).Format(time.RFC3339)
	f.History = []Event{first, second}
	if !p.Allow(x, testNow, s, f).Allowed {
		t.Fatal("separate windows falsely tripped")
	}
	second.At = testNow.Add(-3*time.Hour - time.Second).Format(time.RFC3339)
	f.History[1] = second
	if p.Allow(x, testNow, s, f).Allowed {
		t.Fatal("historical threshold escaped after window elapsed")
	}
	s.Circuits = nil
	if p.Allow(x, testNow, s, testFacts()).Allowed {
		t.Fatal("missing ledger allowed")
	}
	s = testState(p)
	f = testFacts()
	f.History = nil
	if p.Allow(x, testNow, s, f).Allowed {
		t.Fatal("missing history allowed")
	}
}
