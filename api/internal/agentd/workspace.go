package agentd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
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
	// wall, when the host has one, is the command git runs in once the
	// workspace is the executor's: git reads the checkout's .git/config and
	// hooks, which the walled executor can write (an fsmonitor, a filter, a
	// hook are commands), so agentd's own git there may write the checkout
	// only, like the executor (TD-371).
	wall []string
	// root is agentd's directory, through which agentd reaches the checkout
	// (openWithin) for what it does there itself (review 302).
	root string
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
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	return prepareIn(ctx, filepath.Dir(dir), dir, remote, base, env, also)
}

// prepareIn is PrepareFetching for a workspace inside root, agentd's own
// directory, as a sibling is made (review 277 A1): an attempt that ran
// before may have run unsandboxed and left a link on the way, or a git
// configuration and template hooks for the next clone to follow. So every
// directory from root down, and the workspace, must not be a symbolic link,
// the directories are made through an os.Root on root, and git reads neither
// the user's global nor the system configuration and installs no template.
func prepareIn(ctx context.Context, root, dir, remote, base string, env []string, also Fetch) (*Workspace, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("agentd: workspace %s is not inside %s", dir, root)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	if _, err = r.Lstat(rel); err == nil {
		return nil, ErrWorkspaceExists
	}
	if err = r.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
		return nil, fmt.Errorf("agentd: workspace: %w", err)
	}
	for p := filepath.Dir(rel); p != "."; p = filepath.Dir(p) {
		fi, lerr := r.Lstat(p)
		switch {
		case lerr != nil:
			return nil, fmt.Errorf("agentd: workspace: %w", lerr)
		case fi.Mode()&os.ModeSymlink != 0:
			return nil, fmt.Errorf("agentd: workspace: %s is a symbolic link", filepath.Join(root, p))
		case !fi.IsDir():
			return nil, fmt.Errorf("agentd: workspace: %s is not a directory", filepath.Join(root, p))
		}
	}
	w := &Workspace{Dir: dir, Base: base, env: isolatedGit(env)}
	if _, err := w.git(ctx, filepath.Dir(dir), "clone", "--quiet", "--no-checkout", "--template=", "--", remote, dir); err != nil {
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
// no template hooks. Nothing that may write outside its own workspace runs
// on this host while a sibling is prepared: an attempt that may has the host
// to itself, and one starts only when no process group of an earlier attempt
// stands (share.go, review 276 A2); so the checked path is the one cloned to.
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
	w := &Workspace{Dir: dir, env: isolatedGit(env)}
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
	g, err := w.gitDir()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = g.Close() }()
	idx := filepath.Join(w.Dir, ".git", "agentd-checkpoint-index")
	defer func() { _ = g.Remove("agentd-checkpoint-index") }()
	env := append(append([]string{}, w.env...), "GIT_INDEX_FILE="+idx)
	if _, err = w.gitEnv(ctx, env, "read-tree", w.Base); err != nil {
		return nil, false, err
	}
	if _, err = w.gitEnv(ctx, env, "add", "--all", "--", "."); err != nil {
		return nil, false, err
	}
	// exec copies git's output into a bounded writer, so the wait for the
	// output, like the wait for git, ends WaitDelay after git ended or was
	// killed, even while a helper git started still holds stdout (review
	// 171 A1). A patch over the limit stops the copy and git.
	cmd := gitCmd(ctx, w.wall, w.Dir, env, "diff", "--cached", "--binary", w.Base)
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
	out, err := runGit(ctx, w.wall, dir, w.env, args...)
	return strings.TrimSpace(out), err
}

// gitEnv is git in the workspace with env, inside its wall.
func (w *Workspace) gitEnv(ctx context.Context, env []string, args ...string) (string, error) {
	return runGit(ctx, w.wall, w.Dir, env, args...)
}

