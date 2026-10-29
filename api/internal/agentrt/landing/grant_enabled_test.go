package landing

import (
	"testing"
	"time"
)

// Review 694 R-03: until the Owner's signed envelope names the external
// host (Facts.GrantEnabled), a core change is refused at the claim as
// floor and a pinned soft one as rule, as before C-4/C-5; nothing prepares
// to wait. With it, both may prepare (the final gate still never pushes
// them without a grant). On dff96c3c the unpinned core change was admitted.
func TestC5R03NothingWaitsUntilGrantsAreEnabled(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	for _, c := range []struct {
		path, pin, refused string
	}{
		{"api/internal/agentrt/store/example.go", "", "floor"},
		{"api/internal/research/x.go", "external:coordinator", "floor"},
	} {
		for _, enabled := range []bool{false, true} {
			f := eligibleFacts()
			f.Environment, f.Mode, f.ExecutorPin, f.GrantEnabled = "prod", "live", c.pin, enabled
			f.Changes[0].OldPath, f.Changes[0].NewPath = c.path, c.path
			d := EligibleToClaim(f, now)
			if enabled != d.Allowed || !enabled && d.Reason != c.refused {
				t.Errorf("%s pin=%q enabled=%v: %+v", c.path, c.pin, enabled, d)
			}
		}
	}
}
