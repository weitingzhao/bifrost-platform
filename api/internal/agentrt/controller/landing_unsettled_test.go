package controller

import (
	"slices"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Each phase ClaimLandingDrill refuses from the stream alone is skipped
// before any Git read; a task with no operation, or whose last operation was
// discarded or failed and retried (the k+1 waits), stays a candidate.
func TestUnsettledSkipsWhatTheStreamAlreadyRefuses(t *testing.T) {
	ops := map[string]landing.Operation{
		"land:t_reh:1":   {Task: "t_reh", Phase: "rehearsed"},
		"land:t_push:1":  {Task: "t_push", Phase: "branch_pushed"},
		"land:t_park:1":  {Task: "t_park", Phase: "parked"},
		"land:t_fail:1":  {Task: "t_fail", Phase: "failed"},
		"land:t_land:1":  {Task: "t_land", Phase: "discarded", EverLanded: true},
		"land:t_disc:1":  {Task: "t_disc", Phase: "discarded"},
		"land:t_retry:1": {Task: "t_retry", Phase: "failed", Retried: true},
		"enabled:dev":    {},
	}
	got := unsettled([]string{"t_new", "t_reh", "t_push", "t_park", "t_fail", "t_land", "t_disc", "t_retry"}, ops)
	if want := []string{"t_new", "t_disc", "t_retry"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := unsettled(nil, ops); len(got) != 0 {
		t.Fatal(got)
	}
}
