package rules

import (
	"slices"
	"strings"
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
	if !LandingHardAbsolute("unknown", "README.md") {
		t.Fatal("unknown repository opened")
	}
}

// Open paths (checks version 3) need neither core approval nor an allow
// match. Deny still applies, and a permission path in the same
// change still asks for core.
func TestLandingOpenRefusals(t *testing.T) {
	t.Parallel()
	change := func(p string) LandingChange {
		c := simpleChange()
		c.OldPath, c.NewPath = p, p
		return c
	}
	open := []string{
		"api/internal/agentrt/controller/loop.go", "api/internal/agentrt/questioncards/x_test.go", "api/internal/delivery/new.go",
		"api/cmd/agentrt-gate/main.go", "api/Makefile", "console/vitest.config.ts", "console/tsconfig.json",
		"console/tsconfig.node.json", "console/tsconfig.app.json", "api/internal/agentrt/store/progressneeds.go",
	}
	for _, p := range open {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			if !LandingOpen("bifrost-platform", p) || LandingHard("bifrost-platform", p) {
				t.Fatal("not open", p)
			}
			l := landingRule(t).Landing
			l.Repos[0].Allow = nil
			if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{change(p)}); len(got) != 0 {
				t.Fatal("open-only change refused", got)
			}
			l.Repos[0].Deny = []string{p}
			if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{change(p)}); !slices.Equal(got, []LandingRefusal{LandingRulePaths}) {
				t.Fatal("deny", got)
			}
			l.Repos[0].Deny = nil
			big := change(p)
			big.Added = 400
			if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{big}); len(got) != 0 {
				t.Fatal("size", got)
			}
			got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{change(p), change("api/internal/agentrt/landing/x.go")})
			if !slices.Contains(got, LandingCore) {
				t.Fatal("open plus permission", got)
			}
		})
	}
	// A rename from an open path into allow is checked for display on its
	// allowed endpoint only.
	l := landingRule(t).Landing
	c := simpleChange()
	c.OldPath = "api/internal/agentrt/controller/loop.go"
	if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); len(got) != 0 {
		t.Fatal("open rename into allow", got)
	}
	c.AddedText = "fetch('/api')"
	if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); !slices.Equal(got, []LandingRefusal{LandingPresentation}) {
		t.Fatal("display of the allowed endpoint", got)
	}
	for _, p := range []string{
		"api/internal/agentrt/controllerx/a.go", "console/tsconfig.json.bak", "console/src/tsconfig.json", "deep/api/Makefile",
		"api/internal/agentrt/controller/Dockerfile", "api/internal/agentrt/controller/package.json", ".github/a",
	} {
		if LandingOpen("bifrost-platform", p) {
			t.Error("overbroad open match", p)
		}
	}
	if LandingOpen("bifrost-trade-infra", "api/internal/agentrt/controller/loop.go") || LandingOpen("unknown", "api/Makefile") {
		t.Error("open outside bifrost-platform")
	}
}

// The store is permission unless a file directly under it is on the open
// map: an unlisted file, a test file and any subdirectory are permission.
func TestLandingStoreSplit(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"api/internal/agentrt/store/zz_unlisted.go", "api/internal/agentrt/store/progressneeds_test.go",
		"api/internal/agentrt/store/sub/progressneeds.go", "api/internal/agentrt/store/signals.go",
		"api/internal/agentrt/store/landing_grant.go", "api/internal/agentrt/store/landing_new.go",
		"api/internal/agentrt/store/pinreview.go", "api/internal/agentrt/store/nomerge.go",
		"api/internal/agentrt/store/stg_gate.go", "api/internal/agentrt/store/controls.go",
		"api/internal/agentrt/store/stepexplain.go", "api/internal/agentrt/store/progress.go",
		"Makefile", "console/src/api/questions.ts",
	} {
		if !LandingHardCore("bifrost-platform", p) || LandingOpen("bifrost-platform", p) {
			t.Errorf("not permission: %s", p)
		}
		l := landingRule(t).Landing
		c := simpleChange()
		c.OldPath, c.NewPath = p, p
		if got := LandingSoftRefusals(l, "bifrost-platform", []LandingChange{c}); !slices.Contains(got, LandingCore) {
			t.Errorf("%s: %v", p, got)
		}
	}
	for name := range landingStoreOpen {
		if key, open := landingStoreEntry(landingStore + name); !open || key != name {
			t.Errorf("open store file shadowed by permission entry %q: %s", key, name)
		}
		if _, listed := landingStorePermission[name]; listed {
			t.Errorf("store file in both maps: %s", name)
		}
	}
	for p := range landingOpen {
		probe := strings.ReplaceAll(p, "*", ".x")
		if strings.HasSuffix(probe, "/") {
			probe += "x.go"
		}
		if !LandingOpen("bifrost-platform", probe) {
			t.Errorf("open entry %s is shadowed by a permission entry", p)
		}
	}
}
