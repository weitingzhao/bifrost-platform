package server

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/catalog"
	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// Who uses each package, for the map (2026-11-21, Foundation's ask): the
// packages of the table's repository read the way the domain graph reads them
// (unitplan.ReadGoModule over the same Gitea tree, the table's domain of each
// package), with their test imports; and the bus registry's events between
// domains. Read once per commit: the module read walks every Go file.

var catalogUsesCache struct {
	sync.Mutex
	repo, commit string
	uses         catalog.Uses
}

func (s *Server) catalogUses(ctx context.Context, repo string) (catalog.Uses, bool, error) {
	g, err := s.ciDomainsKept.Graph(ctx, s.ciDomainsSourceOf(), repo)
	if err != nil {
		return catalog.Uses{}, false, err
	}
	if !g.Measured {
		return catalog.Uses{Commit: g.Commit}, false, nil
	}
	catalogUsesCache.Lock()
	defer catalogUsesCache.Unlock()
	if catalogUsesCache.repo != repo || catalogUsesCache.commit != g.Commit {
		pkgs, err := s.catalogPackages(ctx, repo, g.Commit)
		if err != nil {
			return catalog.Uses{Commit: g.Commit}, false, err
		}
		catalogUsesCache.repo, catalogUsesCache.commit = repo, g.Commit
		catalogUsesCache.uses = catalog.Uses{Commit: g.Commit, Base: g.Base, Packages: pkgs}
	}
	u := catalogUsesCache.uses
	u.Events, u.EventsMeasured = catalog.RegistryEvents()
	return u, true, nil
}

func (s *Server) catalogPackages(ctx context.Context, repo, commit string) ([]catalog.GoPackage, error) {
	t, err := s.unitTreesOf()(ctx, repo, commit)
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
