package hostops

import (
	"math"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"time"
)

// These declarative vectors are shared by the future platform/executor/helper
// consumers. The same evaluator is deliberately the reference at all sites.
func TestSharedIntersectionVectors(t *testing.T) {
	var vectors []struct {
		Name     string `json:"name"`
		Layout   string `json:"layout"`
		Mutation string `json:"mutation"`
		Expected bool   `json:"expected"`
	}
	raw, e := os.ReadFile("testdata/intersection.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = Decode(raw, &vectors); e != nil {
		t.Fatal(e)
	}
	for _, site := range []string{"platform", "executor", "helper"} {
		for _, v := range vectors {
			t.Run(site+"/"+v.Name, func(t *testing.T) {
				a := testRule()
				b := cloneRule(a)
				b.ID = "strict-rule"
				b.Clauses[0].ID = "B"
				b.Clauses[0].MaxPerDay = 1
				rules := []Rule{a, b}
				if v.Layout == "within" {
					a.Clauses = append(a.Clauses, b.Clauses[0])
					rules = []Rule{a}
				}
				if v.Mutation == "disjoint" {
					if v.Layout == "within" {
						rules[0].Clauses[1].Hosts = []string{"host-b"}
					} else {
						rules[1].Clauses[0].Hosts = []string{"host-b"}
					}
				}
				tr, _, s, p := baseline(t, rules)
				ri := len(rules) - 1
				n := cloneRule(rules[ri])
				ci := len(n.Clauses) - 1
				n.Version++
				n.Direction = "tighten"
				switch v.Mutation {
				case "delete-clause", "disjoint":
					n.Clauses[ci].Tombstone = true
				case "delete-rule":
					n.Tombstone = true
				case "hosts":
					n.Clauses[ci].Hosts = []string{"host-b"}
				case "executors":
					n.Clauses[ci].ExecutorIDs = []string{"exec-b"}
				case "params":
					n.Clauses[ci].Params["service"] = []string{"other"}
				case "expired":
					n.ExpiresAt = "2026-10-26T11:59:00Z"
				case "disabled":
					n.Disabled = true
				case "missing":
					s.Baseline.Text = ""
					if _, err := tr.Load(s, p.head, p.version); err == nil {
						t.Fatal("missing baseline accepted")
					}
					return
				}
				_, next := tighten(t, tr, s, p, n)
				f := testFacts()
				f.History = []Event{event("succeeded")}
				x := testRequest()
				if got := next.Allow(x, testNow, testState(next), f); got.Allowed != v.Expected {
					t.Fatalf("%+v, want %v", got, v.Expected)
				}
			})
		}
	}
}

func TestLastClauseAndUncoveredDefaultDeny(t *testing.T) {
	r := testRule()
	tr, _, s, p := baseline(t, []Rule{r})
	r.Version++
	r.Direction = "tighten"
	r.Clauses[0].Tombstone = true
	_, n := tighten(t, tr, s, p, r)
	for _, host := range []string{"host-a", "uncovered"} {
		x := testRequest()
		x.Host = host
		if n.Allow(x, testNow, testState(n), testFacts()).Allowed {
			t.Fatal("deleted/uncovered allowed")
		}
	}
	r.Clauses[0].Tombstone = false
	r.Tombstone = true
	_, n = tighten(t, tr, s, p, r)
	if n.Allow(testRequest(), testNow, testState(n), testFacts()).Allowed {
		t.Fatal("last rule tombstone allowed")
	}
}

func TestVerifiedSnapshotOwnsTighteningMemory(t *testing.T) {
	r := testRule()
	tr, _, s, p := baseline(t, []Rule{r})
	r.Version++
	r.Direction = "tighten"
	r.Clauses[0].MaxPerDay = 0
	chain, next := tighten(t, tr, s, p, r)
	chain.Chain[0].Rule.Clauses[0].MaxPerDay = 100
	if next.Allow(testRequest(), testNow, testState(next), testFacts()).Allowed {
		t.Fatal("caller mutated a verified policy snapshot")
	}
}

