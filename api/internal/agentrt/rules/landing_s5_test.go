package rules

import (
	"strings"
	"testing"
)

const s5YAML = `rule_id: landing:dev
rule_version: 3
direction: loosen
effective_at: 2026-10-29T00:00:00Z
expires_at: 2026-11-28T00:00:00Z
tenant: dev
repos:
  - repo: bifrost-platform
    allow:
      - console/src/components/**/*.tsx
      - console/src/components/**/*.css
      - console/src/pages/**/*.tsx
      - console/src/styles/**/*.css
      - console/src/lib/patrol/cronSchedule.ts
      - console/src/lib/research/signalHealthAgeMeters.ts
      - console/src/lib/research/researchHealthCopy.ts
      - console/src/lib/market-data/queueHistoryModel.ts
    deny: []
    stg_release: none
limits:
  max_files: 10
  max_changed_lines: 300
  max_landings_per_day: 12
  max_in_flight_per_repo: 1
`

func TestS5V3RuleParseTightenAndLegacyFloor(t *testing.T) {
	r, err := Parse([]byte(s5YAML))
	if err != nil {
		t.Fatal(err)
	}
	if !S5Active(r.Landing, "bifrost-platform") {
		t.Fatal("inactive")
	}
	body := s5YAML
	for _, p := range S5Paths() {
		if LandingHard("bifrost-platform", p) {
			t.Fatal("hard candidate", p)
		}
		c := LandingChange{Kind: "M", OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1}
		if err := r.Landing.CheckChanges("bifrost-platform", []LandingChange{c}); err == nil {
			t.Fatal("legacy floor waived", p)
		}
		body = strings.ReplaceAll(body, "      - "+p+"\n", "")
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "rule_version: 3", "rule_version: 4"), "direction: loosen", "direction: tighten")
	n, err := Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := Tightens(r, n); err != nil {
		t.Fatal(err)
	}
	if S5Active(n.Landing, "bifrost-platform") {
		t.Fatal("tighten retains S5")
	}
	if err := Tightens(n, r); err == nil {
		t.Fatal("reopen is tightening")
	}
	if _, err := Parse([]byte(s5YAML + "content_profile: trusted\n")); err == nil {
		t.Fatal("open schema")
	}
}
