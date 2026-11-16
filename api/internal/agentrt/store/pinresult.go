package store

import (
	"context"
	"fmt"
)

// The result of a pinned task (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.5, §2.7). The coordinator
// hands in one commit it made itself, on the task's base, and pushes it to
// the branch the platform gave the attempt. Persist and salvage check it the
// same way, here, on what Gitea says the commit is (never what the host
// says): the branch is the attempt's and points at the commit (checkHead,
// fits), the commit has exactly one parent and it is the task's base, its
// message names the task in exactly one Agentrt-Task trailer and carries
// exactly one Change-Id, the repository is one the envelope's entry still
// lists, and whether the change is core work is the platform's reading of
// the diff: core work the task did not say it was is refused (core_mismatch;
// propose it again with core). Anything that does not hold refuses the
// result and changes nothing. What the diff weighs and whether it touches
// the trusted core is recorded with the result, for its review (accept.go).

// CommitFacts is what a commit is, as the forge reads it: its parents, its
// message, every path its diff against its parent names (both sides of a
// rename or a copy), and its added plus deleted lines.
type CommitFacts struct {
	Parents []string
	Message string
	Paths   []string
	Lines   int
}

// CommitReader reads a commit of a repository where agentd pushes.
type CommitReader func(ctx context.Context, repo, sha string) (CommitFacts, error)

// ErrCoreMismatch: a pinned result changes core work its task does not say
// it is; nothing was recorded.
var ErrCoreMismatch = fmt.Errorf("%w: core_mismatch", ErrState)
