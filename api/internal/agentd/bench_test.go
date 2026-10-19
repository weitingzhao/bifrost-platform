package agentd_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

// benchRepo is a bare repository with the code before a fix and the fix:
// calc.txt goes from old to new; the fix adds check.sh (passes on new only),
// always.sh (passes on anything) and never.sh (passes on nothing).
func benchRepo(t *testing.T, env []string) (bare, base, fix string) {
	t.Helper()
	bare, _ = bareRemote(t, env)
	seed := filepath.Join(t.TempDir(), "seed")
	sh(t, filepath.Dir(seed), env, "git", "clone", "--quiet", bare, seed)
	write := func(name, body string) {
		must(t, os.WriteFile(filepath.Join(seed, name), []byte(body), 0o600))
		sh(t, seed, env, "git", "add", name)
	}
	write("calc.txt", "old\n")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "base")
	base = sh(t, seed, env, "git", "rev-parse", "HEAD")
	write("calc.txt", "new\n")
	write("check.sh", "test \"$(cat calc.txt)\" = new\n")
	write("always.sh", "exit 0\n")
	write("never.sh", "exit 1\n")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "fix: calc says new")
	fix = sh(t, seed, env, "git", "rev-parse", "HEAD")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, base, fix
}

// W-31 2.4 slice ⑤: a question that does not stand on this host never
// reaches the executor; one that does passes only when the fix's tests pass
// over the change; a run cut short or run again goes on from its results.
func TestBench(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, base, fix := benchRepo(t, env)
	q := func(id, test string) agentd.BenchQuestion {
		return agentd.BenchQuestion{ID: id, Repo: "/somewhere/bifrost-x", Language: "go", BaseCommit: base, FixCommit: fix,
			TestFiles: []string{test}, Checks: []agentd.BenchCheck{{Cwd: ".", Argv: []string{"sh", test}}}, TaskText: "fix: calc says new"}
	}
	qs := []agentd.BenchQuestion{q("Q1", "check.sh"), q("Q2", "check.sh"), q("Q3", "always.sh"), q("Q4", "never.sh"), q("Q5", "check.sh")}
	calls := filepath.Join(t.TempDir(), "calls")
	// Q1 makes the change, Q2 does nothing, Q5 runs out of time; Q3 and Q4
	// never reach the executor.
	exec := script(`echo "$PWD" >> ` + calls + `
case "$PWD" in
*/Q2/ws) ;;
*/Q5/ws) sleep 30 ;;
*) echo new > calc.txt ;;
esac`)
	root := filepath.Join(t.TempDir(), "bench", "run-test-0001")
	b := &agentd.Bench{Root: root, Self: os.Args[0], Remote: func(string) string { return bare }, GitEnv: env,
		CheckEnv: os.Environ(), Executor: exec, Tool: agentd.Tool{Executor: "exec:codex", Provider: "openai"},
		Limit: 3 * time.Second, KillGrace: time.Second, Logf: t.Logf}
	_, err := agentd.WriteBenchMeta(root, agentd.BenchMeta{RunID: "run-test-0001", Executor: "exec:codex", Provider: "openai",
		Questions: strings.Repeat("a", 64), StartedAt: time.Now()})
	must(t, err)
	got, err := b.Run(context.Background(), qs)
	must(t, err)
	outcomes := map[string]string{}
	for _, r := range got {
		outcomes[r.Question] = r.Outcome + ": " + r.Reason
	}
	for id, want := range map[string]string{"Q1": "pass: ", "Q2": "fail: sh check.sh exited 1",
		"Q3": "unrunnable: the checks pass on the code before the fix", "Q4": "unrunnable: the fix does not pass its checks here: sh never.sh exited 1",
		"Q5": "fail: the executor ran out of time; sh check.sh exited 1"} {
		if outcomes[id] != want {
			t.Errorf("%s: %q, want %q", id, outcomes[id], want)
		}
	}
	ran, err := os.ReadFile(calls)
	must(t, err)
	if n := strings.Count(string(ran), "\n"); n != 3 {
		t.Fatalf("the executor ran %d times (Q1, Q2, Q5 only): %s", n, ran)
	}
	// Run again: every outcome is read back, the executor is not called.
	again, err := b.Run(context.Background(), qs)
	must(t, err)
	a, _ := json.Marshal(again)
	g, _ := json.Marshal(got)
	if string(a) != string(g) {
		t.Fatalf("resumed: %s; first %s", a, g)
	}
	if ran2, _ := os.ReadFile(calls); string(ran2) != string(ran) {
		t.Fatalf("a resumed run called the executor again")
	}
	// A run is of one executor on one question set.
	if _, err = agentd.WriteBenchMeta(root, agentd.BenchMeta{RunID: "run-test-0001", Executor: "exec:cursor", Questions: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("a run resumed with another executor")
	}
}

func TestBenchQuestionsRefused(t *testing.T) {
	good := map[string]any{"id": "Q1", "repo": "/x/bifrost-platform", "language": "go", "base_commit": strings.Repeat("a", 40),
		"fix_commit": strings.Repeat("b", 40), "test_files": []string{"api/x_test.go"},
		"check_command": []any{map[string]any{"cwd": "api", "argv": []string{"go", "test", "./..."}}}, "task_text": "fix"}
	for name, mutate := range map[string]func(m map[string]any){
		"a test file outside": func(m map[string]any) { m["test_files"] = []string{"../evil_test.go"} },
		"a check outside": func(m map[string]any) {
			m["check_command"] = []any{map[string]any{"cwd": "/tmp", "argv": []string{"true"}}}
		},
		"a short commit":          func(m map[string]any) { m["base_commit"] = "abc" },
		"a language unknown":      func(m map[string]any) { m["language"] = "rust" },
		"no task text":            func(m map[string]any) { m["task_text"] = " " },
		"an id with a slash":      func(m map[string]any) { m["id"] = "Q/1" },
		"a repository path of ..": func(m map[string]any) { m["repo"] = "/x/.." },
	} {
		m := map[string]any{}
		for k, v := range good {
			m[k] = v
		}
		mutate(m)
		b, _ := json.Marshal([]any{m})
		path := filepath.Join(t.TempDir(), "q.json")
		must(t, os.WriteFile(path, b, 0o600))
		if _, _, err := agentd.ReadBenchQuestions(path); err == nil {
			t.Errorf("%s: read", name)
		}
	}
	b, _ := json.Marshal([]any{good})
	path := filepath.Join(t.TempDir(), "q.json")
	must(t, os.WriteFile(path, b, 0o600))
	if qs, sum, err := agentd.ReadBenchQuestions(path); err != nil || len(qs) != 1 || len(sum) != 64 {
		t.Fatalf("the good one: %v %s %v", qs, sum, err)
	}
}
