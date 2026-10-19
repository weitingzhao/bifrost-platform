package agentd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
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
		if m.Model == "" {
			// The executor's default model may have changed since: a run
			// with no model pinned is never resumed (review 288 B9).
			return m, fmt.Errorf("agentd: run %s has no model pinned and is not resumed: start a new run", m.RunID)
		}
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