func TestFieldDirections(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Rule)
		ok   bool
	}{
		{"lower-day", func(r *Rule) { r.Clauses[0].MaxPerDay = 0 }, true},
		{"lower-week", func(r *Rule) { r.Clauses[0].MaxPerWeek = 0 }, true},
		{"zero-concurrency", func(r *Rule) { r.Limits.GlobalWrites = 0; r.Limits.PerHostWrites = 0 }, true},
		{"cooldown-up", func(r *Rule) { r.Limits.CooldownS = 100 }, true},
		{"condition-add", func(r *Rule) { r.Clauses[0].Conditions = append(r.Clauses[0].Conditions, "config_unchanged") }, true},
		{"rule-executor-subset", func(r *Rule) { r.ExecutorIDs = []string{}; r.Clauses[0].ExecutorIDs = []string{} }, true},
		{"later-effective", func(r *Rule) { r.EffectiveAt = "2026-10-26T00:00:00Z" }, true},
		{"early-expiry", func(r *Rule) { r.ExpiresAt = "2026-10-27T00:00:00Z" }, true},
		{"mixed", func(r *Rule) { r.Limits.CooldownS = 100; r.Clauses[0].MaxPerDay = 0; r.Clauses[0].Hosts = []string{} }, true},
		{"drop-condition", func(r *Rule) { r.Clauses[0].Conditions = []string{} }, false},
		{"unknown-condition", func(r *Rule) { r.Clauses[0].Conditions = append(r.Clauses[0].Conditions, "shell_ok") }, false},
		{"timeout-even-shorter", func(r *Rule) { r.Clauses[0].TimeoutS = 1 }, false},
		{"watchdog", func(r *Rule) { r.Clauses[0].WatchdogS = 1 }, false},
		{"rollback", func(r *Rule) { r.Clauses[0].Rollback = "other" }, false},
		{"window", func(r *Rule) { r.Clauses[0].RepeatedSuccess.WindowS = 1 }, false},
		{"quota-id", func(r *Rule) { r.Clauses[0].QuotaID = "fresh" }, false},
		{"handler", func(r *Rule) { r.Clauses[0].HandlerDigest = Digest([]byte("new")) }, false},
		{"inventory", func(r *Rule) { r.InventoryDigest = Digest([]byte("new")) }, false},
		{"calendar", func(r *Rule) { r.Digest.Timezone = "UTC" }, false},
		{"reminders", func(r *Rule) { r.RemindersDays = []int{} }, false},
		{"delete-clause", func(r *Rule) { r.Clauses = []Clause{} }, false},
		{"delete-param", func(r *Rule) { delete(r.Clauses[0].Params, "service") }, false},
		{"new-host", func(r *Rule) { r.Clauses[0].Hosts = append(r.Clauses[0].Hosts, "host-c") }, false},
		{"day-up", func(r *Rule) { r.Clauses[0].MaxPerDay++ }, false},
		{"extend-expiry", func(r *Rule) { r.ExpiresAt = "2026-11-25T00:00:00Z" }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := testRule()
			n := cloneRule(p)
			n.Version++
			n.Direction = "tighten"
			c.edit(&n)
			if err := Tightens(p, n); (err == nil) != c.ok {
				t.Fatalf("%v (want allowed=%v)", err, c.ok)
			}
		})
	}
	p := testRule()
	p.Limits.CooldownS = 10
	n := cloneRule(p)
	n.Version++
	n.Direction = "tighten"
	n.Limits.CooldownS = 9
	if Tightens(p, n) == nil {
		t.Fatal("reverse cooldown accepted")
	}
	p.Tombstone = true
	n = cloneRule(p)
	n.Version++
	n.Direction = "tighten"
	n.Tombstone = false
	if Tightens(p, n) == nil {
		t.Fatal("tombstone removed")
	}
}

func TestCompletePolicySubsetProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(584))
	checks := 0
	allowedOld, allowedOne, allowedTwo := 0, 0, 0
	for layout := 0; layout < 2; layout++ {
		a := testRule()
		b := cloneRule(a)
		b.ID = "second"
		b.Clauses[0].ID = "B"
		b.Clauses[0].MaxPerDay = 2
		rules := []Rule{a, b}
		if layout == 0 {
			a.Clauses = append(a.Clauses, b.Clauses[0])
			rules = []Rule{a}
		}
		tr, _, s, p := baseline(t, rules)
		for iteration := 0; iteration < 120; iteration++ {
			ri := rng.Intn(len(rules))
			n := cloneRule(rules[ri])
			n.Version++
			n.Direction = "tighten"
			ci := rng.Intn(len(n.Clauses))
			n.Clauses[ci].MaxPerDay = rng.Intn(n.Clauses[ci].MaxPerDay + 1)
			n.Limits.CooldownS = rng.Intn(500)
			n.Clauses[ci].RepeatedSuccess.MaxCount = rng.Intn(4)
			if rng.Intn(2) == 0 {
				n.Clauses[ci].Hosts = []string{"host-b"}
			}
			if rng.Intn(3) == 0 {
				n.Clauses[ci].Tombstone = true
			}
			if rng.Intn(3) == 0 {
				n.Clauses[ci].Conditions = append(n.Clauses[ci].Conditions, "config_unchanged")
			}
			s1, p1 := tighten(t, tr, s, p, n)
			n2 := cloneRule(n)
			n2.Version++
			n2.Limits.CooldownS += 60
			n2.Clauses[ci].MaxPerWeek = rng.Intn(n2.Clauses[ci].MaxPerWeek + 1)
			_, p2 := tighten(t, tr, s1, p1, n2)
			for count := 0; count < 6; count++ {
				for _, host := range []string{"host-a", "host-b", "uncovered"} {
					for _, outcome := range []string{"succeeded", "failed", "unknown", "reserved"} {
						for stateCase := 0; stateCase < 5; stateCase++ {
							f := testFacts()
							for h := 0; h < count; h++ {
								ev := event(outcome)
								ev.ExecutionID = string(rune('a' + h))
								ev.At = testNow.Add(-time.Duration(60+h) * time.Second).Format(time.RFC3339)
								ev.SideEffectStarted = h%2 == 0
								f.History = append(f.History, ev)
							}
							if stateCase == 1 {
								f.LockAvailable = false
							}
							if stateCase == 2 {
								f.ActiveWrites = 1
							}
							if stateCase == 3 {
								f.Conditions["config_unchanged"] = false
							}
							x := testRequest()
							x.Host = host
							st0, st1, st2 := testState(p), testState(p1), testState(p2)
							if stateCase == 4 {
								st0.Frozen = true
								st1.Frozen = true
								st2.Frozen = true
							}
							old := p.Allow(x, testNow, st0, f).Allowed
							one := p1.Allow(x, testNow, st1, f).Allowed
							two := p2.Allow(x, testNow, st2, f).Allowed
							if one && !old || two && !one || two && !old {
								t.Fatal("authorization expanded with identical history/freeze/locks")
							}
							if old {
								allowedOld++
							}
							if one {
								allowedOne++
							}
							if two {
								allowedTwo++
							}
							checks++
						}
					}
				}
			}
		}
	}
	t.Logf("%d complete-state subset/transitivity comparisons", checks)
	if allowedOld == 0 || allowedOne == 0 || allowedTwo == 0 {
		t.Fatal("vacuous property: no positive authorizations")
	}
	t.Logf("positive authorizations: baseline=%d first=%d second=%d", allowedOld, allowedOne, allowedTwo)
}

func TestChainCASAndReplacement(t *testing.T) {
	r := testRule()
	tr, key, s, p := baseline(t, []Rule{r})
	r.Version++
	r.Direction = "tighten"
	r.Limits.CooldownS = 10
	s1, p1 := tighten(t, tr, s, p, r)
	if _, e := tr.Load(s, p1.head, p1.version); e == nil {
		t.Fatal("missing chain accepted")
	}
	bad := s1
	bad.Chain = append(append([]Tightening{}, s1.Chain...), s1.Chain[0])
	if _, e := tr.Load(bad, p1.head, p1.version); e == nil {
		t.Fatal("stale parent accepted")
	}
	b := p.bundle
	b.Meta.Version = p1.version + 1
	b.ReplacesDigest = p1.head
	b.ReplacesRules = p1.RuleDigests()
	b.Rules = []Rule{cloneRule(p1.rules[0])}
	b.Rules[0].Direction = "replace"
	b.Rules[0].Version++
	b.Rules[0].Limits.CooldownS = 0
	if _, e := tr.AdmitBaseline(signed(t, key, b, NamespacePolicy), p1, "test", testNow); e != nil {
		t.Fatal(e)
	}
	b.ReplacesDigest = p.head
	if _, e := tr.AdmitBaseline(signed(t, key, b, NamespacePolicy), p1, "test", testNow); e == nil {
		t.Fatal("replacement lost tightening chain")
	}
	b.ReplacesDigest = p1.head
	b.Rules[0].Clauses[0].QuotaID = "reset"
	if _, e := tr.AdmitBaseline(signed(t, key, b, NamespacePolicy), p1, "test", testNow); e == nil {
		t.Fatal("quota reset accepted")
	}
}

