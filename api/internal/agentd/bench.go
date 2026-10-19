package agentd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// agentd bench (W-31 2.4 slice ⑤; the Owner 2026-10-18): the question set,
// run on this host with one executor, judged by each fix's own tests, the
// outcome handed to the platform as this host (store/benchmark.go). For each
// question:
//
//  1. Whether it stands here, at no vendor cost: in a tree of the fix and in
//     a tree of the code before it with the fix's tests over it (each a fresh
//     snapshot, no repository, its own dependencies: TypeScript npm ci
//     without install scripts, Python and shell a virtual environment with
//     pytest, Go its modules), the checks must pass on the first and fail on
//     the second. Otherwise the question is unrunnable and costs nothing.
//  2. The executor works in a repository holding only the code before the
//     fix (one commit, no history: the fix is not there to read), asked to
//     make the change the fix's commit message describes, within the limit.
//     Before it starts, the clone and the two trees are gone from disk; the
//     unit hides the daemon's own task checkouts (review 287 B3).
//  3. The judgement, in a fresh snapshot of the code before the fix: the
//     executor's changes are taken file by file, except tests, test and
//     build configuration, dependency manifests and links (protected, review
//     287 B2); the fix's tests go over them; the checks run. From the
//     executor's start on, whatever fails is the executor's: fail (B5).
//
// Every check runs in a process group of its own, cleared when it ends, and,
// on the agent host, inside codex's sandbox: no network, writes only to its
// tree (review 287 A1, B6). Each outcome is appended to the run's
// results.jsonl as it is known, so a run cut short goes on from where it was
// (the same run id); a run is handed in only once every one of its questions
// has an outcome.

// BenchQuestion is one question of the set (agent-config/work/multi-agent/
// benchmark/questions.json in bifrost-trade-infra).
type BenchQuestion struct {
	ID         string       `json:"id"`
	Repo       string       `json:"repo"` // the path it was picked from; its last element is the repository
	Language   string       `json:"language"`
	BaseCommit string       `json:"base_commit"`
	FixCommit  string       `json:"fix_commit"`
	TestFiles  []string     `json:"test_files"`
	Checks     []BenchCheck `json:"check_command"`
	FailsBy    string       `json:"fails_by"`
	TaskText   string       `json:"task_text"`
}

// BenchCheck is one check command, run in Cwd of the tree.
type BenchCheck struct {
	Cwd  string   `json:"cwd"`
	Argv []string `json:"argv"`
}

// BenchResult is one question's outcome as the platform takes it.
type BenchResult struct {
	Question string `json:"question"`
	Class    string `json:"class"`
	Outcome  string `json:"outcome"` // pass, fail, unrunnable
	WallS    int    `json:"wall_s"`
	Reason   string `json:"reason,omitempty"`
}

// BenchMeta is what a run is: its id, the executor as configured, the
// question set and the questions it answers. A trial (--only) is never
// handed in.
type BenchMeta struct {
	RunID       string    `json:"run_id"`
	Executor    string    `json:"executor"`
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Questions   string    `json:"questions_sha256"`
	QuestionIDs []string  `json:"question_ids"`
	Trial       bool      `json:"trial,omitempty"`
	StartedAt   time.Time `json:"started_at"`
}

var (
	benchIDRE   = regexp.MustCompile(`^[0-9A-Za-z_-]{1,32}$`)
	benchRepoRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	benchSHARE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	benchRunRE  = regexp.MustCompile(`^[0-9A-Za-z_-]{8,64}$`)
)

var benchClasses = map[string]bool{"go": true, "python": true, "ts": true, "shell": true}

// ReadBenchQuestions reads and checks a question set, and returns it with
// the sha256 of the file.
func ReadBenchQuestions(path string) ([]BenchQuestion, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var qs []BenchQuestion
	if err = json.Unmarshal(b, &qs); err != nil {
		return nil, "", fmt.Errorf("agentd: questions: %w", err)
	}
	seen := map[string]bool{}
	for _, q := range qs {
		if err = q.check(); err != nil {
			return nil, "", err
		}
		if seen[q.ID] {
			return nil, "", fmt.Errorf("agentd: question %s comes twice", q.ID)
		}
		seen[q.ID] = true
	}
	if len(qs) == 0 || len(qs) > 100 {
		return nil, "", errors.New("agentd: a question set has 1 to 100 questions")
	}
	sum := sha256.Sum256(b)
	return qs, hex.EncodeToString(sum[:]), nil
}

