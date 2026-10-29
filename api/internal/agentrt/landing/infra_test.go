package landing

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

const infraDoc = "agent-config/work/multi-agent/REPORT-a.md"

func infraFacts() Facts {
	f := eligibleFacts()
	f.Repo = "bifrost-trade-infra"
	f.GrantEnabled, f.ExecutorPin = true, "external-host"
	f.Rule.Landing.Repos = []rules.LandingRepo{{Repo: f.Repo, Allow: rules.InfraLandingAllow, STGRelease: "none", Limits: &rules.LandingRepoLimits{MaxFiles: 5, MaxChangedLines: 1500, MaxInFlight: 1}}}
	f.Changes = []rules.LandingChange{{OldPath: infraDoc, NewPath: infraDoc, OldMode: "100644", NewMode: "100644", Added: 20}}
	return f
}

// T1–T12: each negative reaches the same eligibility entry as its control.
// Hard precedes kind and allow; only one condition is changed in each pair.
func TestInfraEligibilityLayers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, layer, soft string
		edit              func(*Facts)
	}{
		{"T1 manifest", "hard", "", func(f *Facts) {
			f.Changes[0].OldPath = "k8s/overlays/prod/kustomization.yaml"
			f.Changes[0].NewPath = f.Changes[0].OldPath
		}},
		{"T2 delete hard", "hard", "", func(f *Facts) {
			f.Changes[0].OldPath = "k8s/base-platform/manifest.yaml"
			f.Changes[0].NewPath = ""
			f.Changes[0].NewMode = ""
		}},
		{"T3 rename into governance", "hard", "", func(f *Facts) {
			f.Changes = []rules.LandingChange{{OldPath: infraDoc, OldMode: "100644", Deleted: 20}, {NewPath: "agent-config/scripts/x.md", NewMode: "100644", Added: 20}}
		}},
		{"T4 skill inside allow", "hard", "", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/reviews/SKILL.md", NewMode: "100644", Added: 20}}
		}},
		{"T4 runbook inside allow", "hard", "", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/DESIGN-RUNBOOK-x.md", NewMode: "100644", Added: 20}}
		}},
		{"T5 executable", "structural", "", func(f *Facts) { f.Changes[0].NewMode = "100755" }},
		{"T6 symlink", "structural", "", func(f *Facts) { f.Changes[0].NewMode = "120000" }},
		{"T7 binary", "structural", "", func(f *Facts) { f.Changes[0].Binary = true }},
		{"T8 deletion", "change_kind", "", func(f *Facts) { f.Changes[0].NewPath = ""; f.Changes[0].NewMode = "" }},
		{"T9 rename inside allow", "change_kind", "", func(f *Facts) {
			f.Changes = []rules.LandingChange{{OldPath: infraDoc, OldMode: "100644", Deleted: 20}, {NewPath: "agent-config/work/multi-agent/REPORT-b.md", NewMode: "100644", Added: 20}}
		}},
		{"T10 not allowed", "soft", "rule_paths", func(f *Facts) {
			f.Changes[0].OldPath = "agent-config/work/multi-agent/PLAN-x.md"
			f.Changes[0].NewPath = f.Changes[0].OldPath
		}},
		{"T11 six files", "soft", "size", func(f *Facts) {
			for i := 1; i < 6; i++ {
				c := f.Changes[0]
				c.OldPath = fmt.Sprintf("agent-config/work/multi-agent/REPORT-%d.md", i)
				c.NewPath = c.OldPath
				f.Changes = append(f.Changes, c)
			}
		}},
		{"T12 1501 lines", "soft", "size", func(f *Facts) { f.Changes[0].Added = 1501 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := infraFacts()
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
				t.Fatalf("control: %+v", d)
			}
			tc.edit(&f)
			d := EligibleToClaim(f, epoch.Add(time.Hour))
			if d.Allowed || d.Reason != "floor" || d.Detail[tc.layer] == nil {
				t.Fatalf("wrong layer: %+v", d)
			}
			if tc.soft != "" && (!reflect.DeepEqual(d.Detail["soft"], []string{tc.soft}) || d.Detail["grant_supported"] != false) {
				t.Fatalf("wrong soft failure: %+v", d)
			}
			if MayAwaitGrant(f, rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)) {
				t.Fatal("infra may await grant")
			}
		})
	}
	for _, n := range []int{5, 6} {
		f := infraFacts()
		f.Changes = nil
		for i := 0; i < n; i++ {
			f.Changes = append(f.Changes, rules.LandingChange{NewPath: fmt.Sprintf("agent-config/work/multi-agent/REPORT-%d.md", i), NewMode: "100644", Added: 1})
		}
		if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed != (n == 5) {
			t.Fatal("file boundary", n)
		}
	}
	for _, n := range []int{1500, 1501} {
		f := infraFacts()
		f.Changes[0].Added = n
		if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed != (n == 1500) {
			t.Fatal("line boundary", n)
		}
	}
}

func TestInfraGrantBranchAndEntry(t *testing.T) {
	t.Parallel()
	f := eligibleFacts()
	f.ExecutorPin = "external-host"
	f.GrantEnabled = true
	f.Changes[0].Added = 301
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal("platform grant control", d)
	}
	f = infraFacts()
	f.Changes[0].Added = 1501
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "floor" {
		t.Fatal("T13", d)
	}
	f.Repo = "bifrost-research"
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "repo" {
		t.Fatal("T15", d)
	}
	f = infraFacts()
	f.Rule = nil
	f.Changes[0].NewMode = "120000"
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "rule" {
		t.Fatal("L0 precedes L1", d)
	}
}

func TestLiveReposNewSettingIsAuthoritative(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		values map[string]string
		want   []string
	}{
		{map[string]string{"AGENTRT_LANDING_LIVE": "on"}, []string{"bifrost-platform"}},
		{map[string]string{"AGENTRT_LANDING_LIVE": "on", "AGENTRT_LANDING_LIVE_REPOS": ""}, nil},
		{map[string]string{"AGENTRT_LANDING_LIVE_REPOS": "bifrost-platform"}, []string{"bifrost-platform"}},
		{map[string]string{"AGENTRT_LANDING_LIVE_REPOS": "bifrost-trade-infra,bifrost-platform,bifrost-research"}, []string{"bifrost-platform", "bifrost-trade-infra"}},
	} {
		got := LiveRepos(func(k string) (string, bool) { v, ok := tc.values[k]; return v, ok })
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("T62 %v -> %v", tc.values, got)
		}
	}
}
