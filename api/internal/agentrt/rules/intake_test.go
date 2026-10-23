package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The intake-gates rule as DESIGN-crew-intake §7.2 lays it out: small passes
// 1, 2, 3, 7, 8; medium adds 4 and 5; large adds 6.
var intake = text("intake-gates", `classes:
  small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]
  medium: [rule_in_force, review, design, quota, owner_priority, classified, brief_confirmed]
  large: [brief_confirmed, classified, owner_priority, design, review, owner_decisions, quota, rule_in_force]
max_live_per_product: 1
exceptions: [fault_small, answer_only]
`)

// It reads, kept in the predicate's order (whatever order the text lists),
// and its body is the canonical JSON of that.
func TestIntakeGatesParse(t *testing.T) {
	r := parse(t, intake)
	body, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"classes":{"large":["brief_confirmed","classified","owner_priority","design","review","owner_decisions","quota","rule_in_force"],` +
		`"medium":["brief_confirmed","classified","owner_priority","design","review","quota","rule_in_force"],` +
		`"small":["brief_confirmed","classified","owner_priority","quota","rule_in_force"]},"direction":"initial",` +
		`"effective_at":"2026-10-15T00:00:00Z","exceptions":["answer_only","fault_small"],"expires_at":"2027-01-15T00:00:00Z",` +
		`"max_live_per_product":1,"rule_id":"intake-gates","rule_version":1}`
	if r.Kind != rules.KindIntake || string(body) != want {
		t.Fatalf("kind %s, body\n%s\nwant\n%s", r.Kind, body, want)
	}
	g := r.Intake
	if !g.Requires("medium", rules.IntakeReview) || g.Requires("small", rules.IntakeDesign) || g.Requires("medium", rules.IntakeOwnerDecisions) ||
		!g.Allows(rules.IntakeFaultSmall) || g.MaxLivePerProduct != 1 {
		t.Fatalf("read as %+v", g)
	}
	// Reading it again from its own body's order gives the same body.
	none := parse(t, strings.Replace(intake, "[fault_small, answer_only]", "[]", 1))
	if len(none.Intake.Exceptions) != 0 || none.Intake.Allows(rules.IntakeAnswerOnly) {
		t.Fatalf("no exceptions read as %v", none.Intake.Exceptions)
	}
}

// What an intake-gates rule may not say.
func TestIntakeGatesRefuses(t *testing.T) {
	for name, s := range map[string]string{
		"a class without the brief's confirmation": strings.Replace(intake, "small: [brief_confirmed, ", "small: [", 1),
		"a class without the quota":                strings.Replace(intake, "small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]", "small: [brief_confirmed, classified, owner_priority, rule_in_force]", 1),
		"a class without the rule itself":          strings.Replace(intake, "owner_decisions, quota, rule_in_force]", "owner_decisions, quota]", 1),
		"a larger class passing fewer gates":       strings.Replace(intake, "medium: [rule_in_force, review, design, quota, owner_priority,", "medium: [rule_in_force, review, design, quota,", 1),
		"an item nobody knows":                     strings.Replace(intake, "quota, rule_in_force]\n  medium", "quota, rule_in_force, vibes]\n  medium", 1),
		"an item twice":                            strings.Replace(intake, "small: [brief_confirmed,", "small: [brief_confirmed, brief_confirmed,", 1),
		"a class missing":                          strings.Replace(intake, "  small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]\n", "", 1),
		"a class nobody has":                       strings.Replace(intake, "classes:\n", "classes:\n  tiny: [brief_confirmed]\n", 1),
		"a bound past the limit":                   strings.Replace(intake, "max_live_per_product: 1", "max_live_per_product: 21", 1),
		"an exception nobody has":                  strings.Replace(intake, "[fault_small, answer_only]", "[fault_small, vip]", 1),
		"no exceptions field":                      strings.Replace(intake, "exceptions: [fault_small, answer_only]\n", "", 1),
		"an argument in the id":                    strings.Replace(intake, "rule_id: intake-gates", "rule_id: intake-gates:screening", 1),
	} {
		if _, err := rules.Parse([]byte(s)); err == nil {
			t.Errorf("%s: parsed\n%s", name, s)
		} else {
			t.Logf("%s: %v", name, err)
		}
	}
}

// Tightening an intake-gates rule: each class keeps every gate it had, the
// bound only comes down, an exception only closes (§4.6).
func TestIntakeGatesTightens(t *testing.T) {
	next := func(from, to string) string {
		s := strings.Replace(intake, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: tighten", 1)
		if from != "" {
			if !strings.Contains(s, from) {
				t.Fatalf("no %q", from)
			}
			s = strings.Replace(s, from, to, 1)
		}
		return s
	}
	for name, c := range map[string]struct {
		next    string
		tighter bool
	}{
		"the same rule again":         {next("", ""), true},
		"a gate added to small":       {next("small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]", "small: [brief_confirmed, classified, owner_priority, design, quota, rule_in_force]"), true},
		"the priority dropped":        {next("small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]", "small: [brief_confirmed, classified, quota, rule_in_force]"), false},
		"the review dropped (medium)": {next("medium: [rule_in_force, review, design,", "medium: [rule_in_force, design,"), false},
		"no slot at all":              {next("max_live_per_product: 1", "max_live_per_product: 0"), true},
		"a second slot":               {next("max_live_per_product: 1", "max_live_per_product: 2"), false},
		"an exception closed":         {next("[fault_small, answer_only]", "[answer_only]"), true},
		"expiring later":              {next("2027-01-15T00:00:00Z", "2027-06-15T00:00:00Z"), false},
	} {
		err := rules.Tightens(parse(t, intake), parse(t, c.next))
		if (err == nil) != c.tighter {
			t.Errorf("%s: tightens %v (%v); want %v", name, err == nil, err, c.tighter)
		}
	}
	closed := parse(t, strings.Replace(intake, "[fault_small, answer_only]", "[answer_only]", 1))
	if err := rules.Tightens(closed, parse(t, next("", ""))); err == nil {
		t.Error("reopening an exception was taken as tightening")
	}
}

// A kind Tightens has no comparison for is never shown to tighten: before
// §4.6 such a kind fell through to "tightens".
func TestTightensRefusesAKindItCannotCompare(t *testing.T) {
	r := parse(t, concurrency)
	r.Kind = "visibility"
	n := r
	n.Version = 2
	if err := rules.Tightens(r, n); err == nil || !strings.Contains(err.Error(), "no tightening comparison") {
		t.Fatalf("an unknown kind: %v", err)
	}
}
