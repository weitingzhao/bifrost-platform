package rules

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestS66LegacySignedBodyGolden(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/landing-prod-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(raw)
	r, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	// Independent golden from the pre-S6-6 PROD v1 schema: no expanded repo
	// limits may be inserted into its canonical body or its signed bytes.
	if fmt.Sprintf("%x", sha256.Sum256(b)) != "0c7873210a83529b7a694ac6b3bdf6c4633836a6da4e66e243f17ef2c6808497" || !bytes.Equal(raw, original) {
		t.Fatalf("legacy signature material changed: %s", b)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "ddeacc2a9235816d1c1be1f3cf10ff0612902cda09a842034865e7363a3ee145" {
		t.Fatal("signed fixture changed")
	}
	if len(r.Landing.Repos) != 1 || r.Landing.Repos[0].Limits != nil || r.Landing.EffectiveLimits("bifrost-platform") != (LandingLimits{10, 300, 5, 1}) {
		t.Fatal("legacy rule expanded", r.Landing)
	}
	if _, ok := r.Landing.Repo("bifrost-trade-infra"); ok {
		t.Fatal("legacy rule authorizes infra")
	}
}

func TestS66EffectiveTighteningAcrossRepos(t *testing.T) {
	t.Parallel()
	base := strings.Replace(landingYAML, "limits:\n", infraRuleSection+"limits:\n", 1)
	parse := func(text string) Rule {
		t.Helper()
		r, err := Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, tc := range []struct {
		name   string
		change func(*Landing)
		tight  bool
	}{
		{"same", func(*Landing) {}, true},
		{"explicit inherited", func(l *Landing) { l.Repos[0].Limits = &LandingRepoLimits{10, 300, 1} }, true},
		{"files down", func(l *Landing) { l.Repos[1].Limits.MaxFiles-- }, true},
		{"files up", func(l *Landing) { l.Repos[1].Limits.MaxFiles++ }, false},
		{"lines down", func(l *Landing) { l.Repos[1].Limits.MaxChangedLines-- }, true},
		{"lines up", func(l *Landing) { l.Repos[1].Limits.MaxChangedLines++ }, false},
		{"flight down", func(l *Landing) { l.Repos[1].Limits.MaxInFlight = 0 }, true},
		{"remove override expands files", func(l *Landing) { l.Repos[1].Limits = nil }, false},
		{"quota down", func(l *Landing) { l.Limits.MaxLandingsPerDay-- }, true},
		{"quota up", func(l *Landing) { l.Limits.MaxLandingsPerDay++ }, false},
		{"allow shrinks", func(l *Landing) { l.Repos[1].Allow = nil }, true},
		{"allow expands", func(l *Landing) { l.Repos[1].Allow = append(l.Repos[1].Allow, "**") }, false},
		{"deny added", func(l *Landing) { l.Repos[1].Deny = []string{"agent-config/work/multi-agent/REPORT-private.md"} }, true},
		{"remove infra", func(l *Landing) { l.Repos = l.Repos[:1] }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, n := parse(base), parse(base)
			tc.change(n.Landing)
			if err := Tightens(p, n); (err == nil) != tc.tight {
				t.Fatal(tc.name, err)
			}
		})
	}
	p, n := parse(base), parse(base)
	// Defaults can rise when every remaining repository overrides them and
	// every effective value stays unchanged. Comparing raw defaults is wrong.
	for i := range p.Landing.Repos {
		v := p.Landing.EffectiveLimits(p.Landing.Repos[i].Repo)
		p.Landing.Repos[i].Limits = &LandingRepoLimits{v.MaxFiles, v.MaxChangedLines, v.MaxInFlightPerRepo}
		n.Landing.Repos[i].Limits = &LandingRepoLimits{v.MaxFiles, v.MaxChangedLines, v.MaxInFlightPerRepo}
	}
	n.Landing.Limits.MaxFiles = 99
	if err := Tightens(p, n); err != nil {
		t.Fatal("unchanged effective limits", err)
	}
	p.Landing.Repos[1].Limits.MaxInFlight = 0
	if Tightens(p, n) == nil {
		t.Fatal("per-repo flight raised")
	}
	p, n = parse(base), parse(base)
	p.Landing.Repos[1].Deny = []string{"agent-config/work/multi-agent/REPORT-private.md"}
	if Tightens(p, n) == nil {
		t.Fatal("deny removed")
	}
}

func TestS66ClosedRepoLimitsSchema(t *testing.T) {
	t.Parallel()
	base := strings.Replace(landingYAML, "limits:\n", infraRuleSection+"limits:\n", 1)
	if _, err := Parse([]byte(base)); err != nil {
		t.Fatal("same-entry control", err)
	}
	for _, bad := range []string{
		strings.Replace(base, "max_files: 5", "max_files: -1", 1),
		strings.Replace(base, "max_files: 5", "max_files: '5'", 1),
		strings.Replace(base, "max_files: 5, ", "", 1),
		strings.Replace(base, "max_in_flight: 1", "max_in_flight: 2", 1),
		strings.Replace(base, "max_in_flight: 1", "max_in_flight: 1, max_landings_per_day: 9", 1),
		strings.Replace(base, "repo: bifrost-trade-infra", "repo: unknown", 1),
		strings.Replace(base, "repo: bifrost-trade-infra", "repo: bifrost-platform", 1),
		strings.Replace(base, "stg_release: none", "stg_release: bifrost-deliver-platform", 1),
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Fatal("accepted invalid schema", bad)
		}
	}
}