func (q BenchQuestion) check() error {
	bad := func(what string) error { return fmt.Errorf("agentd: question %q: %s", q.ID, what) }
	switch {
	case !benchIDRE.MatchString(q.ID):
		return bad("the id is letters, digits, _ or -")
	case !benchRepoRE.MatchString(q.RepoName()):
		return bad("the repository is not a repository name")
	case !benchClasses[q.Language]:
		return bad("the language is not go, python, ts or shell")
	case !benchSHARE.MatchString(q.BaseCommit) || !benchSHARE.MatchString(q.FixCommit):
		return bad("the commits are full hex ids")
	case len(q.TestFiles) == 0 || len(q.Checks) == 0 || strings.TrimSpace(q.TaskText) == "":
		return bad("it has test files, checks and a task text")
	}
	for _, f := range q.TestFiles {
		if !filepath.IsLocal(f) {
			return bad("test file " + f + " is not a path inside the tree")
		}
	}
	for _, c := range q.Checks {
		if !filepath.IsLocal(c.Cwd) || len(c.Argv) == 0 || c.Argv[0] == "" {
			return bad("a check runs a command in a directory inside the tree")
		}
	}
	return nil
}

// RepoName is the repository the question is of.
func (q BenchQuestion) RepoName() string { return filepath.Base(filepath.Clean(q.Repo)) }

// Bench runs a question set with one executor on this host.
type Bench struct {
	Root     string // the run's directory: <AGENTD_ROOT>/bench/<run id>
	Self     string // agentd's executable: the launcher
	Remote   func(repo string) string
	GitEnv   []string
	CheckEnv []string // the checks' and the setup's environment
	Executor Executor
	Tool     Tool
	// Sandbox is the command that runs argv confined to tree (CodexSandbox on
	// the agent host); nil runs it as it is, which only tests do.
	Sandbox    func(tree string, argv []string) []string
	Limit      time.Duration // the executor's, a question; 30 min
	CheckLimit time.Duration // each setup or check command's; 15 min
	KillGrace  time.Duration // 10 s
	Logf       func(format string, args ...any)
}

