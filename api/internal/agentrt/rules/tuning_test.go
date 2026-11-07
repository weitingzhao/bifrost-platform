package rules_test

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

var tuning = text("tuning", `values:
  lease_ttl_s: 180
  assess_per_pass: 5
`)

// A tuning rule reads the keys it names, each within the registry's
// bounds; a key it does not name, and every key without a rule, keeps the
// code's default.
func TestTuningParse(t *testing.T) {
	r := parse(t, tuning)
	body, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","rule_id":"tuning","rule_version":1,"values":{"assess_per_pass":5,"lease_ttl_s":180}}`
	if r.Kind != rules.KindTuning || string(body) != want {
		t.Fatalf("kind %s, body\n%s\nwant\n%s", r.Kind, body, want)
	}
	if r.Tuning.Duration(rules.TuneLeaseTTL) != 3*time.Minute || r.Tuning.Int(rules.TuneAssessPerPass) != 5 ||
		r.Tuning.Int(rules.TuneThreadsPerDay) != 200 {
		t.Fatalf("values %+v", r.Tuning.Values)
	}
	var none *rules.Tuning
	if none.Duration(rules.TuneLeaseTTL) != 2*time.Minute || none.Int(rules.TuneAssessPerPass) != 3 {
		t.Fatal("no rule in force is not the defaults")
	}
	if r := parse(t, text("tuning", "values: {}\n")); len(r.Tuning.Values) != 0 || r.Tuning.Int(rules.TuneNudgesPerPass) != 3 {
		t.Fatalf("an empty rule: %+v", r.Tuning)
	}
	for name, s := range map[string]string{
		"no values":        text("tuning", ""),
		"an unknown key":   strings.Replace(tuning, "assess_per_pass", "assess_every_s", 1),
		"below the floor":  strings.Replace(tuning, "lease_ttl_s: 180", "lease_ttl_s: 10", 1),
		"above a ceiling":  strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: 500", 1),
		"not a number":     strings.Replace(tuning, "assess_per_pass: 5", "assess_per_pass: five", 1),
		"a key twice":      tuning + "  assess_per_pass: 4\n",
		"a field besides":  tuning + "auto: true\n",
		"an argument":      strings.Replace(tuning, "rule_id: tuning", "rule_id: tuning:dev", 1),
		"values not a map": text("tuning", "values: [lease_ttl_s]\n"),
	} {
		if _, err := rules.Parse([]byte(s)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// The registry is closed and sound: names sorted and unique, each default
// within its bounds, a key in seconds named _s and only such.
func TestTuningRegistry(t *testing.T) {
	keys := rules.TuningKeys()
	if len(keys) == 0 {
		t.Fatal("no keys")
	}
	for i, k := range keys {
		if i > 0 && keys[i-1].Name >= k.Name {
			t.Errorf("%s after %s: the registry is sorted, each name once", k.Name, keys[i-1].Name)
		}
		if k.Min < 1 || k.Default < k.Min || k.Default > k.Ceiling || k.ReadBy == "" {
			t.Errorf("%+v: 1 <= floor <= default <= ceiling, and a reader", k)
		}
		if (k.Unit == rules.TuningSeconds) != strings.HasSuffix(k.Name, "_s") || (k.Unit != rules.TuningSeconds && k.Unit != rules.TuningCount) {
			t.Errorf("%+v: a key in seconds is named _s, a count is not", k)
		}
	}
}

// A tuning rule is never shown to tighten: each version needs the Owner's
// signature (or the card that approves a capacity change), even one that
// only lowers values.
func TestTuningNeverTightens(t *testing.T) {
	prev := parse(t, tuning)
	next := parse(t, strings.Replace(strings.Replace(tuning, "rule_version: 1\ndirection: initial", "rule_version: 2\ndirection: tighten", 1),
		"assess_per_pass: 5", "assess_per_pass: 4", 1))
	if err := rules.Tightens(prev, next); err == nil {
		t.Fatal("a tuning rule took effect as a tightening")
	}
}