func TestCountsExpiryAndFacts(t *testing.T) {
	r := testRule()
	r.Clauses[0].MaxPerDay = 1
	_, _, _, p := baseline(t, []Rule{r})
	f := testFacts()
	x := testRequest()
	if !p.Allow(x, testNow, testState(p), f).Allowed {
		t.Fatal("positive control denied")
	}
	for _, outcome := range []string{"succeeded", "failed", "unknown", "reserved"} {
		f.History = []Event{event(outcome)}
		if p.Allow(x, testNow, testState(p), f).Allowed {
			t.Fatalf("%s did not consume quota", outcome)
		}
	}
	f.History = []Event{}
	old := canonical(f)
	_ = p.Allow(x, testNow, testState(p), f)
	if !reflect.DeepEqual(old, canonical(f)) {
		t.Fatal("evaluation mutated history")
	}
	x.Params["unknown"] = "ignored?"
	if p.Allow(x, testNow, testState(p), f).Allowed {
		t.Fatal("unknown parameter accepted")
	}
	x = testRequest()
	if p.Allow(x, testNow.Add(40*24*time.Hour), testState(p), f).Allowed {
		t.Fatal("expired allowed")
	}
	for _, field := range []string{"Fresh", "AuditDurable", "NotificationHealthy", "RollbackAvailable", "LockAvailable"} {
		f = testFacts()
		reflect.ValueOf(&f).Elem().FieldByName(field).SetBool(false)
		if p.Allow(x, testNow, testState(p), f).Allowed {
			t.Fatal(field)
		}
	}
}

func TestHugeCooldownCannotOverflowIntoPermission(t *testing.T) {
	r := testRule()
	tr, _, s, p := baseline(t, []Rule{r})
	r.Version++
	r.Direction = "tighten"
	r.Limits.CooldownS = math.MaxInt
	_, next := tighten(t, tr, s, p, r)
	f := testFacts()
	f.History = []Event{event("failed")}
	if next.Allow(testRequest(), testNow, testState(next), f).Allowed {
		t.Fatal("duration overflow broadened cooldown")
	}
}

func TestUTCQuotasAggregationAndRepeatedSuccess(t *testing.T) {
	r := testRule()
	r.Clauses[0].MaxPerWeek = 1
	_, _, _, p := baseline(t, []Rule{r})
	f := testFacts()
	ev := event("unknown")
	ev.Host = "host-b"
	f.History = []Event{ev}
	if p.Allow(testRequest(), testNow, testState(p), f).Allowed {
		t.Fatal("quota split across hosts")
	}
	// The same instant expressed in another zone belongs to the UTC day/week.
	ev.Host = "host-a"
	ev.At = "2026-10-26T00:00:01+09:00"
	f.History = []Event{ev}
	if !p.Allow(testRequest(), testNow, testState(p), f).Allowed {
		t.Fatal("previous UTC week counted in new week")
	}
	r.Clauses[0].MaxPerDay = 1
	r.Clauses[0].MaxPerWeek = 12
	_, _, _, p = baseline(t, []Rule{r})
	ev.At = "2026-10-27T00:00:01+14:00"
	f.History = []Event{ev}
	if p.Allow(testRequest(), testNow, testState(p), f).Allowed {
		t.Fatal("current UTC day escaped daily quota")
	}
	r.Clauses[0].MaxPerDay = 10
	r.Clauses[0].RepeatedSuccess.MaxCount = 1
	_, _, _, p = baseline(t, []Rule{r})
	ev = event("succeeded")
	ev.QuotaIDs = []string{"old-clause-name"}
	f.History = []Event{ev}
	if p.Allow(testRequest(), testNow, testState(p), f).Allowed {
		t.Fatal("success circuit reset by quota rename")
	}
}
