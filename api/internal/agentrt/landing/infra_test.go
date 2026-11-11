package landing

import (
	"fmt"
	"reflect"
	"slices"
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
		if !EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
			t.Fatal("file boundary", n)
		}
	}
	for _, n := range []int{1500, 1501} {
		f := infraFacts()
		f.Changes[0].Added = n
		if !EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
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
	f.Changes[0].OldPath, f.Changes[0].NewPath = "agent-config/work/multi-agent/PLAN-x.md", "agent-config/work/multi-agent/PLAN-x.md"
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

// Platform floor paths neither core nor open (testscope, scripts/ci) wait
// for approve_floor_landing; core keeps the core rule, and deny, an
// open path and a hard_absolute path are not floor-grantable.
func TestPlatformFloorGrantableWaits(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	platformFacts := func() Facts {
		f := eligibleFacts()
		f.GrantEnabled = true
		f.Changes = []rules.LandingChange{
			{OldPath: "api/internal/testscope/x.go", NewPath: "api/internal/testscope/x.go", OldMode: "100644", NewMode: "100644", Added: 3, Deleted: 1},
			{NewPath: "scripts/ci/y.sh", NewMode: "100644", Added: 2},
		}
		return f
	}
	f := platformFacts()
	soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
	if !reflect.DeepEqual(soft, []rules.LandingRefusal{rules.LandingFloorGrant, rules.LandingRulePaths}) {
		t.Fatalf("soft %v", soft)
	}
	if reason, _ := PolicyChecks(f, now); reason != "" || !MayAwaitGrant(f, soft) {
		t.Fatalf("floor-grantable change does not wait: %q", reason)
	}
	if d := EligibleToClaim(f, now); !d.Allowed {
		t.Fatalf("refused instead of waiting: %+v", d)
	}
	if d := EligibleToPush(f, now); d.Reason != "awaiting_grant" {
		t.Fatalf("pushable without a grant: %+v", d)
	}
	f.Rule.Landing.Repos[0].Allow = append(f.Rule.Landing.Repos[0].Allow, "api/**", "scripts/**")
	if soft = rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes); !reflect.DeepEqual(soft, []rules.LandingRefusal{rules.LandingFloorGrant}) || !MayAwaitGrant(f, soft) {
		t.Fatalf("inside allow: %v", soft)
	}
	for _, off := range []func(*Facts){
		func(f *Facts) { f.GrantEnabled = false },
		func(f *Facts) { f.Environment, f.Mode = "stg", "dry_run" },
	} {
		f := platformFacts()
		off(&f)
		if MayAwaitGrant(f, rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)) || EligibleToClaim(f, now).Allowed {
			t.Fatal("waits without grants in prod/live")
		}
	}
	// A core path keeps the core rule (approve_core_landing), pin or not.
	f = platformFacts()
	f.Changes = append(f.Changes, rules.LandingChange{OldPath: "api/internal/agentrt/rules/x.go", NewPath: "api/internal/agentrt/rules/x.go", OldMode: "100644", NewMode: "100644", Added: 1})
	soft = rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
	if !slices.Contains(soft, rules.LandingCore) || !slices.Contains(soft, rules.LandingFloorGrant) || rules.LandingFloorGrantable(f.Rule.Landing, f.Repo, f.Changes, soft) {
		t.Fatalf("core soft %v", soft)
	}
	if !MayAwaitGrant(f, soft) || !EligibleToClaim(f, now).Allowed {
		t.Fatal("core change lost approve_core_landing")
	}
	if f.GrantEnabled = false; MayAwaitGrant(f, soft) {
		t.Fatal("core waits without grants")
	}
	for name, edit := range map[string]func(*Facts){
		"deny": func(f *Facts) { f.Rule.Landing.Repos[0].Deny = []string{"scripts/ci/**"} },
	} {
		f := platformFacts()
		edit(&f)
		soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
		if !slices.Contains(soft, rules.LandingFloorGrant) || MayAwaitGrant(f, soft) {
			t.Fatalf("%s: waits (%v)", name, soft)
		}
		if d := EligibleToClaim(f, now); d.Allowed || d.Reason != "floor" {
			t.Fatalf("%s: %+v", name, d)
		}
	}
	// A floor path beside an allowed one is no floor grant's; a pinned root
	// still waits under platform's own rule, as it did with floor_soft.
	f = platformFacts()
	f.Rule.Landing.Repos[0].Allow = append(f.Rule.Landing.Repos[0].Allow, "api/**", "scripts/**")
	f.Changes = append(f.Changes, eligibleFacts().Changes[0])
	soft = rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
	if !reflect.DeepEqual(soft, []rules.LandingRefusal{rules.LandingFloorGrant}) || rules.LandingFloorGrantable(f.Rule.Landing, f.Repo, f.Changes, soft) || MayAwaitGrant(f, soft) {
		t.Fatalf("mixed unpinned: %v", soft)
	}
	if f.ExecutorPin = "external:coordinator"; !MayAwaitGrant(f, soft) {
		t.Fatal("pinned root lost its wait")
	}
	f = platformFacts()
	f.Changes = []rules.LandingChange{{NewPath: "api/internal/cluster/x.go", NewMode: "100644", Added: 1}}
	if soft = rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes); len(soft) != 0 || rules.LandingFloorGrantable(f.Rule.Landing, f.Repo, f.Changes, []rules.LandingRefusal{rules.LandingFloorGrant}) {
		t.Fatalf("open alone: %v", soft)
	}
	// hard_absolute is refused at the floor with the platform's event body.
	f = platformFacts()
	f.Changes = append(f.Changes, rules.LandingChange{NewPath: "scripts/.env.local", NewMode: "100644", Added: 1})
	if d := EligibleToClaim(f, now); d.Allowed || d.Reason != "floor" || d.Detail != nil {
		t.Fatalf("hard_absolute: %+v", d)
	}
	// A floor-grantable change refused at the floor reports its layer.
	f = platformFacts()
	f.Changes[1].Binary = true
	if d := EligibleToClaim(f, now); d.Allowed || d.Reason != "floor" || d.Detail["structural"] == nil {
		t.Fatalf("structural: %+v", d)
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
