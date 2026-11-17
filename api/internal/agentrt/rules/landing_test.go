package rules

import (
	"strings"
	"testing"
	"time"
)

const landingYAML = `rule_id: landing:dev
rule_version: 1
direction: initial
effective_at: 2026-10-24T00:00:00Z
expires_at: 2026-11-23T00:00:00Z
tenant: dev
repos:
  - repo: bifrost-platform
    allow: ["console/src/components/**/*.tsx", "console/src/components/**/*.css", "console/src/pages/**/*.tsx", "console/src/styles/**/*.css"]
    deny: []
    stg_release: bifrost-deliver-platform
limits:
  max_files: 10
  max_changed_lines: 300
  max_landings_per_day: 5
  max_in_flight_per_repo: 1
`

func landingRule(t *testing.T) Rule {
	t.Helper()
	r, e := Parse([]byte(landingYAML))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestLandingRuleClosedAndCanonical(t *testing.T) {
	r := landingRule(t)
	b, e := r.BodyJSON()
	if e != nil {
		t.Fatal(e)
	}
	round, e := Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	b2, _ := round.BodyJSON()
	if string(b) != string(b2) {
		t.Fatal("unstable canonical body")
	}
	for _, text := range []string{landingYAML + "extra: true\n", strings.Replace(landingYAML, "deny: []", "deny: []\n    extra: true", 1), strings.Replace(landingYAML, "max_files: 10", "max_files: -1", 1), strings.Replace(landingYAML, "max_files: 10", "max_files: '10'", 1), strings.Replace(landingYAML, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 9", 1), strings.Replace(landingYAML, "tenant: dev", "tenant: business", 1), strings.Replace(landingYAML, "repo: bifrost-platform", "repo: bifrost-trade-infra", 1), strings.Replace(landingYAML, "deny: []", "deny: [../api/**]", 1), strings.Replace(landingYAML, "deny: []", "deny: [a**b]", 1)} {
		if _, e := Parse([]byte(text)); e == nil {
			t.Errorf("accepted bad rule %s", text)
		}
	}
}
func TestLandingTightening(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Rule)
		tight  bool
	}{
		{"repo removed", func(r *Rule) { r.Landing.Repos = nil }, true},
		{"allow removed", func(r *Rule) { r.Landing.Repos[0].Allow = nil }, true},
		{"allow added", func(r *Rule) { r.Landing.Repos[0].Allow = append(r.Landing.Repos[0].Allow, "**") }, false},
		{"deny added", func(r *Rule) { r.Landing.Repos[0].Deny = []string{"console/src/pages/**"} }, true},
		{"limits down", func(r *Rule) { r.Landing.Limits = LandingLimits{} }, true},
		{"files up", func(r *Rule) { r.Landing.Limits.MaxFiles++ }, false},
		{"lines up", func(r *Rule) { r.Landing.Limits.MaxChangedLines++ }, false},
		// The daily quota is legacy (cleanup wave 1): raising it loosens nothing.
		{"quota up", func(r *Rule) { r.Landing.Limits.LegacyMaxLandingsPerDay++ }, true},
		{"flight up", func(r *Rule) { r.Landing.Limits.MaxInFlightPerRepo++ }, false},
		{"stg off", func(r *Rule) { r.Landing.Repos[0].STGRelease = "none" }, true},
		{"expiry shorter", func(r *Rule) { r.Expires = r.Expires.Add(-time.Hour) }, true},
		{"renewal", func(r *Rule) { r.Expires = r.Expires.Add(time.Hour) }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, n := landingRule(t), landingRule(t)
			c.mutate(&n)
			if e := Tightens(p, n); (e == nil) != c.tight {
				t.Fatalf("tight=%v: %v", c.tight, e)
			}
		})
	}
	p, n := landingRule(t), landingRule(t)
	p.Landing.Repos[0].Deny = []string{"console/src/pages/**"}
	if Tightens(p, n) == nil {
		t.Fatal("removed deny tightens")
	}
	p = landingRule(t)
	p.Landing.Repos[0].STGRelease = "none"
	if Tightens(p, n) == nil {
		t.Fatal("enable STG tightens")
	}
}

func TestLandingRetiredSizeAndCapacity(t *testing.T) {
	t.Parallel()
	for _, body := range []string{strings.Replace(strings.Replace(landingYAML, "rule_version: 1", "rule_version: 10", 1), "direction: initial", "direction: loosen", 1), strings.ReplaceAll(strings.ReplaceAll(landingYAML, "  max_files: 10\n", ""), "  max_changed_lines: 300\n", "")} {
		r, err := Parse([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if limits := r.Landing.CheckedLimits("bifrost-platform"); limits.MaxFiles != 0 || limits.MaxChangedLines != 0 {
			t.Fatal(limits)
		}
	}
	for _, field := range []string{"max_in_flight_per_repo", "max_in_flight"} {
		base := landingYAML
		if field == "max_in_flight" {
			base = strings.Replace(base, "limits:\n", infraRuleSection+"limits:\n", 1)
		}
		for _, n := range []string{"0", "1", "2", "8", "9"} {
			_, err := Parse([]byte(strings.Replace(base, field+": 1", field+": "+n, 1)))
			if (err == nil) != (n != "0" && n != "9") {
				t.Fatalf("%s=%s: %v", field, n, err)
			}
		}
	}
}
