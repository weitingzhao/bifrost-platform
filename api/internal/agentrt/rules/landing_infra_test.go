package rules

import (
	"strings"
	"testing"
)

const infraRuleSection = `  - repo: bifrost-trade-infra
    allow: ["agent-config/work/multi-agent/REPORT-*.md"]
    deny: []
    stg_release: none
    limits: {max_files: 5, max_changed_lines: 1500, max_in_flight: 1}
`

func TestS6T72EffectiveRepoLimits(t *testing.T) {
	t.Parallel()
	text := strings.Replace(landingYAML, "limits:\n", infraRuleSection+"limits:\n", 1)
	text = strings.Replace(text, "deny: []", "deny: [\"console/src/pages/Private.tsx\"]", 1)
	text = strings.Replace(text, "stg_release: bifrost-deliver-platform", "stg_release: none", 1)
	parse := func(text string) Rule {
		t.Helper()
		r, err := Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	old := parse(text)
	if old.Landing.EffectiveLimits("bifrost-platform").MaxChangedLines != 300 || old.Landing.EffectiveLimits("bifrost-trade-infra").MaxChangedLines != 1500 {
		t.Fatal("inheritance")
	}
	b, err := old.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	round := parse(string(b))
	b2, _ := round.BodyJSON()
	if string(b) != string(b2) {
		t.Fatal("unstable rule body")
	}
	for _, tc := range []struct {
		from, to string
		tight    bool
	}{
		{"max_changed_lines: 1500", "max_changed_lines: 1000", true},
		{"max_changed_lines: 300", "max_changed_lines: 400", false},
		{"deny: [\"console/src/pages/Private.tsx\"]", "deny: []", false},
		{"stg_release: none", "stg_release: bifrost-deliver-platform", false},
	} {
		next := parse(strings.Replace(text, tc.from, tc.to, 1))
		if err := Tightens(old, next); (err == nil) != tc.tight {
			t.Fatalf("%s -> %s: %v", tc.from, tc.to, err)
		}
	}
	if err := Tightens(landingRule(t), old); err == nil {
		t.Fatal("adding infra is expansion requiring signature")
	}
	if _, err := Parse([]byte(strings.Replace(text, "repo: bifrost-trade-infra", "repo: bifrost-research", 1))); err == nil {
		t.Fatal("unknown repo")
	}
}