// gitCmd is the command that runs git args in dir, inside wall when there
// is one.
func gitCmd(ctx context.Context, wall []string, dir string, env []string, args ...string) *exec.Cmd {
	argv := append(append(slices.Clone(wall), "git"), args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, gitWaitDelay
	return cmd
}

// isolatedGit is env for git that reads neither the user's global nor the
// system configuration (an earlier run could have written them).
func isolatedGit(env []string) []string {
	return append(append([]string{}, env...), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
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
	return runGit(ctx, nil, dir, env, args...)
}

// runGit is gitRaw inside wall.
func runGit(ctx context.Context, wall []string, dir string, env []string, args ...string) (string, error) {
	cmd := gitCmd(ctx, wall, dir, env, args...)
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
// ignoredConfig are the untracked files .gitignore hides that are where an
// executor keeps its configuration, at any depth, an ignored parent
// directory included (review 239 A1, review 240 A1): git lists only the
// matches, so an ignored node_modules is walked, not printed. Paths are read
// NUL-separated and untrimmed (review 239 B4). Each call has an index of its
// own, removed with its lock, so a call cut short leaves nothing in the way
// of the next (review 240 B5).
func (w *Workspace) Changes(ctx context.Context) (changed, ignoredConfig []string, err error) {
	g, err := w.gitDir()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = g.Close() }()
	removeStaleIndexes(g)
	name, err := NewChangeID() // a unique name: git writes the index itself
	if err != nil {
		return nil, nil, err
	}
	name = "agentd-changed-index-" + name
	idx := filepath.Join(w.Dir, ".git", name)
	defer func() { _ = g.Remove(name); _ = g.Remove(name + ".lock") }()
	env := append(append([]string{}, w.env...), "GIT_INDEX_FILE="+idx)
	if _, err = w.gitEnv(ctx, env, "read-tree", w.Base); err != nil {
		return nil, nil, err
	}
	if _, err = w.gitEnv(ctx, env, "add", "--all", "--", "."); err != nil {
		return nil, nil, err
	}
	out, err := w.gitEnv(ctx, env, "diff", "--cached", "--no-renames", "--name-only", "-z", w.Base)
	if err != nil {
		return nil, nil, err
	}
	changed = nulSplit(out)
	// The configuration directories at any depth, ignored parents walked;
	// the top-level files with --directory, so that a directory of that
	// name is one line, not its contents (review 241 A1).
	var found []string
	for _, q := range [][]string{
		append([]string{"ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--"}, configDirPathspecs...),
		append([]string{"ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory", "--"}, configTopFiles...),
	} {
		out, err = w.gitEnv(ctx, env, q...)
		if err != nil {
			return nil, nil, err
		}
		found = append(found, nulSplit(out)...)
	}
	// git stops at a nested repository and names its directory: those, and
	// the directories among the changed paths (a nested repository added as
	// a gitlink), are walked here (review 241 A1).
	var walk []string
	for _, p := range found {
		p = strings.TrimSuffix(p, "/")
		info, lerr := os.Lstat(filepath.Join(w.Dir, p))
		switch {
		case lerr == nil && info.IsDir() && configDir(path.Base(p)):
			ignoredConfig = append(ignoredConfig, p)
		case lerr == nil && info.IsDir():
			walk = append(walk, p) // a nested repository, or a top-level directory named like a file
		case ExecutorConfig(p):
			ignoredConfig = append(ignoredConfig, p)
		}
	}
	for _, p := range changed {
		if info, lerr := os.Lstat(filepath.Join(w.Dir, p)); lerr == nil && info.IsDir() {
			walk = append(walk, p)
		}
	}
	for _, p := range walk {
		ignoredConfig = append(ignoredConfig, configUnder(w.Dir, p)...)
	}
	return changed, ignoredConfig, nil
}

// configDirPathspecs match ExecutorConfig's directories at any depth for
// git ("**/" matches the top as well), a file of that name included;
// configTopFiles are its files at the top.
var (
	configDirPathspecs = []string{
		":(glob)**/.cursor", ":(glob)**/.codex", ":(glob)**/.claude", ":(glob)**/.gemini",
		":(glob)**/.cursor/**", ":(glob)**/.codex/**", ":(glob)**/.claude/**", ":(glob)**/.gemini/**",
	}
	configTopFiles = []string{":(literal)cli.json", ":(literal).cursorrules", ":(literal).cursorignore", ":(literal).mcp.json"}
)

// configUnder walks dir (relative to root) for what ExecutorConfig names,
// without following symbolic links and skipping .git; a configuration
// directory is named once, not walked. A directory that cannot be read is
// named as it is, so that the attempt fails rather than passes unchecked.
func configUnder(root, dir string) []string {
	var hits []string
	_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if err != nil {
			hits = append(hits, rel+" (unreadable)")
			return nil
		}
		switch {
		case d.IsDir() && d.Name() == ".git":
			return fs.SkipDir
		case d.IsDir() && configDir(d.Name()):
			hits = append(hits, rel)
			return fs.SkipDir
		case !d.IsDir() && ExecutorConfig(rel):
			hits = append(hits, rel)
		}
		return nil
	})
	return hits
}

