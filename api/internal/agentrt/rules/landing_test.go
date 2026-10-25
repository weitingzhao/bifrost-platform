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
	for _, text := range []string{landingYAML + "extra: true\n", strings.Replace(landingYAML, "deny: []", "deny: []\n    extra: true", 1), strings.Replace(landingYAML, "max_files: 10", "max_files: -1", 1), strings.Replace(landingYAML, "max_files: 10", "max_files: '10'", 1), strings.Replace(landingYAML, "max_in_flight_per_repo: 1", "max_in_flight_per_repo: 2", 1), strings.Replace(landingYAML, "tenant: dev", "tenant: business", 1), strings.Replace(landingYAML, "repo: bifrost-platform", "repo: bifrost-trade-infra", 1), strings.Replace(landingYAML, "deny: []", "deny: [../api/**]", 1), strings.Replace(landingYAML, "deny: []", "deny: [a**b]", 1)} {
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
		{"quota up", func(r *Rule) { r.Landing.Limits.MaxLandingsPerDay++ }, false},
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
func simpleChange() LandingChange {
	return LandingChange{OldPath: "console/src/components/Button.tsx", NewPath: "console/src/components/Button.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<span>Ready</span>"}
}
func TestLandingFloorCannotBeSignedAway(t *testing.T) {
	l := landingRule(t).Landing
	l.Repos[0].Allow = []string{"**"}
	for _, p := range []string{"api/internal/actions/tier.go", "api/internal/server/x.go", "api/anything.txt", "go.mod", "nested/package.json", "x/.env.local", "console/vite.config.ts", "console/tsconfig.app.json", "console/index.html", "console/src/main.tsx", "console/src/App.tsx", "console/src/api/x.ts", "console/src/lib/x.ts", "console/src/hooks/x.ts", "console/src/pwa/x.ts", "mcp/a", "agent/a", "scripts/a", "config/a", "Makefile", ".husky/a"} {
		c := simpleChange()
		c.NewPath = p
		c.OldPath = p
		if e := l.CheckChanges("bifrost-platform", []LandingChange{c}); e == nil || !strings.Contains(e.Error(), "floor") {
			t.Errorf("floor allowed %s: %v", p, e)
		}
	}
}
func TestLandingDiffPathsAndPresentation(t *testing.T) {
	l := landingRule(t).Landing
	for _, p := range []string{"console/src/components/Button.tsx", "console/src/components/deep/more/Button.tsx", "console/src/styles/a.css"} {
		c := simpleChange()
		c.OldPath = p
		c.NewPath = p
		if e := l.CheckChanges("bifrost-platform", []LandingChange{c}); e != nil {
			t.Fatal(p, e)
		}
	}
	cases := map[string]func(*LandingChange){
		"rename old floor": func(c *LandingChange) { c.OldPath = "api/internal/actions/tier.go" },
		"symlink":          func(c *LandingChange) { c.NewMode = "120000" }, "submodule": func(c *LandingChange) { c.OldMode = "160000" },
		"binary": func(c *LandingChange) { c.Binary = true }, "mode only": func(c *LandingChange) { c.NewMode = "100755"; c.Added = 0; c.Deleted = 0 },
		"traversal": func(c *LandingChange) { c.NewPath = "console/src/components/../../../api/a" }, "case": func(c *LandingChange) { c.NewPath = "Console/src/components/Button.tsx" },
		"delete disallowed": func(c *LandingChange) { c.OldPath = "README.md"; c.NewPath = ""; c.NewMode = "" },
		"line sum":          func(c *LandingChange) { c.Added = 150; c.Deleted = 151 },
	}
	for _, text := range []string{"import {x} from '../api/x'", "import {x} from 'api/x'", "import {x} from '@/lib/x'", "import {\n x\n} from '../../hooks/x'", "fetch('/x')", "fetch ('/x')", "new XMLHttpRequest()", "useQuery()", "useMutation()", "window.location.href"} {
		text := text
		cases[text] = func(c *LandingChange) { c.AddedText = text }
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			c := simpleChange()
			mut(&c)
			if l.CheckChanges("bifrost-platform", []LandingChange{c}) == nil {
				t.Fatal("accepted unsafe diff")
			}
		})
	}
	c := simpleChange()
	c.NewPath = ""
	c.NewMode = ""
	c.Added = 0
	c.AddedText = ""
	if e := l.CheckChanges("bifrost-platform", []LandingChange{c}); e != nil {
		t.Fatal("allowed deletion", e)
	}
	l.Repos[0].Deny = []string{"console/src/components/**"}
	if l.CheckChanges("bifrost-platform", []LandingChange{simpleChange()}) == nil {
		t.Fatal("deny ignored")
	}
}
