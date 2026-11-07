package landing

import (
	"testing"
	"time"
)

// 2026-11-06 landed changes waiting on a blocked STG release kept the
// repository's landing slot: the next root waited unclaimed:limit although
// nothing was landing. Only an operation before its change is on main holds
// the slot; a landed one never does, whatever its STG phase.
func TestLandingSlotLimitLandedHoldsNoSlot(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		want int
	}{
		{"claimed", 1, 1},
		{"branch_pushed", 4, 1},
		{"main_intent", 8, 1},
		{"main_result ok", 9, 0},
		{"landed", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mustFold(t, records()[:tc.n])
			if got := s.InFlight("land:2"); got != tc.want {
				t.Fatalf("in flight %d, want %d", got, tc.want)
			}
			if s.InFlight("land:1") != 0 {
				t.Fatal("an operation counted against itself")
			}
			// A second root is claimed exactly when the first holds no slot.
			f := eligibleFacts()
			f.OtherInFlight = s.InFlight("land:2")
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Allowed != (tc.want == 0) || !d.Allowed && d.Reason != "limit" {
				t.Fatalf("claim %+v", d)
			}
		})
	}
	landed := mustFold(t, records()[:10]).Operations["land:1"]
	if landed.HoldsSlot() || !landed.EverLanded {
		t.Fatal("a landed operation holds the slot")
	}
}
