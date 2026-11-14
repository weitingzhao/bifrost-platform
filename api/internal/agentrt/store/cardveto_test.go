package store_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 489 A1 on T12 (the coordinating thread 2026-10-22 for the Owner:
// A1): under assessment: veto an AI assessment only ever keeps the rule from
// the card's recommendation. A card that recommends nothing takes the option
// that changes nothing (b on the decision cards) whatever its assessment
// says; a card that recommends one is answered as before; an option the card
// does not have is never taken, and the card is left to the Owner. Without
// assessment: veto the rule reads as versions 1 to 3 were signed.
func TestRuleChoiceUnderAVeto(t *testing.T) {
	t.Parallel()
	ab, abc := []string{"a", "b"}, []string{"a", "b", "c"}
	for name, c := range map[string]struct {
		kind, rec, assessed string
		veto                bool
		options             []string
		want, why           string
	}{
		"no recommendation, the assessment's a":      {store.CardBlocked, "", "a", true, abc, "b", "not the one its AI assessment recommends (a)"},
		"no recommendation, no assessment":           {store.CardBlocked, "", "", true, abc, "b", "the card recommends nothing: the rule takes the option that changes nothing"},
		"no recommendation, the assessment's b too":  {store.CardBlocked, "", "b", true, abc, "b", ", which its AI assessment recommends too"},
		"a recorder import, no recommendation":       {"recorder_import", "", "a", true, ab, "b", "the option that changes nothing"},
		"recommended and agreed":                     {store.CardBlocked, "a", "a", true, abc, "a", ""},
		"recommended, the assessment differs":        {store.CardBlocked, "a", "c", true, abc, "b", "the rule takes the option that changes nothing"},
		"recommended, no assessment":                 {"mission", "a", "", true, ab, "a", ""},
		"a planner's question, nothing recommended":  {store.CardPlanQuestion, "", "a", true, ab, "", ""},
		"a planner's question, its own option":       {store.CardPlanQuestion, "b", "a", true, ab, "b", ""},
		"no option that changes nothing on the card": {store.CardBlocked, "", "", true, []string{"a"}, "", ""},
		"the recommendation not among the options":   {"plan_change", "a", "", true, nil, "", ""},
		"before the veto: the assessment's option":   {store.CardBlocked, "", "a", false, abc, "a", "the rule takes the assessment's"},
		"before the veto: options not read":          {"plan_change", "a", "", false, nil, "a", ""},
	} {
		option, why := store.RuleChoiceForTest(c.kind, c.rec, c.assessed, c.veto, c.options)
		if option != c.want || (c.why == "" && why != "" && c.rec != "") || !strings.Contains(why, c.why) {
			t.Errorf("%s: %q (%q), want %q (%q)", name, option, why, c.want, c.why)
		}
		// Review 507 C1: the reason never says the assessment recommends
		// another option when it recommends the one taken.
		if c.veto && c.assessed == option && strings.Contains(why, "not the one") {
			t.Errorf("%s: %q says %q", name, option, why)
		}
	}
}
