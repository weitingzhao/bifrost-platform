package landing

import "testing"

// A granted landing's final gate yields only to an operation pushing to main
// (PROD 2026-11-06); a landed change, deploying or not, never holds the slot.
func TestHoldsGrantedSlotEveryPhase(t *testing.T) {
	for _, c := range []struct {
		phase, outcome string
		holds          bool
	}{
		{"", "", false},
		{"claimed", "", false},
		{"prepared", "", false},
		{"branch_intent", "", false},
		{"branch_pushed", "", false}, // ci_kick, ci_seen and ci_rerun leave it here
		{"awaiting_grant", "", false},
		{"parked", "", false},
		{"main_intent", "", true},
		{"main_result", "ok", true}, // pushed, not yet reconciled landed
		{"main_result", "unknown", true},
		{"main_result", "non_ff", false},
		{"landed", "", false},
		{"reverted", "", false},
		{"failed", "bound", false},
		{"discarded", "main_changed", false},
		{"rehearsed", "", false},
		{"stg_pending", "", false},
		{"stg_snapshot", "", false},
		{"stg_started", "", false},
		{"stg_result", "failed", false},
		{"stg_verified", "", false},
		{"stg_failed", "", false},
	} {
		o := Operation{Phase: c.phase, Outcome: c.outcome}
		if got := o.HoldsGrantedSlot(); got != c.holds {
			t.Errorf("%s/%s: HoldsGrantedSlot %v, want %v", c.phase, c.outcome, got, c.holds)
		}
		if o.HoldsGrantedSlot() && !o.Active() {
			t.Errorf("%s/%s holds the slot but is not active", c.phase, c.outcome)
		}
	}
}