// CodexSandbox runs argv in codex's own sandbox with the workspace profile,
// tree its workspace: no network, writes only under tree (the step
// codex-sandbox-probe.sh proves on the agent host).
func CodexSandbox(codex string) func(tree string, argv []string) []string {
	return func(tree string, argv []string) []string {
		return append([]string{codex, "sandbox", "--permission-profile", ":workspace", "--cd", tree, "--"}, argv...)
	}
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
		r := b.one(ctx, q)
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

func (b *Bench) one(ctx context.Context, q BenchQuestion) BenchResult {
	r := BenchResult{Question: q.ID, Class: q.Language}
	unrunnable := func(why string) BenchResult {
		r.Outcome, r.Reason = "unrunnable", oneLine(why, 280)
		return r
	}
	fail := func(why string) BenchResult {
		r.Outcome, r.Reason = "fail", oneLine(why, 280)
		return r
	}
	qdir := filepath.Join(b.Root, q.ID)
	if err := os.RemoveAll(qdir); err != nil {
		return unrunnable("clearing the question's directory: " + err.Error())
	}
	out := filepath.Join(qdir, "out")
	if err := os.MkdirAll(filepath.Join(out, "tmp"), 0o700); err != nil {
		return unrunnable(err.Error())
	}
	src, err := Prepare(ctx, filepath.Join(qdir, "src"), b.Remote(q.RepoName()), q.FixCommit, b.GitEnv)
	if err != nil {
		return unrunnable("cloning the repository: " + err.Error())
	}
	tests := map[string][]byte{}
	for _, f := range q.TestFiles {
		body, gerr := gitRaw(ctx, src.Dir, src.env, "show", q.FixCommit+":"+filepath.ToSlash(f))
		if gerr != nil {
			return unrunnable("reading the fix's test " + f + ": " + gerr.Error())
		}
		tests[f] = []byte(body)
	}
	venv := filepath.Join(out, "venv")
	fixTree, baseTree, judge := filepath.Join(qdir, "fix"), filepath.Join(qdir, "base"), filepath.Join(qdir, "judge")
	if err = b.snapshot(ctx, src, q.FixCommit, fixTree); err != nil {
		return unrunnable("the fix's tree: " + err.Error())
	}
	checks, err := b.setup(ctx, fixTree, venv, q)
	if err != nil {
		return unrunnable("setting up the checks: " + err.Error())
	}
	if why := b.verify(ctx, fixTree, checks); why != "" {
		return unrunnable("the fix does not pass its checks here: " + why)
	}
	if err = b.snapshot(ctx, src, q.BaseCommit, baseTree); err == nil {
		err = overlay(baseTree, tests)
	}
	if err != nil {
		return unrunnable("the tree before the fix: " + err.Error())
	}
	if _, err = b.setup(ctx, baseTree, venv, q); err != nil {
		return unrunnable("setting up the checks before the fix: " + err.Error())
	}
	if why := b.verify(ctx, baseTree, checks); why == "" {
		return unrunnable("the checks pass on the code before the fix")
	}
	work := filepath.Join(qdir, "work")
	if err = b.snapshot(ctx, src, q.BaseCommit, judge); err == nil {
		err = b.snapshot(ctx, src, q.BaseCommit, work)
	}
	if err == nil {
		err = b.initRepo(ctx, work)
	}
	if err != nil {
		return unrunnable("the executor's checkout: " + err.Error())
	}
	// Nothing of the fix stays on disk for the executor to read.
	for _, d := range []string{src.Dir, fixTree, baseTree} {
		if err = os.RemoveAll(d); err != nil {
			return unrunnable("clearing " + filepath.Base(d) + ": " + err.Error())
		}
	}
	start := time.Now()
	note, err := b.execute(ctx, q, work, out)
	r.WallS = int(time.Since(start).Seconds())
	if err != nil {
		return fail("the executor: " + err.Error())
	}
	dropped, err := takeChanges(work, judge)
	if err == nil {
		err = overlay(judge, tests)
	}
	if err != nil {
		return fail(note + "taking its change: " + err.Error())
	}
	if _, err = b.setup(ctx, judge, venv, q); err != nil {
		return fail(note + "setting up the checks over its change: " + err.Error())
	}
	if dropped > 0 {
		note += fmt.Sprintf("%d protected or linked files of its change left out; ", dropped)
	}
	if why := b.verify(ctx, judge, checks); why != "" {
		return fail(note + why)
	}
	r.Outcome = "pass"
	if note != "" {
		r.Reason = oneLine(strings.TrimSuffix(note, "; "), 280)
	}
	return r
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
var protectedDirs = map[string]bool{".git": true, "node_modules": true, "__tests__": true, "__fixtures__": true, "testdata": true,
	".pytest_cache": true, ".bench": true}

var protectedFiles = []string{"*_test.go", "test_*.py", "*_test.py", "conftest.py", "*.test.ts", "*.test.tsx", "*.spec.ts",
	"*.spec.tsx", "*.test.js", "*.test.mjs", "*.test.cjs", "test_*.sh", "*_test.sh", "vitest.config.*", "vitest.workspace.*",
	"vite.config.*", "jest.config.*", "tsconfig*.json", "pytest.ini", "tox.ini", "setup.cfg", "pyproject.toml", "package.json",
	"package-lock.json", ".npmrc", "go.mod", "go.sum", "go.work", "go.work.sum"}

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

// setup installs what the checks need in tree (dependencies from the
// tree's own manifests, which the judgement never takes from the executor)
// and returns the checks as they are run.
func (b *Bench) setup(ctx context.Context, tree, venv string, q BenchQuestion) ([]BenchCheck, error) {
	checks := append([]BenchCheck(nil), q.Checks...)
	if err := os.MkdirAll(filepath.Join(tree, ".bench", "tmp"), 0o700); err != nil {
		return nil, err
	}
	env := b.setupEnv(tree)
	seen := map[string]bool{}
	switch q.Language {
	case "ts":
		for _, c := range checks {
			if seen[c.Cwd] {
				continue
			}
			seen[c.Cwd] = true
			if _, err := os.Stat(filepath.Join(tree, c.Cwd, "package-lock.json")); err != nil {
				return nil, fmt.Errorf("no package-lock.json in %s", c.Cwd)
			}
			if why := b.run(ctx, filepath.Join(tree, c.Cwd), env, "npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"); why != "" {
				return nil, errors.New("npm ci: " + why)
			}
		}
	case "go":
		for _, c := range checks {
			dir := moduleRoot(tree, c.Cwd)
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			if why := b.run(ctx, dir, env, "go", "mod", "download"); why != "" {
				return nil, errors.New("go mod download: " + why)
			}
		}
	case "python", "shell":
		if _, err := os.Stat(filepath.Join(venv, "bin", "python3")); err != nil {
			if why := b.run(ctx, tree, env, "python3", "-m", "venv", venv); why != "" {
				return nil, errors.New("python3 -m venv: " + why)
			}
			if why := b.run(ctx, tree, env, filepath.Join(venv, "bin", "pip"), "install", "--quiet", "--disable-pip-version-check", "pytest"); why != "" {
				return nil, errors.New("pip install pytest: " + why)
			}
		}
		for i, c := range checks {
			if c.Argv[0] == "python3" || c.Argv[0] == "python" {
				checks[i] = BenchCheck{Cwd: c.Cwd, Argv: append([]string{filepath.Join(venv, "bin", "python3")}, c.Argv[1:]...)}
			}
		}
	}
	return checks, nil
}

// moduleRoot is the directory of the go.mod nearest cwd inside tree.
func moduleRoot(tree, cwd string) string {
	for d := filepath.Join(tree, cwd); strings.HasPrefix(d, tree); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if d == tree {
			break
		}
	}
	return ""
}

func (b *Bench) setupEnv(tree string) []string {
	env := append([]string{}, b.CheckEnv...)
	env = withEnv(env, "GOCACHE", filepath.Join(tree, ".bench", "go-build"))
	env = withEnv(env, "GOTOOLCHAIN", "local")
	return withEnv(env, "TMPDIR", filepath.Join(tree, ".bench", "tmp"))
}

// checkEnv is the setup's, offline: everything the checks need is in place.
func (b *Bench) checkEnv(tree string) []string {
	env := b.setupEnv(tree)
	env = withEnv(env, "GOFLAGS", "-mod=mod")
	env = withEnv(env, "GOPROXY", "off")
	return withEnv(env, "GONOSUMDB", "*")
}

func (b *Bench) limit() time.Duration {
	if b.CheckLimit > 0 {
		return b.CheckLimit
	}
	return 15 * time.Minute
}

// verify runs the checks in tree, confined, each in its directory, and
// returns why they did not pass ("" when they did).
func (b *Bench) verify(ctx context.Context, tree string, checks []BenchCheck) string {
	env := b.checkEnv(tree)
	for _, c := range checks {
		// The command's own directory, entered inside the sandbox.
		argv := append([]string{"/bin/sh", "-c", `cd "$1" && shift && exec "$@"`, "sh", filepath.Join(tree, c.Cwd)}, c.Argv...)
		if b.Sandbox != nil {
			argv = b.Sandbox(tree, argv)
		}
		if why := b.run(ctx, tree, env, argv...); why != "" {
			return strings.Join(c.Argv, " ") + " " + why
		}
	}
	return ""
}

// run runs argv in dir in a process group of its own within the limit, and
// clears the group when it ends however it ends; it returns why it failed
// ("" when it exited 0).
func (b *Bench) run(ctx context.Context, dir string, env []string, argv ...string) string {
	out := &ring{max: tailBytes}
	sp := Spawn{Argv: argv, Env: env, Dir: dir, Stdout: out, Stderr: out}
	code, timedOut, err := b.launch(ctx, sp, b.limit())
	switch {
	case err != nil:
		return err.Error()
	case timedOut:
		return "timed out"
	case code != 0:
		if t := oneLine(string(tail(out.bytes())), 160); t != "" {
			return fmt.Sprintf("exited %d: %s", code, t)
		}
		return fmt.Sprintf("exited %d", code)
	}
	return ""
}

// launch starts sp through the launcher, waits for it within limit, and
// clears its group afterwards.
func (b *Bench) launch(ctx context.Context, sp Spawn, limit time.Duration) (code int, timedOut bool, err error) {
	proc, err := Launch(b.Self, sp, func(int, string) error { return nil })
	if err != nil {
		return 0, false, err
	}
	exited := make(chan int, 1)
	safego.Go("agentd-bench-wait", func() { exited <- proc.Wait() })
	grace := b.KillGrace
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

// benchPrompt is what the executor is asked.
func benchPrompt(q BenchQuestion) string {
	return fmt.Sprintf("This checkout of %s is at the commit before a change was made. Make that change: its commit message follows. "+
		"Tests for it exist upstream; once you have finished they are put over this checkout and run. Do not commit.\n\n%s",
		q.RepoName(), strings.TrimSpace(q.TaskText))
}

// execute runs the executor on the checkout within the limit and returns a
// note on how it ended when that was not a clean exit.
func (b *Bench) execute(ctx context.Context, q BenchQuestion, dir, out string) (string, error) {
	a := &Attempt{Claim: &Claimed{TaskID: q.ID, AttemptNo: 1, Role: "implement", Repo: q.RepoName(), BaseCommit: q.BaseCommit,
		EffectClass: "branch", Tool: b.Tool}, Spec: TaskSpec{Text: benchPrompt(q)}, Dir: dir, Out: out}
	sp, err := b.Executor(a)
	if err != nil {
		return "", err
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

// ReadBenchResults reads a run's results so far.
func ReadBenchResults(root string) ([]BenchResult, error) {
	f, err := os.Open(filepath.Join(root, "results.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []BenchResult
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r BenchResult
		if err = json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("agentd: %s: %w", f.Name(), err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

func appendBenchResult(root string, r BenchResult) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, "results.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// WriteBenchMeta writes what the run is, once; a run resumed keeps its own,
// and only with the same executor, provider, model, question set, questions
// and kind (review 287 B9).
func WriteBenchMeta(root string, m BenchMeta) (BenchMeta, error) {
	p := filepath.Join(root, "meta.json")
	if b, err := os.ReadFile(p); err == nil {
		var old BenchMeta
		if err = json.Unmarshal(b, &old); err != nil {
			return m, err
		}
		if old.Executor != m.Executor || old.Provider != m.Provider || old.Model != m.Model || old.Questions != m.Questions ||
			!slices.Equal(old.QuestionIDs, m.QuestionIDs) || old.Trial != m.Trial {
			return m, fmt.Errorf("agentd: run %s is of %s (%s %s) on other questions: start a new run", old.RunID, old.Executor, old.Provider, old.Model)
		}
		return old, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return m, err
	}
	if !benchRunRE.MatchString(m.RunID) {
		return m, fmt.Errorf("agentd: run id %q is 8 to 64 letters, digits, _ or -", m.RunID)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return m, err
	}
	return m, os.WriteFile(p, b, 0o600)
}

// ReadBenchMeta reads what a run is.
func ReadBenchMeta(root string) (BenchMeta, error) {
	var m BenchMeta
	b, err := os.ReadFile(filepath.Join(root, "meta.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// Complete is the run's results for its questions, in their order, when
// every one has an outcome and it is not a trial: only then is it handed
// in (review 287 B10).
func (m BenchMeta) Complete(results []BenchResult) ([]BenchResult, error) {
	if m.Trial {
		return nil, fmt.Errorf("run %s is a trial (--only): it is not handed in", m.RunID)
	}
	by := map[string]BenchResult{}
	for _, r := range results {
		by[r.Question] = r
	}
	out := make([]BenchResult, 0, len(m.QuestionIDs))
	for _, id := range m.QuestionIDs {
		r, ok := by[id]
		if !ok {
			return nil, fmt.Errorf("run %s has no outcome for %s yet: run it again with --run-id %s", m.RunID, id, m.RunID)
		}
		out = append(out, r)
	}
	return out, nil
}
