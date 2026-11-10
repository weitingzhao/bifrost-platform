package landinggit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var ErrConflict = errors.New("conflict")
var taskTrailer = regexp.MustCompile(`(?im)^Agentrt-Task:\s*(\S+)\s*$`)
var changeTrailer = regexp.MustCompile(`(?im)^Change-Id:\s*(I[0-9a-f]{40})\s*$`)

// ConflictError is ErrConflict with what did not merge: the paths the
// change and main since its base both changed in the same lines, so the
// rework round is told where to look.
type ConflictError struct {
	M0    string
	Paths []string
	// InMain: the change merges onto M0 with nothing left to commit, because
	// main already has it (landed by another commit, such as a hand-merged
	// batch). Refused like a conflict so the root leaves the landing queue
	// instead of failing the whole batch on an empty commit (2026-11-10).
	InMain bool
}

func (c *ConflictError) Error() string {
	if c.InMain {
		return "nothing to replay onto " + c.M0 + ": main already has the change"
	}
	return "conflict onto " + c.M0 + ": " + strings.Join(c.Paths, ", ")
}
func (c *ConflictError) Is(target error) bool { return target == ErrConflict }

// conflict reads the unmerged paths of a failed cherry-pick onto m0 and
// leaves the workspace clean at m0 for the next replay in it.
func (w *Workspace) conflict(ctx context.Context, m0 string) error {
	c := &ConflictError{M0: m0, Paths: []string{}}
	if out, e := w.local(ctx, "diff", "--name-only", "-z", "--diff-filter=U"); e == nil {
		for _, p := range strings.Split(out, "\x00") {
			if p != "" && !slices.Contains(c.Paths, p) {
				c.Paths = append(c.Paths, p)
			}
		}
	}
	_, _ = w.local(ctx, "cherry-pick", "--abort")
	_, _ = w.local(ctx, "reset", "--hard", "-q", m0)
	return c
}

// ReplayInput's metadata comes from the accepted verification and claimed
// operation. On recovery it must be identical, including CommitterDate.
type ReplayInput struct {
	A, Base, M0, Task, Attempt, Mission, Reviewers, Operation string
	// Env is where the landing runs: "prod" for the live landing (S3),
	// "stg" or empty for the drill. It names the run in the message and
	// the Landed-By trailer (CUT §2.5 step 3).
	Env                       string
	RuleVersion               int
	AcceptedAt, CommitterDate time.Time
}

func (w *Workspace) Replay(ctx context.Context, in ReplayInput) (string, error) {
	if !fullSHA.MatchString(in.A) || !fullSHA.MatchString(in.Base) || !fullSHA.MatchString(in.M0) || in.CommitterDate.IsZero() || in.AcceptedAt.IsZero() {
		return "", errors.New("invalid replay input")
	}
	parent, e := w.local(ctx, "rev-list", "--parents", "-n", "1", in.A)
	if e != nil {
		return "", e
	}
	p := strings.Fields(parent)
	if len(p) != 2 || p[1] != in.Base {
		return "", errors.New("result is not one commit on base")
	}
	msg, e := w.local(ctx, "show", "-s", "--format=%B", in.A)
	if e != nil {
		return "", e
	}
	tasks, changes := taskTrailer.FindAllStringSubmatch(msg, -1), changeTrailer.FindAllStringSubmatch(msg, -1)
	if len(tasks) != 1 || tasks[0][1] != in.Task || len(changes) != 1 {
		return "", errors.New("result lineage does not match accepted task")
	}
	// A path changed both on main since the base and in the change is no
	// refusal: the cherry-pick is a three-way merge of the change onto M0,
	// and the landing CI tests what it gives. Only a real conflict refuses.
	if _, e = w.local(ctx, "checkout", "--detach", in.M0); e != nil {
		return "", e
	}
	if _, e = w.local(ctx, "cherry-pick", "--no-commit", in.A); e != nil {
		return "", w.conflict(ctx, in.M0)
	}
	// Nothing staged: main already has the change. An empty commit would
	// fail and drop the whole batch on every pass; refuse it instead.
	if _, e = w.local(ctx, "diff", "--cached", "--quiet"); e == nil {
		_, _ = w.local(ctx, "cherry-pick", "--abort")
		_, _ = w.local(ctx, "reset", "--hard", "-q", in.M0)
		return "", &ConflictError{M0: in.M0, Paths: []string{}, InMain: true}
	}
	author, e := w.local(ctx, "show", "-s", "--format=%an%x00%ae%x00%aI", in.A)
	if e != nil {
		return "", e
	}
	parts := strings.Split(strings.TrimSuffix(author, "\n"), "\x00")
	if len(parts) != 3 {
		return "", errors.New("invalid author")
	}
	// Insert provenance before the original trailing block so Work, Change-Id
	// and Agentrt-Task remain Git trailers, not body text (second draft §2.5.3).
	msg = strings.TrimRight(msg, "\n")
	split := strings.LastIndex(msg, "\n\n")
	if split < 0 {
		return "", errors.New("missing trailer block")
	}
	run, by := "STG rehearsal", "agentrt-stg/"
	if in.Env == "prod" {
		run, by = "PROD landing", "agentrt-prod/"
	}
	provenance := fmt.Sprintf("Landed by the agent runtime (%s) under landing:dev v%d: mission %s, task %s, attempt %s, reviewed by %s, accepted %s, replayed onto %s as %s.\n\n(cherry picked from commit %s)", run, in.RuleVersion, in.Mission, in.Task, in.Attempt, in.Reviewers, in.AcceptedAt.UTC().Format(time.RFC3339Nano), in.M0, in.Operation, in.A)
	msg = msg[:split] + "\n\n" + provenance + msg[split:] + "\nLanded-By: " + by + in.Operation + "\n"
	env := []string{"GIT_AUTHOR_NAME=" + parts[0], "GIT_AUTHOR_EMAIL=" + parts[1], "GIT_AUTHOR_DATE=" + parts[2], "GIT_COMMITTER_NAME=Agent Runtime", "GIT_COMMITTER_EMAIL=agentrt@bifrost.local", "GIT_COMMITTER_DATE=" + in.CommitterDate.UTC().Format(time.RFC3339)}
	if _, e = w.git.run(ctx, w.dir, false, []byte(msg), env, "commit", "--no-verify", "--cleanup=verbatim", "-F", "-"); e != nil {
		return "", e
	}
	s, e := w.local(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(s), e
}
