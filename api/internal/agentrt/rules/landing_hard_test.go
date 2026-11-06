package rules

import (
	"slices"
	"testing"
)

func TestLandingHardLayersAndEndpoints(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"api/internal/agentrt/store/zz_new.go", "api/internal/agentrt/store/sub/testdata/a",
		"api/internal/server/x.go", "console/src/pages/ApprovalsPage.tsx",
		"mcp/platform/src/x.ts", "config/platform-auth.yaml", "api/internal/statefile/x.go",
		".gitattributes", "deep/.gitattributes", "deep/Dockerfile.dev", "deep/package.json",
		"console/src/pwa/new.ts", "console/src/pages/landing-grant/New.tsx",
		"api/internal/agentrt/store/repos.go", "api/internal/agentrt/store/landing_new.go", "api/internal/agentrt/store/pin_new_test.go",
		"api/cmd/agentd/main.go", "api/cmd/operator-plane/main.go", "Makefile", "console/src/api/cluster.ts",
	} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			for _, kind := range []string{"add", "delete", "rename into", "rename out", "copy"} {
				c := simpleChange()
				if LandingHard("bifrost-platform", c.NewPath) {
					t.Fatal("control")
				}
				switch kind {
				case "add":
					c.OldPath, c.OldMode, c.NewPath = "", "", p
				case "delete":
					c.NewPath, c.NewMode, c.OldPath = "", "", p
				case "rename into", "copy":
					c.NewPath = p
				case "rename out":
					c.OldPath = p
				}
				if why, bad := StructuralViolation([]LandingChange{c}); bad {
					t.Fatal("masked:", why)
				}
				if !LandingHardCore("bifrost-platform", p) || LandingHardAbsolute("bifrost-platform", p) {
					t.Fatal(p)
				}
				l := landingRule(t).Landing
				l.Repos[0].Allow = []string{"**"}
				got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c})
				if len(got) != 1 || got[0] != LandingCore {
					t.Fatal(kind, got)
				}
			}
		})
	}
	for _, p := range []string{".env", "nested/.env.secret", ".gitmodules", "nested/.gitmodules", ".github/workflows/ci.yaml"} {
		c := simpleChange()
		if why, bad := LandingAbsoluteViolation("bifrost-platform", []LandingChange{c}); bad {
			t.Fatal("control", why)
		}
		c.OldPath, c.NewPath = p, ""
		c.NewMode = ""
		if why, bad := StructuralViolation([]LandingChange{c}); bad {
			t.Fatal("masked:", why)
		}
		if !LandingHardAbsolute("bifrost-platform", p) || LandingHardCore("bifrost-platform", p) {
			t.Fatal(p)
		}
		if _, bad := LandingAbsoluteViolation("bifrost-platform", []LandingChange{c}); !bad {
			t.Fatal("old endpoint omitted")
		}
	}
	for _, p := range []string{"api/internal/research/x.go", "api/internal/serverish/x.go", "console/src/pages/ApprovalsPage.tsx.extra", "x/.github/a", "x/Dockerfiles/a",
		"api/internal/agentrt/controller/x.go", "api/cmd/agentrt-gate/main.go", "api/Makefile", "console/tsconfig.json", "api/internal/agentrt/store/progress.go"} {
		if LandingHard("bifrost-platform", p) {
			t.Fatal("overbroad hard match", p)
		}
	}
	if !LandingHardAbsolute("unknown", "README.md") {
		t.Fatal("unknown repository opened")
	}
}

func TestLandingOpenPaths(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"api/internal/agentrt/controller/x.go", "api/internal/agentrt/stgrelease/batch.go", "api/internal/questions/new/x.go",
		"api/cmd/debtimport/main.go", "api/Makefile", "console/vitest.config.ts", "console/tsconfig.node.json",
		"console/src/api/questions.ts", "console/src/pages/shell/releases/ReleasesPage.tsx",
		"api/internal/agentrt/store/progress.go", "api/internal/agentrt/store/progress_test.go",
		"api/internal/agentrt/store/planviewsnapshot_more_test.go", "api/internal/agentrt/store/presencejudge2_test.go",
	} {
		if !LandingOpen("bifrost-platform", p) || LandingHard("bifrost-platform", p) || LandingHardCoreEntry("bifrost-platform", p) != "" {
			t.Errorf("not open: %s", p)
		}
	}
	for _, p := range []string{
		"api/internal/agentrt/store/zz_new.go", "api/internal/agentrt/store/sub/progress.go",
		"api/internal/agentrt/store/progressfoo.go", "api/internal/agentrt/store/progressfoo_test.go",
		"api/internal/agentrt/store/landing_progress.go", "api/internal/agentrt/store/export_test.go",
		"api/internal/agentrt/controllerx/x.go", "console/tsconfig.app.json",
	} {
		if LandingOpen("bifrost-platform", p) {
			t.Errorf("open: %s", p)
		}
	}
	for _, p := range []string{"api/internal/agentrt/controller/x.go", "x/.env", "api/internal/agentrt/controller/.env"} {
		if LandingOpen("bifrost-trade-infra", p) || LandingOpen("unknown", p) || p != "api/internal/agentrt/controller/x.go" && LandingOpen("bifrost-platform", p) {
			t.Errorf("open outside bifrost-platform's ordinary paths: %s", p)
		}
	}
}

func TestLandingOpenRefusals(t *testing.T) {
	t.Parallel()
	change := func(p string) LandingChange {
		c := simpleChange()
		c.OldPath, c.NewPath = p, p
		return c
	}
	// Open changes need not be presentation-only.
	var open []LandingChange
	for _, p := range []string{"api/internal/agentrt/controller/x.go", "console/tsconfig.json", "api/internal/agentrt/store/progress.go"} {
		c := change(p)
		c.AddedText = "fetch('/x')"
		open = append(open, c)
	}
	for _, c := range []struct {
		name    string
		allow   []string
		deny    []string
		changes []LandingChange
		want    []LandingRefusal
	}{
		{"open only, allow empty", nil, nil, open, nil},
		{"open plus deny", nil, []string{"api/internal/agentrt/controller/**"}, open, []LandingRefusal{LandingRulePaths}},
		{"open plus oversize", nil, nil, append(slices.Clone(open), LandingChange{OldPath: "api/Makefile", NewPath: "api/Makefile", OldMode: "100644", NewMode: "100644", Added: 1000}), []LandingRefusal{LandingSize}},
		{"open plus permission", []string{"**"}, nil, append(slices.Clone(open), change("api/internal/agentrt/landing/x.go")), []LandingRefusal{LandingCore}},
		{"open plus unlisted store file", []string{"**"}, nil, append(slices.Clone(open), change("api/internal/agentrt/store/zz_new.go")), []LandingRefusal{LandingCore}},
		{"open renamed into a business path", nil, nil, []LandingChange{{OldPath: "api/internal/agentrt/controller/x.go", NewPath: "console/src/components/X.tsx", OldMode: "100644", NewMode: "100644", Added: 1}}, []LandingRefusal{LandingRulePaths}},
		{"open renamed into an allowed display path", []string{"console/src/components/**"}, nil, []LandingChange{{OldPath: "api/internal/agentrt/controller/x.go", NewPath: "console/src/components/X.tsx", OldMode: "100644", NewMode: "100644", Added: 1, AddedText: "fetch('/x')"}}, []LandingRefusal{LandingPresentation}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			l := landingRule(t).Landing
			l.Repos[0].Allow, l.Repos[0].Deny = c.allow, c.deny
			if got := LandingSoftRefusals(l, "bifrost-platform", c.changes); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
