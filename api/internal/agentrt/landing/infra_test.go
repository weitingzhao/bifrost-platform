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
		// A floor-grantable path that is deleted or renamed is still refused:
		// infra changes are additions and modifications only.
		{"T2 delete hard", "change_kind", "", func(f *Facts) {
			f.Changes[0].OldPath = "k8s/base-platform/manifest.yaml"
			f.Changes[0].NewPath = ""
			f.Changes[0].NewMode = ""
		}},
		{"T3 rename into governance", "change_kind", "", func(f *Facts) {
			f.Changes = []rules.LandingChange{{OldPath: infraDoc, OldMode: "100644", Deleted: 20}, {NewPath: "agent-config/scripts/x.md", NewMode: "100644", Added: 20}}
		}},
		{"T4 skill inside allow", "hard", "", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/reviews/SKILL.md", NewMode: "100644", Added: 20}}
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

// (a) A change of floor-grantable paths alone waits for approve_floor_landing
// where grants are enabled, instead of being refused; without the entry, or
// with an unwaivable failure beside it, it is refused with its floor paths.
func TestInfraFloorGrantableWaits(t *testing.T) {
	t.Parallel()
	manifest := "k8s/overlays/prod/kustomization.yaml"
	for _, tc := range []struct {
		name string
		edit func(*Facts)
		soft []rules.LandingRefusal
	}{
		{"manifest", func(f *Facts) { f.Changes[0].OldPath, f.Changes[0].NewPath = manifest, manifest }, []rules.LandingRefusal{rules.LandingFloorGrant, rules.LandingRulePaths}},
		{"new script", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: "scripts/release/x.sh", NewMode: "100644", Added: 3, AddedText: "echo\n"}}
		}, []rules.LandingRefusal{rules.LandingFloorGrant, rules.LandingRulePaths}},
		{"runbook inside allow", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: "agent-config/work/multi-agent/DESIGN-RUNBOOK-x.md", NewMode: "100644", Added: 20}}
		}, []rules.LandingRefusal{rules.LandingFloorGrant}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := infraFacts()
			f.ExecutorPin = "" // a floor grant needs no pinned root
			tc.edit(&f)
			soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
			if !reflect.DeepEqual(soft, tc.soft) {
				t.Fatalf("soft %v, want %v", soft, tc.soft)
			}
			if reason, _ := PolicyChecks(f, epoch.Add(time.Hour)); reason != "" || !MayAwaitGrant(f, soft) {
				t.Fatalf("floor-grantable change does not wait: %q", reason)
			}
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
				t.Fatalf("refused instead of waiting: %+v", d)
			}
			if d := EligibleToPush(f, epoch.Add(time.Hour)); d.Reason != "awaiting_grant" {
				t.Fatalf("pushable without a grant: %+v", d)
			}
			// No entry in the Owner's envelope: refused, its floor paths reported.
			f.GrantEnabled = false
			d := EligibleToClaim(f, epoch.Add(time.Hour))
			if d.Allowed || d.Reason != "floor" || d.Detail["floor_paths"] == nil {
				t.Fatalf("not refused with floor_paths: %+v", d)
			}
		})
	}
	unwaivable := []struct {
		name string
		edit func(*Facts)
	}{
		{"size beside floor_grant", func(f *Facts) {
			f.Changes[0].OldPath, f.Changes[0].NewPath, f.Changes[0].Added = manifest, manifest, 1501
		}},
		{"six files", func(f *Facts) {
			f.Changes = nil
			for i := 0; i < 6; i++ {
				f.Changes = append(f.Changes, rules.LandingChange{NewPath: fmt.Sprintf("k8s/x/%d.yaml", i), NewMode: "100644", Added: 1})
			}
		}},
		{"rule_paths outside the floor", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: manifest, NewMode: "100644", Added: 1}, {NewPath: "notes/x.md", NewMode: "100644", Added: 1}}
		}},
		{"floor path beside an allowed one", func(f *Facts) {
			f.Changes = []rules.LandingChange{{NewPath: manifest, NewMode: "100644", Added: 1}, {NewPath: infraDoc, NewMode: "100644", Added: 1}}
		}},
	}
	for _, tc := range unwaivable {
		t.Run(tc.name, func(t *testing.T) {
			f := infraFacts()
			tc.edit(&f)
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Allowed || d.Reason != "floor" {
				t.Fatalf("unwaivable change waits: %+v", d)
			}
		})
	}
}

// (b) Never-grantable paths and a Secret manifest are refused at the floor,
// before any grant could be asked for, whatever the envelope says.
func TestInfraNeverGrantableRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, layer string
		change      rules.LandingChange
	}{
		{"workflow", "hard", rules.LandingChange{NewPath: ".github/workflows/a.yml", NewMode: "100644", Added: 1}},
		{"gitea workflow", "hard", rules.LandingChange{NewPath: ".gitea/workflows/a.yml", NewMode: "100644", Added: 1}},
		{"CLAUDE.md", "hard", rules.LandingChange{OldPath: "CLAUDE.md", NewPath: "CLAUDE.md", OldMode: "100644", NewMode: "100644", Added: 1}},
		{"nested CLAUDE.md", "hard", rules.LandingChange{NewPath: "k8s/CLAUDE.md", NewMode: "100644", Added: 1}},
		{"env in k8s", "hard", rules.LandingChange{NewPath: "k8s/.env.prod", NewMode: "100644", Added: 1}},
		{"secret", "secret", rules.LandingChange{NewPath: "k8s/apps/db-secret.yaml", NewMode: "100644", Added: 4,
			AddedText: "apiVersion: v1\n  kind :   Secret  \nmetadata:\n  name: db\n"}},
		{"secret in a list", "secret", rules.LandingChange{NewPath: "k8s/apps/list.yaml", NewMode: "100644", Added: 3,
			AddedText: "items:\n- kind: \"Secret\"\n  metadata: {}\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := infraFacts()
			f.Changes = []rules.LandingChange{tc.change}
			if reason, soft := PolicyChecks(f, epoch.Add(time.Hour)); reason != "floor" || len(soft) != 0 {
				t.Fatalf("not floor: %q %v", reason, soft)
			}
			d := EligibleToClaim(f, epoch.Add(time.Hour))
			if d.Allowed || d.Reason != "floor" || d.Detail[tc.layer] == nil {
				t.Fatalf("wrong refusal: %+v", d)
			}
		})
	}
	// Control: the same manifest path without a Secret waits for a grant.
	f := infraFacts()
	f.Changes = []rules.LandingChange{{NewPath: "k8s/apps/db-secret.yaml", NewMode: "100644", Added: 2, AddedText: "kind: ConfigMap\n# kind: Secret is not declared\n"}}
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatalf("control: %+v", d)
	}
}
