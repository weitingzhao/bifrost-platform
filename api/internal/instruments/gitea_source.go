package instruments

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
)

// GiteaSource reads main through the same Gitea reader as the unit plan: its
// blob cache is by content id, so it cannot serve an older file. It lists a
// tree without the unit plan's reader, which would also fetch every Go file of
// the repository on a cold process; this answer needs a few dozen files.
type GiteaSource struct {
	Trees *gitread.Trees
	Head  func(ctx context.Context, repo string) (string, error)
}

// MainHead is the commit main of repo is at.
func (i GiteaSource) MainHead(ctx context.Context, repo string) (string, error) {
	return i.Head(ctx, repo)
}

// Files maps the path of every file of a commit to its blob id.
func (i GiteaSource) Files(ctx context.Context, repo, commit string) (map[string]string, error) {
	listing, err := i.Trees.Listing(ctx, repo, commit)
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(listing))
	for p, e := range listing {
		if e.Type == "blob" {
			files[p] = e.SHA
		}
	}
	return files, nil
}

// Blob is a file's bytes by blob id.
func (i GiteaSource) Blob(ctx context.Context, repo, id string) ([]byte, error) {
	return i.Trees.Blob(ctx, repo, id)
}
