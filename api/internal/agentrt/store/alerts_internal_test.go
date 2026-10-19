package store

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
)

// Every spec the seat reads states the protocol's duties, before its data
// (seat protocol v2): a brief's, a controller's note's and a plan's.
func TestTheSeatsSpecsStateTheDuties(t *testing.T) {
	t.Parallel()
	text := func(raw []byte, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	duties := strings.ReplaceAll(seatproto.Duties, "\n", `\n`) // as canonical JSON writes the text
	for name, spec := range map[string]string{
		"brief":     text(briefSpec("owner", "ask", "status?", nil, "Taken now.")),
		"scheduled": text(controllerSpec(scheduledText, map[string]any{"note": "Scheduled brief: x"})),
		"nudge":     text(controllerSpec(nudgeText, map[string]any{"note": "Nudge: task t_1 is blocked."})),
		"plan": text(planSpec(Goal{Title: "a goal", Text: "do it"}, []string{"bifrost-platform"},
			map[string]string{"bifrost-platform": strings.Repeat("a", 40)}, "M-1", "Taken now.")),
	} {
		// the duties themselves claim no approval: no seat spec is a policy
		// alert for their words (claims.go, Gate B T04)
		if phrase, found := ApprovalClaim([]byte(spec)); found {
			t.Errorf("%s: the spec claims an approval: %q", name, phrase)
		}
		i := strings.Index(spec, duties)
		if i < 0 {
			t.Errorf("%s: the duties are not in the spec", name)
			continue
		}
		next := "## The data"
		if name == "plan" {
			next = "## What to hand in"
		}
		if j := strings.Index(spec, next); j < i {
			t.Errorf("%s: the duties come after %q", name, next)
		}
	}
}
