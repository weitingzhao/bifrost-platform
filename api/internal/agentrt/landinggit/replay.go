package landinggit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var ErrOverlap = errors.New("overlap")
var ErrConflict = errors.New("conflict")
var ErrPatchChanged = errors.New("patch_changed")
var taskTrailer = regexp.MustCompile(`(?im)^Agentrt-Task:\s*(\S+)\s*$`)
var changeTrailer = regexp.MustCompile(`(?im)^Change-Id:\s*(I[0-9a-f]{40})\s*$`)

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
	changed, e := w.local(ctx, "diff", "--name-only", "-z", "--no-renames", in.Base, in.A, "--")
	if e != nil {
		return "", e
	}
	drift, e := w.local(ctx, "diff", "--name-only", "-z", "--no-renames", in.Base, in.M0, "--")
	if e != nil {
		return "", e
	}
	paths := map[string]bool{}
	for _, p := range strings.Split(changed, "\x00") {
		if p != "" {
			paths[p] = true
		}
	}
	for _, p := range strings.Split(drift, "\x00") {
		if paths[p] {
			return "", ErrOverlap
		}
	}
	if _, e = w.local(ctx, "checkout", "--detach", in.M0); e != nil {
		return "", e
	}
	if _, e = w.local(ctx, "cherry-pick", "--no-commit", in.A); e != nil {
		return "", ErrConflict
	}
	original, e := w.local(ctx, "diff", "--no-ext-diff", "--no-textconv", "--binary", in.Base, in.A, "--")
	if e != nil {
		return "", e
	}
	replay, e := w.local(ctx, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--binary", in.M0, "--")
	if e != nil {
		return "", e
	}
	a, e := w.git.run(ctx, w.dir, false, []byte(original), nil, "patch-id", "--stable")
	if e != nil {
		return "", e
	}
	b, e := w.git.run(ctx, w.dir, false, []byte(replay), nil, "patch-id", "--stable")
	if e != nil {
		return "", e
	}
	if len(strings.Fields(a)) == 0 || len(strings.Fields(b)) == 0 || strings.Fields(a)[0] != strings.Fields(b)[0] {
		return "", ErrPatchChanged
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
