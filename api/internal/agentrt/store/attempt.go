package store

import (
	"context"
	"errors"
)

// BranchHead reads the commit a branch of a repository on the main Git
// server points at. The HTTP layer passes a Gitea reader, tests a fake.
type BranchHead func(ctx context.Context, repo, ref string) (string, error)

// ErrNoHead: no reader of branch heads is configured, so a pushed branch
// cannot be checked yet; agentd tries again later.
var ErrNoHead = errors.New("store: no reader of branch heads is configured")
