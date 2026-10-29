package rules

import "testing"

func TestLandingHardLayersAndEndpoints(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"api/internal/agentrt/store/zz_new.go", "api/internal/agentrt/store/sub/testdata/a",
		"api/internal/server/x.go", "console/src/pages/ApprovalsPage.tsx",
		"mcp/platform/src/x.ts", "config/platform-auth.yaml", "api/internal/statefile/x.go",
		".gitattributes", "deep/.gitattributes", "deep/Dockerfile.dev", "deep/package.json",
		"console/src/pwa/new.ts", "console/src/pages/landing-grant/New.tsx",
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
	for _, p := range []string{"api/internal/research/x.go", "api/internal/serverish/x.go", "console/src/pages/ApprovalsPage.tsx.extra", "x/.github/a", "x/Dockerfiles/a"} {
		if LandingHard("bifrost-platform", p) {
			t.Fatal("overbroad hard match", p)
		}
	}
	if !LandingHardAbsolute("unknown", "README.md") || !LandingHardAbsolute("bifrost-trade-infra", "REPORT.md") {
		t.Fatal("unknown repository opened")
	}
}
