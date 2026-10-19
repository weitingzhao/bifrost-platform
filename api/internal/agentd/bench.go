package agentd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// agentd bench (W-31 2.4 slice ⑤; the Owner 2026-10-18): the question set,
// run on this host with one executor, judged by each fix's own tests, the
// outcome handed to the platform as this host (store/benchmark.go). For each
// question, in a checkout of its own:
//
//  1. Whether it stands here, at no vendor cost: the dependencies its checks
//     need are set up (TypeScript: npm ci without install scripts; Python
//     and shell: a virtual environment with pytest; Go: nothing), the checks
//     must pass on the fix and fail on the code before it with the fix's
//     tests put over it. Otherwise the question is unrunnable and the
//     executor is not called.
//  2. The executor, launched as agentd launches an attempt (its own process
//     group, the same argv and environment), is asked on the code before the
//     fix to make the change the fix's commit message describes, within the
//     limit; then the fix's tests are put over what it wrote and the checks
//     run: pass or fail.
//
// Each outcome is appended to the run's results.jsonl as it is known, so a
// run cut short goes on from where it was (the same run id) and a run whose
// hand-in failed is handed in again with --post.

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

// BenchCheck is one check command, run in Cwd of the checkout.
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

// BenchMeta is what a run is: its id, the executor, the question set.
type BenchMeta struct {
	RunID     string    `json:"run_id"`
	Executor  string    `json:"executor"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Questions string    `json:"questions_sha256"`
	StartedAt time.Time `json:"started_at"`
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
			return bad("test file " + f + " is not a path inside the checkout")
		}
	}
	for _, c := range q.Checks {
		if !filepath.IsLocal(c.Cwd) || len(c.Argv) == 0 || c.Argv[0] == "" {
			return bad("a check runs a command in a directory inside the checkout")
		}
	}
	return nil
}

// RepoName is the repository the question is of.
func (q BenchQuestion) RepoName() string { return filepath.Base(filepath.Clean(q.Repo)) }

// Bench runs a question set with one executor on this host.
type Bench struct {
	Root       string // the run's directory: <AGENTD_ROOT>/bench/<run id>
	Self       string // agentd's executable: the launcher
	Remote     func(repo string) string
	GitEnv     []string
	CheckEnv   []string // the checks' and the setup's environment
	Executor   Executor
	Tool       Tool
	Limit      time.Duration // the executor's, a question; 30 min
	CheckLimit time.Duration // the checks', each run of them; 15 min
	KillGrace  time.Duration // 10 s
	Logf       func(format string, args ...any)
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
	qdir := filepath.Join(b.Root, q.ID)
	if err := os.RemoveAll(qdir); err != nil {
		return unrunnable("clearing the question's directory: " + err.Error())
	}
	out := filepath.Join(qdir, "out")
	if err := os.MkdirAll(out, 0o700); err != nil {
		return unrunnable(err.Error())
	}
	ws, err := Prepare(ctx, filepath.Join(qdir, "ws"), b.Remote(q.RepoName()), q.FixCommit, b.GitEnv)
	if err != nil {
		return unrunnable("checking out the fix: " + err.Error())
	}
	checks, err := b.setup(ctx, ws.Dir, out, q)
	if err != nil {
		return unrunnable("setting up the checks: " + err.Error())
	}
	if v := b.verify(ctx, ws.Dir, checks); !v.Pass {
		return unrunnable("the fix does not pass its checks here: " + failure(v))
	}
	if err = b.before(ctx, ws, q); err != nil {
		return unrunnable(err.Error())
	}
	if v := b.verify(ctx, ws.Dir, checks); v.Pass {
		return unrunnable("the checks pass on the code before the fix")
	}
	if _, err = ws.git(ctx, ws.Dir, "reset", "--quiet", "--hard", q.BaseCommit); err != nil {
		return unrunnable(err.Error())
	}
	if _, err = ws.git(ctx, ws.Dir, "clean", "--quiet", "-fd"); err != nil {
		return unrunnable(err.Error())
	}
	start := time.Now()
	ran, err := b.execute(ctx, q, ws.Dir, out)
	r.WallS = int(time.Since(start).Seconds())
	if err != nil {
		r.Outcome, r.Reason = "fail", oneLine("the executor: "+err.Error(), 280)
		return r
	}
	if _, err = ws.git(ctx, ws.Dir, append([]string{"checkout", q.FixCommit, "--"}, q.TestFiles...)...); err != nil {
		return unrunnable("putting the fix's tests over the change: " + err.Error())
	}
	if v := b.verify(ctx, ws.Dir, checks); v.Pass {
		r.Outcome = "pass"
	} else {
		r.Outcome, r.Reason = "fail", oneLine(ran+failure(v), 280)
	}
	return r
}

// before puts the checkout at the code before the fix, the fix's tests over it.
func (b *Bench) before(ctx context.Context, ws *Workspace, q BenchQuestion) error {
	if _, err := ws.git(ctx, ws.Dir, "checkout", "--quiet", "--detach", q.BaseCommit); err != nil {
		return err
	}
	_, err := ws.git(ctx, ws.Dir, append([]string{"checkout", q.FixCommit, "--"}, q.TestFiles...)...)
	return err
}

// setup installs what the checks need and returns them as they are run.
func (b *Bench) setup(ctx context.Context, dir, out string, q BenchQuestion) ([]BenchCheck, error) {
	checks := append([]BenchCheck(nil), q.Checks...)
	switch q.Language {
	case "ts":
		seen := map[string]bool{}
		for _, c := range checks {
			if seen[c.Cwd] {
				continue
			}
			seen[c.Cwd] = true
			if _, err := os.Stat(filepath.Join(dir, c.Cwd, "package-lock.json")); err != nil {
				return nil, fmt.Errorf("no package-lock.json in %s", c.Cwd)
			}
			if v := Verify(ctx, filepath.Join(dir, c.Cwd), b.CheckEnv,
				[][]string{{"npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"}}, b.checkLimit()); !v.Pass {
				return nil, errors.New("npm ci: " + failure(v))
			}
		}
	case "python", "shell":
		venv := filepath.Join(out, "venv")
		if v := Verify(ctx, dir, b.CheckEnv, [][]string{{"python3", "-m", "venv", venv},
			{filepath.Join(venv, "bin", "pip"), "install", "--quiet", "--disable-pip-version-check", "pytest"}}, b.checkLimit()); !v.Pass {
			return nil, errors.New("a virtual environment with pytest: " + failure(v))
		}
		for i, c := range checks {
			if c.Argv[0] == "python3" || c.Argv[0] == "python" {
				argv := append([]string{filepath.Join(venv, "bin", "python3")}, c.Argv[1:]...)
				checks[i] = BenchCheck{Cwd: c.Cwd, Argv: argv}
			}
		}
	}
	return checks, nil
}

func (b *Bench) checkLimit() time.Duration {
	if b.CheckLimit > 0 {
		return b.CheckLimit
	}
	return 15 * time.Minute
}

// verify runs the checks, each in its directory, and passes when all do.
func (b *Bench) verify(ctx context.Context, dir string, checks []BenchCheck) Verification {
	all := Verification{Pass: true, Commands: []CommandTrace{}}
	for _, c := range checks {
		v := Verify(ctx, filepath.Join(dir, c.Cwd), b.CheckEnv, [][]string{c.Argv}, b.checkLimit())
		all.Commands = append(all.Commands, v.Commands...)
		if !v.Pass {
			all.Pass, all.Reason = false, v.Reason
			return all
		}
	}
	return all
}

// failure is one line on why a verification failed.
func failure(v Verification) string {
	for _, c := range v.Commands {
		if c.ExitCode != 0 || c.TimedOut {
			s := fmt.Sprintf("%s exited %d", strings.Join(c.Argv, " "), c.ExitCode)
			if c.TimedOut {
				s += " (timed out)"
			}
			return s
		}
	}
	return v.Reason
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
	proc, err := Launch(b.Self, sp, func(int, string) error { return nil })
	if err != nil {
		return "", err
	}
	exited := make(chan int, 1)
	safego.Go("agentd-bench-wait", func() { exited <- proc.Wait() })
	limit := b.Limit
	if limit <= 0 {
		limit = 30 * time.Minute
	}
	grace := b.KillGrace
	if grace <= 0 {
		grace = 10 * time.Second
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	note := ""
	select {
	case code := <-exited:
		if code != 0 {
			note = fmt.Sprintf("the executor exited %d; ", code)
		}
	case <-timer.C:
		Terminate(proc.PGID, grace)
		<-exited
		note = "the executor ran out of time; "
	case <-ctx.Done():
		Terminate(proc.PGID, grace)
		<-exited
		return "", ctx.Err()
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

// WriteBenchMeta writes what the run is, once; a run resumed keeps its own.
func WriteBenchMeta(root string, m BenchMeta) (BenchMeta, error) {
	path := filepath.Join(root, "meta.json")
	if b, err := os.ReadFile(path); err == nil {
		var old BenchMeta
		if err = json.Unmarshal(b, &old); err != nil {
			return m, err
		}
		if old.Executor != m.Executor || old.Questions != m.Questions {
			return m, fmt.Errorf("agentd: run %s is of %s on another question set", old.RunID, old.Executor)
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
	return m, os.WriteFile(path, b, 0o600)
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
