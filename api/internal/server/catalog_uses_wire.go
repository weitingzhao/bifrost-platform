package server

import (
	"context"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/catalog"
	verificationports "github.com/weitingzhao/bifrost-platform/api/internal/verification/ports"
)

// Who uses each package, for the map (2026-11-21, Foundation's ask): the
// packages of the table's repository read the way the domain graph reads them
// (verification/ports.CatalogPackages over the same Gitea tree, the table's
// domain of each package), with their test imports; and the bus registry's events between
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
		pkgs, err := verificationports.CatalogPackages(ctx, s.unitTreesOf(), repo, g.Commit)
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
