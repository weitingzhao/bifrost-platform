package agentd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The running and the judging of agentd bench (bench.go has the question
// set, the run's record and how it is handed in).

// Bench runs a question set with one executor on this host.
type Bench struct {
	Root     string // the run's directory: <AGENTD_ROOT>/bench/<run id>
	Self     string // agentd's executable: the launcher
	Remote   func(repo string) string
	GitEnv   []string
	CheckEnv []string // the checks' and the setup's environment
	Executor Executor
	Tool     Tool
	// Sandbox is the command that runs argv confined to tree, writable, and
	// the readOnly paths (BwrapSandbox on the agent host); nil runs it as it
	// is, which only tests do.
	Sandbox func(tree string, readOnly, argv []string) []string
	// GoModCache is where Go modules are kept: filled by the setup, read
	// only by the checks.
	GoModCache string
	// Secrets are what a check must not see; Preflight proves it cannot.
	Secrets []string
	// CodexHome is the host user's codex home: codex's login comes from it
	// and goes back to it refreshed (codexlogin.go).
	CodexHome  string
	Limit      time.Duration // the executor's, a question; 30 min
	CheckLimit time.Duration // each setup or check command's; 15 min
	KillGrace  time.Duration // 10 s
	Logf       func(format string, args ...any)

	locks map[string][]byte // the question's npm resolutions by manifest (npmInstall)
	// left is a command's group that was not gone after it ended: its record
	// stays, and the run launches nothing more (launch, Run; review 487 A2).
	left       error
	groupAlive func(pgid int) bool // GroupAlive; tests replace it
}

// BwrapSandbox runs argv in bubblewrap (the one codex ships, which the agent
// host's AppArmor rule bifrost-codex lets create namespaces) with a view of
// its own: the file system read only, /home empty but for tree (writable)
// and the readOnly paths, a fresh /tmp, no network. So a check, whose code
// is the executor's, sees no credential, no configuration in a home, no
// other question: there is nothing it could carry out, by its output, its
// exit or its outcome (review 290 A1). It keeps the host's process ids and
// /proc: the agent units' hardening (ProtectKernelTunables and the like)
// covers parts of /proc, and the kernel then refuses a fresh /proc in a new
// user namespace (measured on 06, 2026-10-18: "Can't mount proc on
// /newroot/proc"). What another process holds stays out of reach as long as
// ptrace is restricted (Preflight requires it): a check may not read the
// memory of the bench above it, and the bench's environment holds no secret.
func BwrapSandbox(bwrap string) func(tree string, readOnly, argv []string) []string {
	return func(tree string, readOnly, argv []string) []string {
		a := []string{bwrap, "--unshare-user", "--unshare-ipc", "--unshare-net", "--unshare-uts", "--unshare-cgroup-try",
			"--die-with-parent", "--new-session", "--ro-bind", "/", "/", "--tmpfs", "/home", "--tmpfs", "/tmp", "--dev", "/dev"}
		for _, p := range readOnly {
			a = append(a, "--ro-bind", p, p)
		}
		a = append(a, "--bind", tree, tree, "--setenv", "HOME", filepath.Join(tree, ".bench", "home"), "--")
		return append(a, argv...)
	}
}

// Preflight proves, before any question, that a check sees none of the
// Secrets: run in the sandbox as a check is, a probe looks for each. Without
// a sandbox nothing is proved, and nothing runs (review 290 A1).
func (b *Bench) Preflight(ctx context.Context) error {
	if b.Sandbox == nil {
		return errors.New("agentd: the benchmark's checks have no sandbox")
	}
	tree := filepath.Join(b.Root, ".preflight")
	if err := os.MkdirAll(filepath.Join(tree, ".bench", "home"), 0o700); err != nil {
		return err
	}
	defer func() { _ = forceRemove(tree) }()
	probe := append([]string{"/bin/sh", "-c", `for f in "$@"; do if [ -e "$f" ]; then echo "visible: $f"; exit 3; fi; done
if [ -r /proc/sys/kernel/yama/ptrace_scope ] && [ "$(cat /proc/sys/kernel/yama/ptrace_scope)" -lt 1 ]; then echo "ptrace is not restricted"; exit 4; fi`, "sh"}, b.Secrets...)
	if why := b.run(ctx, tree, b.checkEnv(tree), b.Sandbox(tree, b.readOnly("", nil), probe)...); why != "" {
		return fmt.Errorf("agentd: a check in the sandbox is not kept from the host's secrets: %s", why)
	}
	return nil
}

// readOnly is what a check of question directory qdir may read beside its
// tree: the Go module cache, the question's virtual environment and its
// siblings.
func (b *Bench) readOnly(qdir string, sibs []BenchSibling) []string {
	var out []string
	paths := []string{b.GoModCache}
	if qdir != "" {
		paths = append(paths, filepath.Join(qdir, "out", "venv"))
		for _, s := range sibs {
			paths = append(paths, filepath.Join(qdir, s.Repo))
		}
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// forceRemove removes path whatever the modes inside it say: a directory an
// executor or a check made unreadable is opened again first (review 290 B3).
func forceRemove(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err = os.Chmod(path, 0o700); err != nil {
			return err
		}
		entries, rerr := os.ReadDir(path)
		if rerr != nil {
			return rerr
		}
		for _, e := range entries {
			if err = forceRemove(filepath.Join(path, e.Name())); err != nil {
				return err
			}
		}
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Run runs the questions not yet in the run's results and returns all of
// its results, in the questions' order.
func (b *Bench) Run(ctx context.Context, qs []BenchQuestion) ([]BenchResult, error) {
	done, err := ReadBenchResults(b.Root)
	if err != nil {
		return nil, err
	}
	have := map[string]BenchResult{}
	for _, r := range done {
		have[r.Question] = r
	}
	var out []BenchResult
	for _, q := range qs {
		if r, ok := have[q.ID]; ok {
			out = append(out, r)
			continue
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		r, cerr := b.one(ctx, q)
		if cerr == nil {
			cerr = b.left
		}
		if cerr != nil {
			return out, fmt.Errorf("agentd: %s: what it left cannot be removed, so no further question runs: %w", q.ID, cerr)
		}
		if ctx.Err() != nil {
			return out, ctx.Err() // a question cut short is run again, not recorded
		}
		b.logf("agentd bench: %s %s %s (%d s) %s", q.ID, q.Language, r.Outcome, r.WallS, r.Reason)
		if err = appendBenchResult(b.Root, r); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (b *Bench) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

func (b *Bench) one(ctx context.Context, q BenchQuestion) (result BenchResult, cleanup error) {
	result, cleanup = b.question(ctx, q)
	return result, cleanup
}

func (b *Bench) question(ctx context.Context, q BenchQuestion) (_ BenchResult, cleanup error) {
	r := BenchResult{Question: q.ID, Class: q.Language}
	b.locks = nil // a question resolves its own
	unrunnable := func(why string) BenchResult {
		r.Outcome, r.Reason = "unrunnable", oneLine(why, 280)
		return r
	}
	fail := func(why string) BenchResult {
		r.Outcome, r.Reason = "fail", oneLine(why, 280)
		return r
	}
	qdir := filepath.Join(b.Root, q.ID)
	if err := forceRemove(qdir); err != nil {
		return unrunnable("clearing the question's directory: " + err.Error()), nil
	}
	out := filepath.Join(qdir, "out")
	if err := os.MkdirAll(filepath.Join(out, "tmp"), 0o700); err != nil {
		return unrunnable(err.Error()), nil
	}
	fixTree, baseTree, judge, work := filepath.Join(qdir, "fix"), filepath.Join(qdir, "base"), filepath.Join(qdir, "judge"), filepath.Join(qdir, "work")
	// However the question ends, only out/ stays: no clone, no tree of the
	// fix, no change of an executor for a later one to read (review 288 B3).
	defer func() {
		gone := []string{filepath.Join(qdir, "src"), fixTree, baseTree, judge, work}
		for _, s := range q.Siblings {
			gone = append(gone, filepath.Join(qdir, s.Repo), filepath.Join(qdir, ".clone-"+s.Repo))
		}
		for _, d := range gone {
			if err := forceRemove(d); err != nil && cleanup == nil {
				cleanup = err
			}
		}
	}()
	for _, s := range q.Siblings {
		if err := b.sibling(ctx, qdir, s); err != nil {
			return unrunnable("the sibling " + s.Repo + ": " + err.Error()), nil
		}
	}
	src, err := Prepare(ctx, filepath.Join(qdir, "src"), b.Remote(q.RepoName()), q.FixCommit, b.GitEnv)
	if err != nil {
		return unrunnable("cloning the repository: " + err.Error()), nil
	}
	tests := map[string][]byte{}
	for _, f := range q.TestFiles {
		body, gerr := gitRaw(ctx, src.Dir, src.env, "show", q.FixCommit+":"+filepath.ToSlash(f))
		if gerr != nil {
			return unrunnable("reading the fix's test " + f + ": " + gerr.Error()), nil
		}
		tests[f] = []byte(body)
	}
	venv := filepath.Join(out, "venv")
	if err = b.snapshot(ctx, src, q.FixCommit, fixTree); err != nil {
		return unrunnable("the fix's tree: " + err.Error()), nil
	}
	checks, err := b.setup(ctx, fixTree, venv, q)
	if err != nil {
		return unrunnable("setting up the checks: " + err.Error()), nil
	}
	if why := b.verify(ctx, fixTree, checks, q.Siblings, true); why != "" {
		return unrunnable("the fix does not pass its checks here: " + why), nil
	}
	if err = b.snapshot(ctx, src, q.BaseCommit, baseTree); err == nil {
		err = overlay(baseTree, tests)
	}
	if err != nil {
		return unrunnable("the tree before the fix: " + err.Error()), nil
	}
	if _, err = b.setup(ctx, baseTree, venv, q); err != nil {
		return unrunnable("setting up the checks before the fix: " + err.Error()), nil
	}
	if why := b.verify(ctx, baseTree, checks, q.Siblings, true); why == "" {
		return unrunnable("the checks pass on the code before the fix"), nil
	}
	if err = b.snapshot(ctx, src, q.BaseCommit, judge); err == nil {
		err = b.snapshot(ctx, src, q.BaseCommit, work)
	}
	if err == nil {
		err = b.initRepo(ctx, work)
	}
	if err != nil {
		return unrunnable("the executor's checkout: " + err.Error()), nil
	}
	// Nothing of the fix stays on disk for the executor to read.
	for _, d := range []string{src.Dir, fixTree, baseTree} {
		if err = os.RemoveAll(d); err != nil {
			return unrunnable("clearing " + filepath.Base(d) + ": " + err.Error()), nil
		}
	}
	start := time.Now()
	note, err := b.execute(ctx, q, work, out)
	r.WallS = int(time.Since(start).Seconds())
	if err != nil {
		return fail("the executor: " + err.Error()), nil
	}
	dropped, err := takeChanges(work, judge)
	if err == nil {
		err = overlay(judge, tests)
	}
	if err != nil {
		return fail(note + "taking its change: " + err.Error()), nil
	}
	if _, err = b.setup(ctx, judge, venv, q); err != nil {
		return fail(note + "setting up the checks over its change failed"), nil
	}
	if dropped > 0 {
		note += fmt.Sprintf("%d protected or linked files of its change left out; ", dropped)
	}
	if why := b.verify(ctx, judge, checks, q.Siblings, false); why != "" {
		return fail(note + why), nil
	}
	r.Outcome = "pass"
	if note != "" {
		r.Reason = oneLine(strings.TrimSuffix(note, "; "), 280)
	}
	return r, nil
}

// snapshot writes the tree of commit, without a repository, at dir.
func (b *Bench) snapshot(ctx context.Context, src *Workspace, commit, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", commit)
	archive.Dir, archive.Env, archive.Stdout = src.Dir, src.env, w
	unpack := exec.CommandContext(ctx, "tar", "-x", "-f", "-", "-C", dir)
	unpack.Env, unpack.Stdin = b.CheckEnv, r
	var aerrb, uerrb bytes.Buffer
	archive.Stderr, unpack.Stderr = &aerrb, &uerrb
	aerr := archive.Start()
	uerr := unpack.Start()
	_ = w.Close()
	_ = r.Close()
	if aerr == nil {
		aerr = archive.Wait()
	}
	if uerr == nil {
		uerr = unpack.Wait()
	}
	if aerr != nil || uerr != nil {
		return fmt.Errorf("git archive %s: %v %v: %s %s", commit, aerr, uerr, strings.TrimSpace(tail(aerrb.Bytes())), strings.TrimSpace(tail(uerrb.Bytes())))
	}
	return nil
}

// initRepo makes the executor's checkout a repository of one commit, the code
// before the fix, so the change it makes is a diff it can read; git reads
// neither the user's nor the system configuration.
func (b *Bench) initRepo(ctx context.Context, dir string) error {
	env := append(append([]string{}, b.GitEnv...), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	for _, args := range [][]string{{"init", "--quiet", "--template=", "--initial-branch=main"}, {"add", "--all"},
		{"commit", "--quiet", "--no-verify", "-m", "the code before the change"}} {
		if _, err := gitIn(ctx, dir, env, args...); err != nil {
			return err
		}
	}
	return nil
}

// overlay writes the fix's tests into tree.
func overlay(tree string, tests map[string][]byte) error {
	root, err := os.OpenRoot(tree)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for f, body := range tests {
		if err = root.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return err
		}
		_ = root.Remove(f)
		if err = root.WriteFile(f, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// protectedDirs and protectedFiles are what the judgement never takes from
// the executor's change: what the checks run as, not what they check.
var protectedDirs = map[string]bool{".git": true, "node_modules": true, "__tests__": true, "__fixtures__": true, "__mocks__": true,
	"testdata": true, "test": true, "tests": true, "_pytest": true, ".pytest_cache": true, ".bench": true}

var protectedFiles = []string{"*_test.go", "test_*.py", "*_test.py", "conftest.py", "*.test.ts", "*.test.tsx", "*.spec.ts",
	"*.spec.tsx", "*.test.js", "*.test.mjs", "*.test.cjs", "test_*.sh", "*_test.sh", "vitest.config.*", "vitest.workspace.*",
	"vite.config.*", "jest.config.*", "tsconfig*.json", "pytest.ini", ".pytest.ini", "pytest.toml", ".pytest.toml", "tox.ini", "setup.cfg", "pyproject.toml", "package.json",
	"package-lock.json", "npm-shrinkwrap.json", ".npmrc", "go.mod", "go.sum", "go.work", "go.work.sum", "pytest.py", "sitecustomize.py", "usercustomize.py", "*.pth"}

func protected(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts[:len(parts)-1] {
		if protectedDirs[p] {
			return true
		}
	}
	name := parts[len(parts)-1]
	for _, g := range protectedFiles {
		if ok, _ := path.Match(g, name); ok {
			return true
		}
	}
	return false
}

const (
	maxTakenFile  = 32 << 20
	maxTakenTotal = 512 << 20
)

// takeChanges brings what the executor changed in work into judge, the code
// before the fix: each regular file that differs or is new, each file it
// removed; never a protected one, a link or anything but a regular file,
// which it counts. Nothing of work is followed or run.
func takeChanges(work, judge string) (dropped int, err error) {
	jr, err := os.OpenRoot(judge)
	if err != nil {
		return 0, err
	}
	defer func() { _ = jr.Close() }()
	total := 0
	err = filepath.WalkDir(work, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(work, p)
		if rerr != nil || rel == "." {
			return rerr
		}
		if d.IsDir() {
			if protectedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		old, oerr := jr.ReadFile(rel)
		if !info.Mode().IsRegular() || protected(rel) {
			if !info.Mode().IsRegular() || oerr != nil || !sameFile(p, old) {
				dropped++
			}
			return nil
		}
		if info.Size() > maxTakenFile {
			return fmt.Errorf("%s is over %d MiB", rel, maxTakenFile>>20)
		}
		body, berr := os.ReadFile(p)
		if berr != nil {
			return berr
		}
		if oerr == nil && bytes.Equal(old, body) {
			return nil
		}
		if total += len(body); total > maxTakenTotal {
			return fmt.Errorf("the change is over %d MiB", maxTakenTotal>>20)
		}
		if merr := jr.MkdirAll(filepath.Dir(rel), 0o700); merr != nil {
			return merr
		}
		_ = jr.Remove(rel)
		mode := os.FileMode(0o600)
		if info.Mode()&0o100 != 0 {
			mode = 0o700
		}
		return jr.WriteFile(rel, body, mode)
	})
	if err != nil {
		return dropped, err
	}
	// what the executor removed (not what it could not have changed)
	var gone []string
	err = filepath.WalkDir(judge, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(judge, p)
		if rerr != nil || rel == "." {
			return rerr
		}
		if d.IsDir() {
			if protectedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if protected(rel) {
			return nil
		}
		if info, lerr := os.Lstat(filepath.Join(work, rel)); lerr != nil || !info.Mode().IsRegular() {
			gone = append(gone, rel)
		}
		return nil
	})
	for _, rel := range gone {
		if rerr := jr.Remove(rel); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return dropped, rerr
		}
	}
	return dropped, err
}

func sameFile(p string, old []byte) bool {
	body, err := os.ReadFile(p)
	return err == nil && bytes.Equal(body, old)
}

func (b *Bench) limit() time.Duration {
	if b.CheckLimit > 0 {
		return b.CheckLimit
	}
	return 15 * time.Minute
}

// verify runs the checks in tree, confined, each in its directory, and
// returns why they did not pass ("" when they did). What a check printed is
// part of why only when withOutput: the code the judgement runs is the
// executor's, and a reason is handed to the platform (review 288 A1).
func (b *Bench) verify(ctx context.Context, tree string, checks []BenchCheck, sibs []BenchSibling, withOutput bool) string {
	env := b.checkEnv(tree)
	for _, c := range checks {
		// The command's own directory, entered inside the sandbox.
		argv := append([]string{"/bin/sh", "-c", `cd "$1" && shift && exec "$@"`, "sh", filepath.Join(tree, c.Cwd)}, c.Argv...)
		if b.Sandbox != nil {
			argv = b.Sandbox(tree, b.readOnly(filepath.Dir(tree), sibs), argv)
		}
		if why := b.run(ctx, tree, env, argv...); why != "" {
			if !withOutput {
				why, _, _ = strings.Cut(why, ":")
			}
			return strings.Join(c.Argv, " ") + " " + why
		}
	}
	return ""
}

// run runs argv in dir in a process group of its own within the limit, and
// clears the group when it ends however it ends; it returns why it failed
// ("" when it exited 0).
func (b *Bench) run(ctx context.Context, dir string, env []string, argv ...string) string {
	why, _ := b.runTail(ctx, dir, env, argv...)
	return why
}

// runTail is run that also returns the end of what the command printed.
func (b *Bench) runTail(ctx context.Context, dir string, env []string, argv ...string) (string, []byte) {
	out := &ring{max: tailBytes}
	sp := Spawn{Argv: argv, Env: env, Dir: dir, Stdout: out, Stderr: out}
	code, timedOut, err := b.launch(ctx, sp, b.limit())
	switch {
	case err != nil:
		return err.Error(), out.bytes()
	case timedOut:
		return "timed out", out.bytes()
	case code != 0:
		if t := oneLine(string(tail(out.bytes())), 160); t != "" {
			return fmt.Sprintf("exited %d: %s", code, t), out.bytes()
		}
		return fmt.Sprintf("exited %d", code), out.bytes()
	}
	return "", out.bytes()
}

// launch starts sp through the launcher, waits for it within limit, and
// clears its group afterwards. While it runs, its group is recorded in the
// run's directory (benchRunning), so that a bench killed meanwhile leaves
// what the next agentd, bench or exam ends first (LeftOnHost, Recover;
// review 487 A2); the record goes once the group is gone. A group still
// there after it ended keeps the record, and the run launches nothing more.
func (b *Bench) launch(ctx context.Context, sp Spawn, limit time.Duration) (code int, timedOut bool, err error) {
	if b.left != nil {
		return 0, false, b.left
	}
	pgid := 0
	code, timedOut, err = launchRecorded(ctx, b.Self, sp, limit, b.KillGrace, func(g int, at string) error {
		pgid = g
		id, _ := procIdentity(g) // none: the record names the group, but never ends it alone
		return keepBenchRunning(b.Root, benchRunning{PGID: g, Start: at, ID: id})
	})
	if pgid == 0 {
		return code, timedOut, err
	}
	alive := GroupAlive
	if b.groupAlive != nil {
		alive = b.groupAlive
	}
	if alive(pgid) {
		b.left = fmt.Errorf("agentd: process group %d of a bench command is not gone after it ended: its record %s stays for the "+
			"next agentd, bench or exam to end it", pgid, filepath.Join(b.Root, benchRunningName))
		return code, timedOut, errors.Join(err, b.left)
	}
	if rerr := os.Remove(filepath.Join(b.Root, benchRunningName)); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		return code, timedOut, errors.Join(err, rerr)
	}
	return code, timedOut, err
}

// launchRecorded starts sp through the launcher at self, waits for it within
// limit (TERM, then KILL after grace, 10 s by default), and clears its group
// afterwards; ctx ending ends it the same way. record is called with the
// group before the group is released (Launch): agentd exam and agentd bench
// keep it, so that a run cut short can be ended (review 415 A2, 487 A2).
func launchRecorded(ctx context.Context, self string, sp Spawn, limit, grace time.Duration,
	record func(pgid int, start string) error) (code int, timedOut bool, err error) {
	proc, err := Launch(self, sp, record)
	if err != nil {
		return 0, false, err
	}
	exited := make(chan int, 1)
	safego.Go("agentd-bench-wait", func() { exited <- proc.Wait() })
	if grace <= 0 {
		grace = 10 * time.Second
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case code = <-exited:
	case <-timer.C:
		Terminate(proc.PGID, grace)
		code, timedOut = <-exited, true
	case <-ctx.Done():
		Terminate(proc.PGID, grace)
		<-exited
		return 0, false, ctx.Err()
	}
	Terminate(proc.PGID, grace) // what it left running in its group (review 287 B6)
	return code, timedOut, nil
}

// benchPrompt is what the executor is asked: the change's commit message,
// the declarations its tests call that are not here yet (TD-373), and the
// siblings beside the checkout.
func benchPrompt(q BenchQuestion) string {
	var p strings.Builder
	fmt.Fprintf(&p, "This checkout of %s is at the commit before a change was made. Make that change: its commit message follows. "+
		"Tests for it exist upstream; once you have finished they are put over this checkout and run. Do not commit.\n\n%s",
		q.RepoName(), strings.TrimSpace(q.TaskText))
	if len(q.Interface) > 0 {
		p.WriteString("\n\n---\nThe tests use these declarations, which this checkout does not have yet or has with another " +
			"signature. Declare them as given; the bodies are yours.\n")
		for _, d := range q.Interface {
			p.WriteString("\n" + strings.TrimSpace(d) + "\n")
		}
	}
	for _, s := range q.Siblings {
		fmt.Fprintf(&p, "\n\n../%s beside this checkout is %s as the change found it, built; the tests read it there.", s.Repo, s.Repo)
	}
	return p.String()
}

// execute runs the executor on the checkout within the limit and returns a
// note on how it ended when that was not a clean exit.
func (b *Bench) execute(ctx context.Context, q BenchQuestion, dir, out string) (string, error) {
	a := &Attempt{Claim: &Claimed{TaskID: q.ID, AttemptNo: 1, Role: "implement", Repo: q.RepoName(), BaseCommit: q.BaseCommit,
		EffectClass: "branch", Tool: b.Tool}, Spec: TaskSpec{Text: benchPrompt(q)}, Dir: dir, Out: out, root: b.Root}
	sp, err := b.Executor(a)
	if err != nil {
		return "", err
	}
	// The question's modules are in the cache the setup filled, which codex's
	// commands (no network) read; its own Go cache stays the attempt's
	// (review 301 B).
	if b.GoModCache != "" {
		sp.Env = withEnv(sp.Env, "GOMODCACHE", b.GoModCache)
	}
	log, err := os.OpenFile(filepath.Join(out, "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = log.Close() }()
	sp.Stdout, sp.Stderr = log, log
	limit := b.Limit
	if limit <= 0 {
		limit = 30 * time.Minute
	}
	code, timedOut, err := b.launch(ctx, sp, limit)
	if err != nil {
		return "", err
	}
	logf := b.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	afterExecutor(b.Tool.Executor, b.CodexHome, b.Root, out, logf)
	note := ""
	switch {
	case timedOut:
		note = "the executor ran out of time; "
	case code != 0:
		note = fmt.Sprintf("the executor exited %d; ", code)
	}
	if sp.After != nil {
		_ = sp.After() // the report is the executor's, not the outcome
	}
	return note, nil
}
