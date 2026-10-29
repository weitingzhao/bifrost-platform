package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
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

// The lineage trailers a handed-in commit carries (landinggit/replay.go
// reads them the same way when it lands).
var (
	agentrtTaskTrailer = regexp.MustCompile(`(?im)^Agentrt-Task:\s*(\S+)\s*$`)
	changeIDTrailer    = regexp.MustCompile(`(?im)^Change-Id:\s*(\S+)\s*$`)
	changeIDShape      = regexp.MustCompile(`^I[0-9a-f]{40}$`)
)

// pinnedRead is what the read before the write found: whether the task is
// pinned and, if it is, the commit.
type pinnedRead struct {
	pinned bool
	facts  CommitFacts
}

// readPinned is the forge half for a pinned task, before the write's
// transaction (as checkHead is): the commit the result names, read in the
// task's repository. Without a reader a pinned result waits (ErrNoHead).
func (r Result) readPinned(db DB, attemptID string, commits CommitReader, out *pinnedRead) func(context.Context) error {
	return func(ctx context.Context) error {
		var pin, repo string
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT COALESCE(t.executor_pin, ''), COALESCE(t.repo, '') FROM agentrt.attempt a
			  JOIN agentrt.task t ON t.task_id = a.task_id WHERE a.attempt_id = $1`, attemptID).Scan(&pin, &repo)
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrFenced
		}
		if err != nil || pin == "" {
			return err
		}
		out.pinned = true
		if r.Ref == "" || r.Commit == "" {
			return fmt.Errorf("%w: a pinned task's result is a pushed branch", ErrState)
		}
		if commits == nil {
			return ErrNoHead
		}
		if out.facts, err = commits(ctx, repo, r.Commit); err != nil {
			return fmt.Errorf("store: read commit %s of %s: %w", r.Commit, repo, err)
		}
		return nil
	}
}

// checkPinnedResult checks a pinned task's result in the write's
// transaction, the attempt locked, and returns what the result's event
// records of it; nil for a task that is not pinned.
func checkPinnedResult(ctx context.Context, tx pgx.Tx, a attemptRow, r Result, read pinnedRead) (map[string]any, error) {
	var pin, repo, base, tenant string
	var core bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(t.executor_pin, ''), COALESCE(t.repo, ''), COALESCE(t.base_commit, ''), t.core, m.tenant
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id WHERE t.task_id = $1`, a.task).
		Scan(&pin, &repo, &base, &core, &tenant); err != nil {
		return nil, err
	}
	if pin == "" {
		return nil, nil
	}
	if !read.pinned {
		return nil, ErrNoHead // read before the pin was seen: never, as pins do not change; refused all the same
	}
	f := read.facts
	switch {
	case r.Ref != AttemptRef(a.task, a.no):
		return nil, fmt.Errorf("%w: attempt %s pushes only to %s", ErrState, a.id, AttemptRef(a.task, a.no))
	case len(f.Parents) != 1:
		return nil, fmt.Errorf("%w: commit %s has %d parents; a pinned result is one commit on the task's base", ErrState, r.Commit, len(f.Parents))
	case f.Parents[0] != base:
		return nil, fmt.Errorf("%w: commit %s is on %s, not on the task's base %s: one commit, on the base", ErrState, r.Commit, f.Parents[0], base)
	}
	tasks := agentrtTaskTrailer.FindAllStringSubmatch(f.Message, -1)
	changes := changeIDTrailer.FindAllStringSubmatch(f.Message, -1)
	switch {
	case len(tasks) != 1 || tasks[0][1] != a.task:
		return nil, fmt.Errorf("%w: commit %s names its task in %d Agentrt-Task trailers; it needs exactly one, Agentrt-Task: %s",
			ErrState, r.Commit, len(tasks), a.task)
	case len(changes) != 1 || !changeIDShape.MatchString(changes[0][1]):
		return nil, fmt.Errorf("%w: commit %s carries %d Change-Id trailers; it needs exactly one, I and 40 hex digits", ErrState, r.Commit, len(changes))
	case len(f.Paths) == 0:
		return nil, fmt.Errorf("%w: commit %s changes no path", ErrState, r.Commit)
	}
	h, err := entryOf(ctx, tx, tenant, pin, a.host)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(h.Repos, repo) {
		return nil, fmt.Errorf("%w: the envelope's entry for %s no longer lists %s", ErrState, pin, repo)
	}
	hard := false
	for _, p := range f.Paths {
		if !core && underAny(p, Repos[repo].Core) {
			return nil, fmt.Errorf("%w: commit %s changes %s, core work in %s, and task %s is not core: propose it again as core",
				ErrCoreMismatch, r.Commit, p, repo, a.task)
		}
		hard = hard || rules.LandingHard(repo, p)
	}
	return map[string]any{"changed_lines": f.Lines, "hard": hard}, nil
}

// underAny says whether repository path p is under one of dirs.
func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(p, strings.TrimSuffix(d, "/")+"/") {
			return true
		}
	}
	return false
}

// pinnedCheck is the read before the write of a result: the branch head
// (checkHead) and, for a pinned task, the commit (readPinned).
func (r Result) pinnedCheck(db DB, attemptID string, head BranchHead, commits CommitReader, out *pinnedRead) func(context.Context) error {
	checkHead, readPinned := r.checkHead(db, attemptID, head), r.readPinned(db, attemptID, commits, out)
	return func(ctx context.Context) error {
		if err := checkHead(ctx); err != nil {
			return err
		}
		return readPinned(ctx)
	}
}

// withPinned is a result's event body with what checkPinnedResult recorded.
func withPinned(body, pinned map[string]any) map[string]any {
	for k, v := range pinned {
		body[k] = v
	}
	return body
}

// PinnedReviewer is who reviews a pinned task (§2.8): Codex, of openai, in
// the first version; the coordinator's own provider is anthropic, and a
// candidate whose provider label cannot show it is not (cursor-mix) is not
// one.
var PinnedReviewer = Tool{Executor: "exec:codex", Provider: "openai"}
