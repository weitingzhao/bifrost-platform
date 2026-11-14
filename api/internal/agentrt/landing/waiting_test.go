package landing

import (
	"slices"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func TestC4PreparationEligibilityAndFinalPolicy(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	for _, pin := range []bool{false, true} {
		for _, path := range []string{"api/internal/research/x.go", "api/internal/agentrt/store/x.go", ".env.local"} {
			f := eligibleFacts()
			f.Environment, f.Mode, f.GrantEnabled = "prod", "live", true
			if pin {
				f.ExecutorPin = "external:coordinator"
			}
			if !EligibleToClaim(f, now).Allowed || !EligibleToPush(f, now).Allowed {
				t.Fatal("same entry success control")
			}
			f.Changes[0].OldPath, f.Changes[0].NewPath = path, path
			// The research path is floor-grantable: it waits for
			// approve_floor_landing with or without a pin.
			allowed := path != ".env.local"
			if EligibleToClaim(f, now).Allowed != allowed {
				t.Fatal(pin, path, EligibleToClaim(f, now))
			}
			push := EligibleToPush(f, now)
			if push.Allowed || allowed && push.Reason != "awaiting_grant" {
				t.Fatal(push)
			}
		}
	}
	if !slices.Contains(RetryableReasons, "approval_expired") || slices.Contains(RetryableReasons, "declined") {
		t.Fatal(RetryableReasons)
	}
	f := eligibleFacts()
	f.ExecutorPin = "external:coordinator"
	f.Changes = []rules.LandingChange{{NewPath: ".env", NewMode: "100644", Added: 1}}
	if reason, _ := PolicyChecks(f, now); reason != "floor" {
		t.Fatal(reason)
	}
}
