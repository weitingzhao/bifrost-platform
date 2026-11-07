package rules

import (
	"strings"
	"testing"
)

// 2026-11-06 Owner: a landing batch's size is the rule's limits.max_batch
// (left out: no cap), not a constant in the code.
func TestLandingMaxBatchOptional(t *testing.T) {
	if r := landingRule(t); r.Landing.Limits.MaxBatch != 0 {
		t.Fatal("max_batch left out is not 0 (no cap)", r.Landing.Limits.MaxBatch)
	}
	r, e := Parse([]byte(strings.Replace(landingYAML, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 1\n  max_batch: 24", 1)))
	if e != nil || r.Landing.Limits.MaxBatch != 24 {
		t.Fatal("max_batch 24", e)
	}
	b, e := r.BodyJSON()
	if e != nil || !strings.Contains(string(b), `"max_batch":24`) {
		t.Fatal("max_batch not in the canonical body", string(b), e)
	}
	if round, e := Parse(b); e != nil || round.Landing.Limits.MaxBatch != 24 {
		t.Fatal("max_batch does not round-trip", e)
	}
	if b, _ := landingRule(t).BodyJSON(); strings.Contains(string(b), "max_batch") {
		t.Fatal("a rule without max_batch gained it in its canonical body")
	}
	for _, bad := range []string{"0", "-1", "'8'"} {
		if _, e := Parse([]byte(strings.Replace(landingYAML, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 1\n  max_batch: "+bad, 1))); e == nil {
			t.Errorf("accepted max_batch %s", bad)
		}
	}
}
