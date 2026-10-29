package landing

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"reflect"
	"testing"
	"time"
)

func TestReview704InfraSoftLayerPrecedesDependencies(t *testing.T) {
	for _, which := range []string{"path", "size"} {
		t.Run(which, func(t *testing.T) {
			f := infraFacts()
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
				t.Fatal("successful control", d)
			}
			if which == "path" {
				f.Changes[0].OldPath = "agent-config/work/multi-agent/PLAN-x.md"
				f.Changes[0].NewPath = f.Changes[0].OldPath
			} else {
				f.Changes[0].Added = 1501
			}
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "floor" {
				t.Fatal("control", d)
			}
			f.DependenciesEffective = false
			want := "size"
			if which == "path" {
				want = "rule_paths"
			}
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "floor" || !reflect.DeepEqual(d.Detail["soft"], []string{want}) || d.Detail["grant_supported"] != false {
				t.Fatalf("L4/L5 rejection was masked: %+v", d)
			}
			f = infraFacts()
			f.DependenciesEffective = false
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "dependencies" {
				t.Fatal("valid policy must still wait", d)
			}
			f = eligibleFacts()
			f.DependenciesEffective = false
			f.Changes[0].Added = 3001
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "dependencies" {
				t.Fatal("platform order changed", d)
			}
		})
	}
}

func TestReview704InfraCeilingBeyondSignedLimits(t *testing.T) {
	for _, which := range []string{"files", "lines", "tight-files", "tight-lines"} {
		t.Run(which, func(t *testing.T) {
			f := infraFacts()
			f.Rule.Landing.Repos[0].Limits = &rules.LandingRepoLimits{MaxFiles: 10, MaxChangedLines: 3000, MaxInFlight: 1}
			if which == "files" || which == "tight-files" {
				f.Changes = nil
				for n := 0; n < 5; n++ {
					f.Changes = append(f.Changes, rules.LandingChange{NewPath: fmt.Sprintf("agent-config/work/multi-agent/REPORT-%d.md", n), NewMode: "100644", Added: 1})
				}
			} else {
				f.Changes[0].Added = 1500
			}
			if which == "tight-files" {
				f.Rule.Landing.Repos[0].Limits.MaxFiles = 3
				f.Changes = f.Changes[:3]
			}
			if which == "tight-lines" {
				f.Rule.Landing.Repos[0].Limits.MaxChangedLines = 1000
				f.Changes[0].Added = 1000
			}
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
				t.Fatal("control", d)
			}
			if which == "files" || which == "tight-files" {
				f.Changes = append(f.Changes, rules.LandingChange{NewPath: "agent-config/work/multi-agent/REPORT-last.md", NewMode: "100644", Added: 1})
			} else {
				f.Changes[0].Added++
			}
			if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Reason != "floor" || !reflect.DeepEqual(d.Detail["soft"], []string{"size"}) {
				t.Fatalf("immutable infra ceiling lost: %+v", d)
			}
		})
	}
}
