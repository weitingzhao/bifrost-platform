package slots_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/slots"
)

func rule(t *testing.T, body string) rules.Rule {
	t.Helper()
	r, err := rules.Parse([]byte("rule_id: concurrency\nrule_version: 1\ndirection: initial\n" +
		"effective_at: 2026-11-01T00:00:00Z\nexpires_at: 2027-01-01T00:00:00Z\n" + body))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The registry holds one policy for each name a rule may give, and no other.
func TestRegistryIsTheRulesPolicies(t *testing.T) {
	want := slices.Clone(rules.SlotPolicies)
	slices.Sort(want)
	if got := slots.Names(); !slices.Equal(got, want) {
		t.Fatalf("registry %v, rules %v", got, want)
	}
	for _, n := range want {
		if _, err := slots.Lookup(n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := slots.Lookup("pid"); err == nil {
		t.Fatal("an unknown policy was found")
	}
}

// A rule without the block is fixed at per_host and not elastic; with one,
// its policy and parameters, max per_host under the ceiling.
func TestConfigOf(t *testing.T) {
	c := slots.ConfigOf(rule(t, "per_host: 2\n"))
	if c.Elastic || c.Policy != "fixed" || c.Params.Min != 2 || c.Params.Start != 2 || c.Params.Max != 2 {
		t.Fatalf("no block: %+v", c)
	}
	c = slots.ConfigOf(rule(t, "per_host: 3\nslots: {policy: aimd, min: 1, start: 2, step: 1, interval_s: 120}\n"))
	if !c.Elastic || c.Policy != "aimd" || c.Params.Min != 1 || c.Params.Start != 2 || c.Params.Max != 3 || c.Params.IntervalS != 120 {
		t.Fatalf("aimd: %+v", c)
	}
	if slots.Ceiling != rules.PerHostCeiling {
		t.Fatalf("ceiling %d", slots.Ceiling)
	}
}

func TestClamp(t *testing.T) {
	for _, c := range []struct{ n, lo, hi, want int }{
		{0, 1, 3, 1}, {2, 1, 3, 2}, {5, 1, 3, 3}, {2, 3, 1, 1}, {-1, 0, 0, 0},
	} {
		if got := slots.Clamp(c.n, c.lo, c.hi); got != c.want {
			t.Errorf("Clamp(%d, %d, %d) = %d, want %d", c.n, c.lo, c.hi, got, c.want)
		}
	}
}

func easy(live int) slots.Signals {
	return slots.Signals{Known: true, Load1: 2, CPUs: 8, MemAvail: 8 << 30, MemTotal: 16 << 30, Live: live}
}

// AIMD: a step up when easy and full, halved when overloaded by load or
// memory, held between the thresholds, with free slots, or without signals;
// always within min..max.
func TestAIMD(t *testing.T) {
	c := slots.ConfigOf(rule(t, "per_host: 3\nslots: {policy: aimd, min: 1, start: 1}\n"))
	for name, k := range map[string]struct {
		current int
		s       slots.Signals
		want    int
		reason  string
	}{
		"easy and full goes up":   {1, easy(1), 2, "up from 1 to 2"},
		"never past max":          {3, easy(3), 3, "held to 1..3"},
		"a free slot holds":       {2, easy(1), 2, "runs 1 attempts of its 2"},
		"between thresholds":      {2, slots.Signals{Known: true, Load1: 6.4, CPUs: 8, Live: 2}, 2, "between 70% and 100%"},
		"overloaded halves":       {3, slots.Signals{Known: true, Load1: 12, CPUs: 8, Live: 3}, 1, "halved from 3 to 1"},
		"never below min":         {1, slots.Signals{Known: true, Load1: 12, CPUs: 8, Live: 1}, 1, "held to 1..3"},
		"low memory halves":       {2, slots.Signals{Known: true, Load1: 1, CPUs: 8, MemAvail: 1 << 30, MemTotal: 16 << 30, Live: 2}, 1, "memory available 6%"},
		"no signals hold":         {2, slots.Signals{Live: 2}, 2, "no recent headroom signals"},
		"memory alone holds":      {2, slots.Signals{Known: true, MemAvail: 8 << 30, MemTotal: 16 << 30, Live: 2}, 2, "load is not known"},
		"none recorded starts":    {-1, easy(0), 1, "runs 0 attempts of its 1"},
		"none recorded, full, up": {-1, easy(1), 2, "up from 1 to 2"},
	} {
		d, err := slots.Decide(c, k.current, k.s)
		if err != nil {
			t.Fatal(err)
		}
		if d.New != k.want || !strings.Contains(d.Reason, k.reason) || d.Policy != "aimd" {
			t.Errorf("%s: %d (%q), want %d with %q", name, d.New, d.Reason, k.want, k.reason)
		}
	}
}

// Fixed holds the start, whatever the host says; a count recorded under an
// older rule comes into the new range.
func TestFixedAndARangeThatMoved(t *testing.T) {
	c := slots.ConfigOf(rule(t, "per_host: 2\nslots: {policy: fixed, min: 1, start: 2}\n"))
	d, err := slots.Decide(c, 1, slots.Signals{Known: true, Load1: 64, CPUs: 8})
	if err != nil || d.New != 2 || !d.Moved() || d.Reason != "fixed at 2" {
		t.Fatalf("fixed: %+v %v", d, err)
	}
	c = slots.ConfigOf(rule(t, "per_host: 1\nslots: {policy: aimd, min: 1, start: 1}\n"))
	d, err = slots.Decide(c, 3, easy(3))
	if err != nil || d.Old != 3 || d.New != 1 || !strings.HasPrefix(d.Reason, "the rule's range is now 1..1") {
		t.Fatalf("range moved: %+v %v", d, err)
	}
	if _, err := slots.Decide(slots.Config{Policy: "pid"}, 1, easy(1)); err == nil {
		t.Fatal("an unknown policy decided")
	}
}

func TestSignalsDoc(t *testing.T) {
	if doc := slots.SignalsDoc(slots.Signals{Live: 2}); len(doc) != 2 || doc["known"] != false || doc["live"] != 2 {
		t.Fatalf("unknown: %v", doc)
	}
	doc := slots.SignalsDoc(easy(1))
	if doc["load1_hundredths"] != int64(200) || doc["cpus"] != 8 || doc["mem_total_bytes"] != int64(16<<30) {
		t.Fatalf("known: %v", doc)
	}
}
