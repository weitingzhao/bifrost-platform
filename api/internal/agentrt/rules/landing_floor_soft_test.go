package rules

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// floor_soft is a category grants and recorded refusals still name, but the
// soft refusal set never produces it: on platform every floor path that is
// neither hard nor open has a floor entry (floor_grant), and on infra every
// floor path is hard. Checked over the platform tree and each table's entries.
func TestLandingSoftRefusalsNeverFloorSoft(t *testing.T) {
	t.Parallel()
	platform := []string{"api/x.go", "api/Makefile", "console/vite.config.ts", "console/tsconfig.json", "console/tsconfig.app.tsbuildinfo", "console/index.html",
		"console/src/main.tsx", "console/src/App.tsx", "console/src/api/x.ts", "console/src/lib/x.ts", "console/src/hooks/x.ts", "console/src/pwa/x.ts",
		"mcp/README.md", "mcp/platform/x.go", "agent/x", "scripts/x.sh", "config/x.yaml", "Makefile", ".husky/pre-commit", "go.mod", "x/go.sum",
		"x/package.json", "x/package-lock.json", "x/.env", "console/src/pages/X.tsx", "README.md", "x/Dockerfile.dev", ".github/workflows/x.yml"}
	root := repositoryRoot(t)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			platform = append(platform, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(platform, "api/internal/agentrt/rules/landing.go") {
		t.Fatal("platform tree not walked")
	}
	var infra []string
	for _, entry := range append(slices.Clone(infraFloor), infraNever...) {
		p := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(entry, "**/", "x/"), "**", "y/z"), "*", "a")
		infra = append(infra, p, strings.TrimPrefix(p, "x/"))
	}
	infra = append(infra, "agent-config/work/x.md", "agent-config/work/multi-agent/CUT-a.md", "README.md", "src/x.py")
	l := &Landing{Repos: []LandingRepo{{Repo: "bifrost-platform", Allow: []string{"**"}}, {Repo: "bifrost-trade-infra", Allow: []string{"**"}}}}
	for repo, paths := range map[string][]string{"bifrost-platform": platform, "bifrost-trade-infra": infra} {
		for _, p := range paths {
			c := LandingChange{OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1}
			if slices.Contains(LandingSoftRefusals(l, repo, []LandingChange{c}), LandingFloorSoft) {
				t.Errorf("%s %s: floor_soft", repo, p)
			}
		}
	}
}
