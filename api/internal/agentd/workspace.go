package agentd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The attempt's workspace (design v5 §5.2 「工作区」): a fresh clone of the
// task's repository at its base commit, in <root>/tasks/<task>/a<n>; the
// checkout must be clean (rule H3). Git runs with argument arrays only, with
// the environment agentd gives it (Gitea credentials come through
// GIT_CONFIG_* variables, never through arguments a process listing shows).

// ErrWorkspaceExists: the attempt's workspace is already there. An attempt
// never runs twice (rule R2): a workspace left by a previous run of agentd is
// for salvage, not for a new run.
var ErrWorkspaceExists = errors.New("agentd: the attempt's workspace already exists")

// Workspace is one attempt's checkout.
type Workspace struct {
	Dir  string
	Base string
	env  []string
}

// WorkspaceDir is where attempt n of task keeps its checkout.
func WorkspaceDir(root, task string, n int) string {
	return filepath.Join(root, "tasks", task, fmt.Sprintf("a%d", n))
}

// Prepare clones remote into a new workspace and checks out base, detached.
func Prepare(ctx context.Context, dir, remote, base string, env []string) (*Workspace, error) {
	if _, err := os.Stat(dir); err == nil {
		return nil, ErrWorkspaceExists
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	w := &Workspace{Dir: dir, Base: base, env: env}
	if _, err := w.git(ctx, filepath.Dir(dir), "clone", "--quiet", "--no-checkout", remote, dir); err != nil {
		return nil, err
	}
	if _, err := w.git(ctx, dir, "checkout", "--quiet", "--detach", base); err != nil {
		return nil, err
	}
	if dirty, err := w.Dirty(ctx); err != nil || dirty {
		if err == nil {
			err = errors.New("agentd: the checkout is not clean (H3)")
		}
		return nil, err
	}
	head, err := w.git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if head != base {
		return nil, fmt.Errorf("agentd: checked out %s, not the base %s", head, base)
	}
	return w, nil
}

// Dirty reports changes against the checkout, untracked files included.
func (w *Workspace) Dirty(ctx context.Context) (bool, error) {
	out, err := w.git(ctx, w.Dir, "status", "--porcelain", "--untracked-files=all")
	return out != "", err
}

// Diff is the patch of the workspace against the base, untracked files
// included (a checkpoint, §5.2 检查点); the index is left as it was.
func (w *Workspace) Diff(ctx context.Context) ([]byte, error) {
	idx := filepath.Join(w.Dir, ".git", "agentd-checkpoint-index")
	defer func() { _ = os.Remove(idx) }()
	env := append(append([]string{}, w.env...), "GIT_INDEX_FILE="+idx)
	if _, err := gitIn(ctx, w.Dir, env, "read-tree", w.Base); err != nil {
		return nil, err
	}
	if _, err := gitIn(ctx, w.Dir, env, "add", "--all", "--", "."); err != nil {
		return nil, err
	}
	out, err := gitIn(ctx, w.Dir, env, "diff", "--cached", "--binary", w.Base)
	return []byte(out), err
}

// Lineage is what a commit agentd pushes carries (design v5 §3 lineage row):
// the task, a Change-Id and the work it belongs to.
type Lineage struct {
	Task, ChangeID, Work string
}

// NewChangeID is a fresh Change-Id.
func NewChangeID() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "I" + hex.EncodeToString(b), nil
}

// Commit records every change in the workspace against the base as one
// commit with the lineage trailers and returns it; "" when nothing differs
// from the base. Commits the executor made itself are folded into it (their
// content kept, review 163 A3), so every commit agentd pushes carries the
// trailers.
func (w *Workspace) Commit(ctx context.Context, subject string, l Lineage) (string, error) {
	if l.Task == "" || l.ChangeID == "" || l.Work == "" {
		return "", errors.New("agentd: a commit carries Agentrt-Task, Change-Id and Work")
	}
	if w.Base == "" {
		return "", errors.New("agentd: the workspace's base is not known")
	}
	head, err := w.git(ctx, w.Dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if head != w.Base {
		if _, err = w.git(ctx, w.Dir, "reset", "--quiet", "--soft", w.Base); err != nil {
			return "", err
		}
	}
	dirty, err := w.Dirty(ctx)
	if err != nil || !dirty {
		return "", err
	}
	if _, err = w.git(ctx, w.Dir, "add", "--all", "--", "."); err != nil {
		return "", err
	}
	if _, err = w.git(ctx, w.Dir, "commit", "--quiet", "--no-verify", "-m", subject,
		"--trailer", "Agentrt-Task: "+l.Task, "--trailer", "Change-Id: "+l.ChangeID, "--trailer", "Work: "+l.Work); err != nil {
		return "", err
	}
	return w.git(ctx, w.Dir, "rev-parse", "HEAD")
}

// Push pushes commit to ref on remote (the attempt's own branch) and
// returns what the remote's ref then names.
func (w *Workspace) Push(ctx context.Context, remote, commit, ref string) (string, error) {
	if !strings.HasPrefix(ref, "refs/heads/agent/") {
		return "", fmt.Errorf("agentd: %s is not an attempt branch", ref)
	}
	if _, err := w.git(ctx, w.Dir, "push", "--quiet", remote, commit+":"+ref); err != nil {
		return "", err
	}
	out, err := w.git(ctx, w.Dir, "ls-remote", remote, ref)
	if err != nil {
		return "", err
	}
	head, _, _ := strings.Cut(out, "\t")
	return head, nil
}

func (w *Workspace) git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitIn(ctx, dir, w.env, args...)
}

func gitIn(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = dir, env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("agentd: git %s: %v: %s", args[0], err, strings.TrimSpace(tail(errb.Bytes())))
	}
	return strings.TrimSpace(out.String()), nil
}
