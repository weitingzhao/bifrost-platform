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
	"sync"
	"time"
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
	return PrepareFetching(ctx, dir, remote, base, env, Fetch{})
}

// Fetch is where else a base may be: the branches Refspec names on Remote.
// A check's base is the commit the checked attempt pushed, which is on the
// repository agent branches go to, not on the one work is cloned from (a
// mirror of the main repository that takes no push).
type Fetch struct {
	Remote, Refspec string
}

// PrepareFetching is Prepare that, when base is not in the clone, fetches
// also's branches and looks again.
func PrepareFetching(ctx context.Context, dir, remote, base string, env []string, also Fetch) (*Workspace, error) {
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
	if _, err := w.git(ctx, dir, "cat-file", "-e", base+"^{commit}"); err != nil && also.Remote != "" {
		if _, err = w.git(ctx, dir, "fetch", "--quiet", "--no-tags", also.Remote, also.Refspec); err != nil {
			return nil, err
		}
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

// PrepareSibling makes dir, a repository beside an attempt's checkout, a fresh
// clone of remote at the head of branch ref and returns that commit. Whatever
// was at dir is removed first, never reused: an earlier attempt's setup ran
// unsandboxed and may have left hooks or another origin in its .git, which a
// fetch with the Gitea credentials would then run or follow (review 227 A1).
// A dir or a task directory that is a symbolic link is refused, and the clone
// reads neither the host user's global nor the system git configuration (the
// same setup could have written them) and installs no template hooks.
func PrepareSibling(ctx context.Context, dir, remote, ref string, env []string) (string, error) {
	if err := (Sibling{Repo: filepath.Base(dir), Ref: ref}).check(); err != nil {
		return "", fmt.Errorf("agentd: sibling: %w", err)
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	for _, p := range []string{parent, dir} {
		fi, err := os.Lstat(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return "", err
		case fi.Mode()&os.ModeSymlink != 0:
			return "", fmt.Errorf("agentd: sibling: %s is a symbolic link", p)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	isolated := append(append([]string{}, env...), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	w := &Workspace{Dir: dir, env: isolated}
	if _, err := w.git(ctx, parent, "clone", "--quiet", "--no-checkout", "--template=", "--single-branch", "--branch", ref, "--", remote, dir); err != nil {
		return "", err
	}
	if _, err := w.git(ctx, dir, "checkout", "--quiet", "--detach", "refs/remotes/origin/"+ref); err != nil {
		return "", err
	}
	return w.git(ctx, dir, "rev-parse", "HEAD")
}

// Dirty reports changes against the checkout, untracked files included.
func (w *Workspace) Dirty(ctx context.Context) (bool, error) {
	out, err := w.git(ctx, w.Dir, "status", "--porcelain", "--untracked-files=all")
	return out != "", err
}

// Diff is the patch of the workspace against the base, untracked files
// included (a checkpoint, §5.2 检查点): the net change, commits the executor
// made itself included (review 164 A6); the index is left as it was. A patch
// over limit is not read past it: tooLarge (review 164 B1).
func (w *Workspace) Diff(ctx context.Context, limit int) (patch []byte, tooLarge bool, err error) {
	idx := filepath.Join(w.Dir, ".git", "agentd-checkpoint-index")
	defer func() { _ = os.Remove(idx) }()
	env := append(append([]string{}, w.env...), "GIT_INDEX_FILE="+idx)
	if _, err = gitIn(ctx, w.Dir, env, "read-tree", w.Base); err != nil {
		return nil, false, err
	}
	if _, err = gitIn(ctx, w.Dir, env, "add", "--all", "--", "."); err != nil {
		return nil, false, err
	}
	// exec copies git's output into a bounded writer, so the wait for the
	// output, like the wait for git, ends WaitDelay after git ended or was
	// killed, even while a helper git started still holds stdout (review
	// 171 A1). A patch over the limit stops the copy and git.
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--binary", w.Base)
	cmd.Dir, cmd.Env, cmd.WaitDelay = w.Dir, env, gitWaitDelay
	errb := &ring{max: tailBytes}
	// stop reads cmd.Process when it is called: Start sets it before the
	// goroutine that copies the output starts.
	out := &capped{max: limit, stop: func() { _ = cmd.Process.Kill() }}
	cmd.Stdout, cmd.Stderr = out, errb
	if err = cmd.Start(); err != nil {
		return nil, false, err
	}
	err = cmd.Wait()
	if out.over() {
		return nil, true, nil
	}
	if err != nil {
		err = fmt.Errorf("agentd: git diff: %v: %s", err, strings.TrimSpace(string(errb.bytes())))
		if ctx.Err() != nil {
			err = fmt.Errorf("%w: %v", ctx.Err(), err)
		}
		return nil, false, err
	}
	return out.bytes(), false, nil
}

// errCapped: the output went over its limit.
var errCapped = errors.New("agentd: output over its limit")

// capped keeps up to max bytes; past that it calls stop (once) and refuses
// the rest.
type capped struct {
	mu   sync.Mutex
	max  int
	buf  []byte
	full bool
	stop func()
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.full || len(c.buf)+len(p) > c.max {
		if !c.full && c.stop != nil {
			c.stop()
		}
		c.full = true
		return 0, errCapped
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

func (c *capped) over() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.full
}

func (c *capped) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf
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

// gitWaitDelay bounds the wait for git's output once git itself has ended,
// or been killed when its context ended: a helper it started that still
// holds the pipes does not hold up agentd (review 165 A2).
const gitWaitDelay = 2 * time.Second

func gitIn(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, gitWaitDelay
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		// git cut short by its context is a step to try again, not a refusal
		// (review 165 A3).
		if cerr := ctx.Err(); cerr != nil {
			return "", fmt.Errorf("%w: agentd: git %s: %v", cerr, args[0], err)
		}
		return "", fmt.Errorf("agentd: git %s: %v: %s", args[0], err, strings.TrimSpace(tail(errb.Bytes())))
	}
	return strings.TrimSpace(out.String()), nil
}
