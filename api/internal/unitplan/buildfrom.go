package unitplan

import "context"

// BuildFrom computes the plan of a commit from its tree and its pins': trees
// reads the tree of a repository at a commit.
func BuildFrom(ctx context.Context, trees func(ctx context.Context, repo, commit string) (*Tree, error), repo, commit string) (*Plan, error) {
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return nil, err
	}
	return Build(repo, commit, t, Source{Pinned: func(pinned, at string) (*Tree, error) { return trees(ctx, pinned, at) }})
}
