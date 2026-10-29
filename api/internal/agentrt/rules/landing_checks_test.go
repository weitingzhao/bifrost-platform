package rules

import (
	"reflect"
	"strings"
	"testing"
)

func TestStructuralViolationEndpoints(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*LandingChange){
		"symlink":               func(c *LandingChange) { c.NewMode = "120000" },
		"submodule":             func(c *LandingChange) { c.OldMode = "160000" },
		"binary":                func(c *LandingChange) { c.Binary = true },
		"negative added":        func(c *LandingChange) { c.Added = -1 },
		"negative deleted":      func(c *LandingChange) { c.Deleted = -1 },
		"mode only":             func(c *LandingChange) { c.NewMode = "100755"; c.Added = 0; c.Deleted = 0 },
		"empty":                 func(c *LandingChange) { c.OldPath = ""; c.NewPath = ""; c.OldMode = ""; c.NewMode = "" },
		"mode without endpoint": func(c *LandingChange) { c.OldPath = "" },
		"traversal":             func(c *LandingChange) { c.NewPath = "../x" },
		"absolute":              func(c *LandingChange) { c.NewPath = "/x" },
		"unclean":               func(c *LandingChange) { c.NewPath = "a/./x" },
		"control":               func(c *LandingChange) { c.NewPath = "a/\tx" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := simpleChange()
			if why, bad := StructuralViolation([]LandingChange{c}); bad {
				t.Fatal("control:", why)
			}
			mutate(&c)
			if _, bad := StructuralViolation([]LandingChange{c}); !bad {
				t.Fatal("structural violation accepted")
			}
		})
	}
	for _, kind := range []string{"add", "delete", "modify", "rename", "copy"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			c := simpleChange()
			want := 1
			switch kind {
			case "add":
				c.OldPath, c.OldMode = "", ""
			case "delete":
				c.NewPath, c.NewMode = "", ""
			case "rename", "copy":
				c.NewPath = "console/src/components/New.tsx"
				want = 2
			}
			if why, bad := StructuralViolation([]LandingChange{c}); bad || len(c.Paths()) != want {
				t.Fatal(c, why)
			}
		})
	}
}

func TestLandingSoftRefusalsIndependentCategories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		want   LandingRefusal
		mutate func(*Landing, *LandingChange)
	}{
		{"floor", LandingFloorSoft, func(l *Landing, c *LandingChange) {
			l.Repos[0].Allow = []string{"**"}
			c.OldPath, c.NewPath = "api/internal/research/x.go", "api/internal/research/x.go"
		}},
		{"core", LandingCore, func(l *Landing, c *LandingChange) {
			l.Repos[0].Allow = []string{"**"}
			c.OldPath, c.NewPath = "api/internal/agentrt/store/new.go", "api/internal/agentrt/store/new.go"
		}},
		{"allow", LandingRulePaths, func(l *Landing, c *LandingChange) { l.Repos[0].Allow = nil }},
		{"deny", LandingRulePaths, func(l *Landing, c *LandingChange) { l.Repos[0].Deny = []string{"**"} }},
		{"presentation", LandingPresentation, func(l *Landing, c *LandingChange) { c.AddedText = "fetch('/api')" }},
		{"files", LandingSize, func(l *Landing, c *LandingChange) { l.Limits.MaxFiles = 0 }},
		{"lines", LandingSize, func(l *Landing, c *LandingChange) { l.Limits.MaxChangedLines = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, c := landingRule(t).Landing, simpleChange()
			if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); len(got) != 0 {
				t.Fatal("control:", got)
			}
			tc.mutate(l, &c)
			if why, bad := StructuralViolation([]LandingChange{c}); bad {
				t.Fatal("masked by structure", why)
			}
			if why, bad := LandingAbsoluteViolation("bifrost-platform", []LandingChange{c}); bad {
				t.Fatal("masked by absolute", why)
			}
			got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})
			if !reflect.DeepEqual(got, []LandingRefusal{tc.want}) {
				t.Fatal(got, tc.want)
			}
		})
	}
}

func TestLandingSoftSetRecomputedAndCovered(t *testing.T) {
	t.Parallel()
	l, c := landingRule(t).Landing, simpleChange()
	c.OldPath, c.NewPath = "api/internal/research/x.go", "api/internal/research/x.go"
	l.Repos[0].Allow = []string{"**"}
	approved := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})
	if !LandingWaivesCover(approved, approved) {
		t.Fatal("control")
	}
	l.Limits.MaxChangedLines = 1
	now := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})
	if LandingWaivesCover(now, approved) {
		t.Fatal("new size failure waived")
	}
	if !LandingWaivesCover(nil, approved) {
		t.Fatal("relaxed rule rejected")
	}
	if LandingWaivesCover([]LandingRefusal{LandingCore}, approved) || LandingWaivesCover(nil, []LandingRefusal{"unknown"}) {
		t.Fatal("invalid waiver")
	}
	c.OldPath, c.NewPath = "api/internal/agentrt/store/x.go", "api/internal/agentrt/store/x.go"
	l.Repos[0].Allow = nil
	c.AddedText = "fetch('/x')" // Outside allow: paths, not presentation.
	got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})
	if !reflect.DeepEqual(got, []LandingRefusal{LandingCore, LandingRulePaths, LandingSize}) {
		t.Fatal(got)
	}
	c.Added, c.Deleted = int(^uint(0)>>1), int(^uint(0)>>1)
	if !strings.Contains(string(LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})[2]), "size") {
		t.Fatal("overflow")
	}
}

func TestLandingMixedCoreAndSoftEndpoints(t *testing.T) {
	t.Parallel()
	l, c := landingRule(t).Landing, simpleChange()
	l.Repos[0].Allow = []string{"**"}
	if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); len(got) != 0 {
		t.Fatal("control", got)
	}
	c.OldPath, c.NewPath = "api/internal/research/x.go", "api/internal/agentrt/store/x.go"
	got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})
	if !reflect.DeepEqual(got, []LandingRefusal{LandingCore, LandingFloorSoft}) {
		t.Fatal(got)
	}
}

// T26 is an inner capability check here. HTTP grant creation belongs to C-5;
// using an infra diff here would be masked by the earlier absolute hard gate.
func TestGrantSupported(t *testing.T) {
	t.Parallel()
	for _, repo := range []string{"bifrost-trade-infra", "unknown", ""} {
		if !GrantSupported("bifrost-platform") {
			t.Fatal("control")
		}
		if GrantSupported(repo) {
			t.Fatal("unsupported grant", repo)
		}
	}
}