// removeStaleIndexes removes the indexes and locks a check left when its
// agentd was killed (review 241 B5), and the fixed-name ones of the version
// before; a check runs once per attempt at a time, so one older than ten
// minutes is no running check's.
func removeStaleIndexes(g *os.Root) {
	d, err := g.Open(".")
	if err != nil {
		return
	}
	names, _ := d.Readdirnames(-1)
	_ = d.Close()
	for _, n := range names {
		if !strings.HasPrefix(n, "agentd-changed-index") {
			continue
		}
		if info, err := g.Lstat(n); err == nil && time.Since(info.ModTime()) > 10*time.Minute {
			_ = g.Remove(n)
		}
	}
}

// gitDir is the checkout's .git, opened through the checkout: agentd makes
// and removes its own index files there outside the wall, and a .git the
// executor turned into a link out of the checkout is not followed (review
// 301).
func (w *Workspace) gitDir() (*os.Root, error) {
	r, err := ownDirAt(w.root, w.Dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return openStep(r, ".git")
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

// configDir reports whether a directory of this name holds an executor's
// configuration.
func configDir(name string) bool {
	switch name {
	case ".cursor", ".codex", ".claude", ".gemini":
		return true
	}
	return false
}

// ExecutorConfig reports whether path is where an executor reads its own
// settings or standing instructions: a .cursor, .codex, .claude or .gemini
// directory at any depth, or cursor's and MCP's files at the top. An attempt
// that writes one is changing how agents behave, which no task asks for
// (2026-10-17, PROD M-3: a reviewer wrote .cursor/rules/allow-shell.mdc and
// cli.json to allow itself the shell).
func ExecutorConfig(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if configDir(part) {
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
	why, _, err := checkedChanges(ctx, ws, readOnly)
	return why, err
}

// checkedChanges is unwantedChanges from the same reading of the checkout,
// and says as well whether the checkout was read and has no change git would
// commit against its base (TD-360): the tree is compared, not the head, so
// an executor's commit it reverted itself is no change.
func checkedChanges(ctx context.Context, ws *Workspace, readOnly bool) (why string, unchanged bool, err error) {
	changed, ignoredConfig, err := ws.Changes(ctx)
	if err != nil {
		if transient(err) {
			return "", false, err
		}
		return "the checkout could not be checked for what the executor changed: " + err.Error(), false, nil
	}
	unchanged = len(changed) == 0
	if readOnly && len(changed) > 0 {
		return "an attempt that pushes no branch changed the checkout: " + listed(changed), unchanged, nil
	}
	var bad []string
	for _, p := range append(changed, ignoredConfig...) {
		if ExecutorConfig(p) {
			bad = append(bad, p)
		}
	}
	if len(bad) == 0 {
		return "", unchanged, nil
	}
	return "the executor changed an executor's own configuration: " + listed(bad), unchanged, nil
}

// listed names up to ten paths and counts the rest.
func listed(paths []string) string {
	if len(paths) <= 10 {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(paths[:10], ", "), len(paths)-10)
}
