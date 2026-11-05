package plan_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// R5:5.5-15: a rule that counts the step's own work holds only with the
// Owner's sign-off as a direct part of a top-level all.
func TestSignoffGate(t *testing.T) {
	for _, c := range []struct {
		name, rule string
		counts     bool
		gated      bool
	}{
		{"no step work: a task", `{"task_accepted":"t_01M6P68EJ77C2MWJ29EA9CC5N5"}`, false, true},
		{"no step work: sign-off alone", `{"owner_signoff":"r"}`, false, true},
		{"no step work: approvals and a sign-off", `{"all":[{"approval_executed":1},{"owner_signoff":"r"}]}`, false, true},
		{"step work alone", `{"step_tasks":{"state":"accepted"}}`, true, false},
		{"the three atoms, no sign-off", `{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}}]}`, true, false},
		{"the three atoms and a sign-off", `{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}},{"owner_signoff":"r"}]}`, true, true},
		{"sign-off first", `{"all":[{"owner_signoff":"r"},{"step_landed":{}}]}`, true, true},
		{"step work nested under the gated all", `{"all":[{"any":[{"step_landed":{}},{"task_accepted":"t_01M6P68EJ77C2MWJ29EA9CC5N5"}]},{"owner_signoff":"r"}]}`, true, true},
		{"sign-off only inside an any", `{"all":[{"step_landed":{}},{"any":[{"owner_signoff":"r"},{"task_accepted":"t_01M6P68EJ77C2MWJ29EA9CC5N5"}]}]}`, true, false},
		{"top any with a sign-off branch", `{"any":[{"step_landed":{}},{"owner_signoff":"r"}]}`, true, false},
		{"gated all under a top any", `{"any":[{"all":[{"step_landed":{}},{"owner_signoff":"r"}]},{"task_accepted":"t_01M6P68EJ77C2MWJ29EA9CC5N5"}]}`, true, false},
		{"sign-off nested in an all inside the top all", `{"all":[{"step_landed":{}},{"all":[{"owner_signoff":"r"}]}]}`, true, true},
		// Plan-view adoption wraps a gated rule beside its bound atoms (composeBoundRule): still gated.
		{"a gated rule wrapped by adoption", `{"all":[{"all":[{"step_landed":{}},{"owner_signoff":"r"}]},{"main_effective":{"binding":"b","version":1}}]}`, true, true},
		{"sign-off in an any inside an all", `{"all":[{"step_landed":{}},{"all":[{"any":[{"owner_signoff":"r"},{"step_tasks":{"state":"accepted"}}]}]}]}`, true, false},
	} {
		r, err := plan.Parse([]byte(c.rule))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := r.CountsStepWork(); got != c.counts {
			t.Errorf("%s: CountsStepWork = %v, want %v", c.name, got, c.counts)
		}
		if got := r.SignoffGated(); got != c.gated {
			t.Errorf("%s: SignoffGated = %v, want %v", c.name, got, c.gated)
		}
	}
}
