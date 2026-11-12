package rules

import (
	"strings"
	"testing"
)

// limits.max_batch is legacy (cleanup wave 1, 2026-11-11): it bounds
// nothing, but a rule signed with it still parses and keeps it in its
// canonical body, so the stored body_json of such a rule still matches.
func TestLandingMaxBatchLegacy(t *testing.T) {
	if r := landingRule(t); r.Landing.Limits.LegacyMaxBatch != 0 {
		t.Fatal("max_batch left out is not 0", r.Landing.Limits.LegacyMaxBatch)
	}
	r, e := Parse([]byte(strings.Replace(landingYAML, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 1\n  max_batch: 24", 1)))
	if e != nil || r.Landing.Limits.LegacyMaxBatch != 24 {
		t.Fatal("a rule signed with max_batch no longer parses", e)
	}
	b, e := r.BodyJSON()
	if e != nil || !strings.Contains(string(b), `"max_batch":24`) {
		t.Fatal("max_batch dropped from the canonical body of a rule signed with it", string(b), e)
	}
	if round, e := Parse(b); e != nil || round.Landing.Limits.LegacyMaxBatch != 24 {
		t.Fatal("max_batch does not round-trip", e)
	}
	if b, _ := landingRule(t).BodyJSON(); strings.Contains(string(b), "max_batch") {
		t.Fatal("a rule without max_batch gained it in its canonical body")
	}
}

// limits.max_landings_per_day is legacy too (cleanup wave 1): every rule
// signed so far states it, and must still parse with the same canonical
// body; a new rule may leave it out and then never gains it.
func TestLandingMaxLandingsPerDayLegacy(t *testing.T) {
	r := landingRule(t)
	if r.Landing.Limits.LegacyMaxLandingsPerDay != 5 {
		t.Fatal("a rule signed with max_landings_per_day no longer reads it", r.Landing.Limits)
	}
	b, e := r.BodyJSON()
	if e != nil || !strings.Contains(string(b), `"max_landings_per_day":5`) {
		t.Fatal("max_landings_per_day dropped from the canonical body of a rule signed with it", string(b), e)
	}
	if round, e := Parse(b); e != nil || round.Landing.Limits.LegacyMaxLandingsPerDay != 5 {
		t.Fatal("max_landings_per_day does not round-trip", e)
	}
	without := strings.Replace(landingYAML, "  max_landings_per_day: 5\n", "", 1)
	n, e := Parse([]byte(without))
	if e != nil {
		t.Fatal("a rule without max_landings_per_day is refused", e)
	}
	if b, _ := n.BodyJSON(); strings.Contains(string(b), "max_landings_per_day") {
		t.Fatal("a rule without max_landings_per_day gained it in its canonical body", string(b))
	}
	// Dropping it in a later version is a capacity change, not a loosening.
	if e := Tightens(r, n); e != nil {
		t.Fatal("dropping the legacy quota loosens", e)
	}
}
