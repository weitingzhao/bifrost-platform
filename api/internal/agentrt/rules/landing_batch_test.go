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
