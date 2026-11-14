package landing

import (
	"context"
	"testing"
	"time"
)

// CUT §2.3 (each gate uses the new rule) and §2.5.6 / S3 ①.
// These are the two serialized orders of the judgement, not a substitute for
// S3's transaction/advisory-lock concurrency test.
func TestSecondGateReReadsRuleTextAndVersion(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "tightening_before_gate", false: "tightening_after_gate"}[before], func(t *testing.T) {
			f := eligibleFacts()
			reads := 0
			read := func(context.Context, string) (Facts, error) { reads++; return f, nil }
			now := epoch.Add(time.Hour)
			claim, err := Evaluate(context.Background(), read, task1, "claim", now)
			if err != nil || !claim.Allowed {
				t.Fatal(claim, err)
			}
			f.Operation = Operation{RuleVersion: 1, R: R}
			tighten := func() { f.Rule.Version = 2; f.Rule.Landing.Repos[0].Deny = []string{"console/src/components/**"} }
			if before {
				tighten()
			}
			push, err := Evaluate(context.Background(), read, task1, "push", now)
			if err != nil || push.Allowed == before {
				t.Fatal(push, err)
			}
			if !before {
				tighten()
				if !push.Allowed {
					t.Fatal("later rule rewrote prior judgement")
				}
			}
			if reads != 2 {
				t.Fatal("gate reused claim facts")
			}
		})
	}
	// A version change that still permits the patch is not itself a refusal.
	f := eligibleFacts()
	f.Operation.RuleVersion = 1
	f.Rule.Version = 2
	if !EligibleToPush(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("version equality used instead of rule judgement")
	}
}
