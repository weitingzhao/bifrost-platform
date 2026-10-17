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

// PrepareSibling makes <root>/<rel>, a repository beside an attempt's
// checkout, a fresh clone of remote at the head of branch ref and returns that
// commit. Whatever was there is removed first, never reused: an earlier
// attempt's setup ran unsandboxed and may have left hooks or another origin in
// its .git, which a fetch with the Gitea credentials would then run or follow
// (review 227 A1). root is agentd's own directory, trusted; rel must stay in
// it. Every directory from root down to the sibling, and the sibling itself,
// must not be a symbolic link, and the removal and the directories made go
// through an os.Root on root, which refuses to leave it: a link the same
// setup put anywhere on the way cannot turn the removal onto something else
// (review 228 A1). The clone reads neither the host user's global nor the
// system git configuration (the setup could have written them) and installs
// no template hooks. Nothing else runs on this host while a sibling is
// prepared (the attempt before has ended and its process group is cleared;
// this attempt's has not started), so the checked path is the one cloned to.
func PrepareSibling(ctx context.Context, root, rel, remote, ref string, env []string) (string, error) {
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("agentd: sibling: %q is not a path inside agentd's directory", rel)
	}
	if err := (Sibling{Repo: filepath.Base(rel), Ref: ref}).check(); err != nil {
		return "", fmt.Errorf("agentd: sibling: %w", err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()
	parentRel := filepath.Dir(rel)
	if err = r.MkdirAll(parentRel, 0o700); err != nil {
		return "", fmt.Errorf("agentd: sibling: %w", err)
	}
	for p := rel; p != "."; p = filepath.Dir(p) {
		fi, lerr := r.Lstat(p)
		switch {
		case errors.Is(lerr, os.ErrNotExist) && p == rel:
		case lerr != nil:
			return "", fmt.Errorf("agentd: sibling: %w", lerr)
		case fi.Mode()&os.ModeSymlink != 0:
			return "", fmt.Errorf("agentd: sibling: %s is a symbolic link", filepath.Join(root, p))
		case p != rel && !fi.IsDir():
			return "", fmt.Errorf("agentd: sibling: %s is not a directory", filepath.Join(root, p))
		}
	}
	if err = r.RemoveAll(rel); err != nil {
		return "", fmt.Errorf("agentd: sibling: %w", err)
	}
	dir, parent := filepath.Join(root, rel), filepath.Join(root, parentRel)
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
	out, err := gitRaw(ctx, dir, env, args...)
	return strings.TrimSpace(out), err
}

// gitRaw is gitIn without trimming the output: for -z lists, whose first
// path may begin with a space (review 239 B4).
func gitRaw(ctx context.Context, dir string, env []string, args ...string) (string, error) {
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
	return out.String(), nil
}

// Changes lists what differs between the workspace and the base: changed
// are the paths git would commit (untracked files and the executor's own
// commits included, a rename as a deletion and an addition, review 239 A2);
// ignored are the untracked files and directories .gitignore hides, a
// directory once with its trailing slash (review 239 A1). Paths are read
// NUL-separated and untrimmed (review 239 B4).
func (w *Workspace) Changes(ctx context.Context) (changed, ignored []string, err error) {
	idx := filepath.Join(w.Dir, ".git", "agentd-changed-index")
	defer func() { _ = os.Remove(idx) }()
	env := append(append([]string{}, w.env...), "GIT_INDEX_FILE="+idx)
	if _, err = gitIn(ctx, w.Dir, env, "read-tree", w.Base); err != nil {
		return nil, nil, err
	}
	if _, err = gitIn(ctx, w.Dir, env, "add", "--all", "--", "."); err != nil {
		return nil, nil, err
	}
	out, err := gitRaw(ctx, w.Dir, env, "diff", "--cached", "--no-renames", "--name-only", "-z", w.Base)
	if err != nil {
		return nil, nil, err
	}
	changed = nulSplit(out)
	out, err = gitRaw(ctx, w.Dir, env, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, nil, err
	}
	return changed, nulSplit(out), nil
}

func nulSplit(out string) []string {
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// ExecutorConfig reports whether path is where an executor reads its own
// settings or standing instructions: a .cursor, .codex, .claude or .gemini
// directory at any depth, or cursor's and MCP's files at the top. An attempt
// that writes one is changing how agents behave, which no task asks for
// (2026-10-17, PROD M-3: a reviewer wrote .cursor/rules/allow-shell.mdc and
// cli.json to allow itself the shell).
func ExecutorConfig(path string) bool {
	for _, part := range strings.Split(path, "/") {
		switch part {
		case ".cursor", ".codex", ".claude", ".gemini":
			return true
		}
	}
	switch path {
	case "cli.json", ".cursorrules", ".cursorignore", ".mcp.json":
		return true
	}
	return false
}

// unwantedChanges says why the attempt must fail for what it changed in the
// checkout, or "" when it may stand: no attempt may change an executor's
// configuration, ignored files included, and a read-only one may change
// nothing git would commit (2026-10-17, PROD M-3: besides the two
// configuration files, the reviewer wrote scripts/agent/ls, a script named
// like an allowed command that ran a program of its own first). A check cut
// short by its context returns the error, to be tried again (review 239 B3);
// a checkout that cannot be read is a reason to fail.
func unwantedChanges(ctx context.Context, ws *Workspace, readOnly bool) (string, error) {
	changed, ignored, err := ws.Changes(ctx)
	if err != nil {
		if transient(err) {
			return "", err
		}
		return "the checkout could not be checked for what the executor changed: " + err.Error(), nil
	}
	if readOnly && len(changed) > 0 {
		return "an attempt that pushes no branch changed the checkout: " + listed(changed), nil
	}
	var bad []string
	for _, p := range append(changed, ignored...) {
		if ExecutorConfig(p) {
			bad = append(bad, p)
		}
	}
	if len(bad) == 0 {
		return "", nil
	}
	return "the executor changed an executor's own configuration: " + listed(bad), nil
}

// listed names up to ten paths and counts the rest.
func listed(paths []string) string {
	if len(paths) <= 10 {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(paths[:10], ", "), len(paths)-10)
}
