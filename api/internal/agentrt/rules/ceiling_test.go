package rules_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// R5 phase 2 step 3b: the capacity caps are defaults now, and a signed rule
// goes past them up to the sanity ceilings, not one past.
func TestCapacityCeilings(t *testing.T) {
	for _, n := range []int{rules.DefaultPerHost + 1, rules.PerHostCeiling} {
		r := parse(t, strings.Replace(concurrency, "per_host: 1", fmt.Sprintf("per_host: %d", n), 1))
		if r.PerHost == nil || *r.PerHost != n {
			t.Errorf("per_host %d: %v", n, r.PerHost)
		}
	}
	r := parse(t, strings.Replace(strings.Replace(budget, "mission: {max_attempts: 40, max_wall_hours: 48}",
		fmt.Sprintf("mission: {max_attempts: %d, max_wall_hours: %d}", rules.MissionAttemptsCeiling, rules.MissionWallHoursCeiling), 1),
		"attempt_hard_limit_s: 3600", fmt.Sprintf("attempt_hard_limit_s: %d", rules.AttemptLimitSCeiling), 1))
	if b := r.Budget; b.Mission.MaxAttempts != 10000 || b.Mission.MaxWallHours != 8760 || b.AttemptLimitSec != 7*24*3600 {
		t.Errorf("a budget at the ceilings: %+v", b)
	}
	for name, s := range map[string]string{
		"attempts past the ceiling": strings.Replace(budget, "max_attempts: 40", fmt.Sprint("max_attempts: ", rules.MissionAttemptsCeiling+1), 1),
		"hours past the ceiling":    strings.Replace(budget, "max_wall_hours: 48", fmt.Sprint("max_wall_hours: ", rules.MissionWallHoursCeiling+1), 1),
		"a limit past the ceiling":  strings.Replace(budget, "attempt_hard_limit_s: 3600", fmt.Sprint("attempt_hard_limit_s: ", rules.AttemptLimitSCeiling+1), 1),
	} {
		if _, err := rules.Parse([]byte(s)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}
