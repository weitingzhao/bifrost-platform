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

// CUT §2.4: unknown reconciles to non_ff, freeing the slot and permitting k+1.
func TestUnknownPushFoldsToNonFF(t *testing.T) {
	rs := records()
	rs[8]["outcome"] = "unknown"
	s := mustFold(t, rs[:9])
	if err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: record("main_result", "outcome", "non_ff")}); err != nil {
		t.Fatal(err)
	}
	o := s.Operations["land:1"]
	if !o.Terminal() || o.Active() || o.BlocksClaim() {
		t.Fatal(o)
	}
	next := records()[0]
	next["op"] = "land:2"
	if err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: next}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: record("landed", "R", R)}); err == nil {
		t.Fatal("non_ff became landed")
	}
}

// CUT §2.4 defines no outgoing transition for failed. §2.7's explicit retry
// decision has no record in v1; keep the old operation immutable until that
// protocol gap is decided (Work 570 report B3).
func TestFailedOperationCannotBeResumed(t *testing.T) {
	for _, why := range reasons {
		rs := []Record{records()[0], record("failed", "why", why)}
		for _, candidate := range append(records(), record("unparked"), record("failed", "why", "declined")) {
			s := mustFold(t, rs)
			if err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: candidate}); err == nil {
				t.Fatalf("failed %s -> %s", why, candidate.String("kind"))
			}
			if !s.Operations["land:1"].BlocksClaim() || s.Operations["land:1"].Active() {
				t.Fatal("failed root reopened")
			}
		}
	}
}
