package rules

import "testing"

func TestLandingAllowKeepsStatedOrderAndCanonicalBody(t *testing.T) {
	t.Parallel()
	r := landingRule(t)
	allow := r.Landing.Repos[0].Allow
	if allow[0] != "console/src/components/**/*.tsx" || allow[1] != "console/src/components/**/*.css" {
		t.Fatalf("stated order lost: %v", allow)
	}
	before, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	r.Landing.Repos[0].Allow[0], r.Landing.Repos[0].Allow[1] = allow[1], allow[0]
	after, err := r.BodyJSON()
	if err != nil || string(before) != string(after) {
		t.Fatalf("canonical form changed: %s %v", after, err)
	}
}
