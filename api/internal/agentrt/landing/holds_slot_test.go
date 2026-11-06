package landing

import "testing"

// A granted landing's final gate yields only to an operation past its CI:
// pushing to main or deploying (PROD 2026-11-06).
func TestHoldsSlotEveryPhase(t *testing.T) {
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
		{"main_result", "ok", true},
		{"main_result", "unknown", true},
		{"main_result", "non_ff", false},
		{"landed", "", false},
		{"reverted", "", false},
		{"failed", "bound", false},
		{"discarded", "main_changed", false},
		{"rehearsed", "", false},
		{"stg_pending", "", true},
		{"stg_snapshot", "", true},
		{"stg_started", "", true},
		{"stg_result", "failed", true},
		{"stg_verified", "", false},
		{"stg_failed", "", false},
	} {
		o := Operation{Phase: c.phase, Outcome: c.outcome}
		if got := o.HoldsSlot(); got != c.holds {
			t.Errorf("%s/%s: HoldsSlot %v, want %v", c.phase, c.outcome, got, c.holds)
		}
		if o.HoldsSlot() && !o.Active() {
			t.Errorf("%s/%s holds the slot but is not active", c.phase, c.outcome)
		}
	}
}
