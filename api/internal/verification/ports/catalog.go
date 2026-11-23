package ports

import (
	"context"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/catalog"
	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// For the catalog's map of who uses each package (server/catalog_uses_wire.go):
// the packages of a repository read the way the domain graph reads them.

// CatalogPackages are the Go packages of repo at commit, read from trees:
// each package's directory, its domain in the commit's ownership table, and
// the directories its code and its tests import. The modules are the Go
// modules of the unit declaration (bifrost.units.yaml), else the table's Go
// roots that have a go.mod. Moved out of server (catalog_uses_wire.go,
// catalogPackages) on 2026-11-23, unchanged.
func CatalogPackages(ctx context.Context, trees unitexec.TreesFunc, repo, commit string) ([]catalog.GoPackage, error) {
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return nil, err
	}
	raw, err := t.ReadFile(cidomains.OwnershipPath)
	if err != nil {
		return nil, err
	}
	table, err := cidomains.ParseTable(raw)
	if err != nil {
		return nil, err
	}
	var roots []string
	if doc, err := t.ReadFile(unitplan.ManifestPath); err == nil {
		if m, err := unitplan.ParseManifest(doc); err == nil {
			for _, mod := range m.Modules {
				if mod.Kind == "go" {
					roots = append(roots, strings.Trim(mod.Root, "/"))
				}
			}
		}
	}
	if len(roots) == 0 {
		for _, root := range table.GoRoots() {
			if _, ok := t.Lookup(root + "/go.mod"); ok {
				roots = append(roots, root)
			}
		}
	}
	var out []catalog.GoPackage
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mod, err := unitplan.ReadGoModule(t, root)
		if err != nil {
			return nil, err
		}
		dirOf := func(ips []string) []string {
			dirs := []string{}
			for _, ip := range ips {
				if p := mod.Packages[ip]; p != nil {
					dirs = append(dirs, p.Dir)
				}
			}
			return dirs
		}
		for _, p := range mod.Packages {
			d, _ := table.Ownership.DomainOf(p.Dir)
			out = append(out, catalog.GoPackage{Dir: p.Dir, Domain: d, Imports: dirOf(p.Imports), TestImports: dirOf(p.TestImports)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}
