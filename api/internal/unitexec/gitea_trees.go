package unitexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// GiteaTrees reads commits' trees from Gitea as unitplan reads them. The root
// id is computed from the root's entries (Gitea names no tree id for a commit,
// see gitread.CommitTree).
func GiteaTrees(g *gitread.Trees) TreesFunc {
	return func(ctx context.Context, repo, commit string) (*unitplan.Tree, error) {
		if !gitread.Name.MatchString(repo) || strings.HasSuffix(repo, ".git") || !gitread.CommitID.MatchString(commit) {
			return nil, fmt.Errorf("gitea: %q of %q is not a commit of a repository", commit, repo)
		}
		listing, err := g.Listing(ctx, repo, commit)
		if err != nil {
			return nil, err
		}
		var entries []unitplan.Entry
		var root []gitread.TreeEntry
		for p, e := range listing {
			if !strings.Contains(p, "/") {
				root = append(root, e)
			}
			if e.Type == "commit" {
				continue // a submodule: not content of this repository
			}
			entries = append(entries, unitplan.Entry{Path: p, Dir: e.Type == "tree", ID: e.SHA})
		}
		rootID, err := gitread.TreeID(root)
		if err != nil {
			return nil, err
		}
		g.Prefetch(ctx, repo, listing)
		return unitplan.NewTree(rootID, entries, func(id string) ([]byte, error) { return g.Blob(ctx, repo, id) })
	}
}
