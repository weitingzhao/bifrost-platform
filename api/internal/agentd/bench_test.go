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
// calc.txt goes from old to new and old.txt goes away; the fix adds the
// tests: check.sh (new calc, or a pytest.ini, which only a protected file
// gives), gone.sh (no old.txt), always.sh, never.sh and bg.sh (leaves a
// process behind that would touch $BENCH_MARK a second later).
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
	write("old.txt", "x\n")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "base")
	base = sh(t, seed, env, "git", "rev-parse", "HEAD")
	write("calc.txt", "new\n")
	sh(t, seed, env, "git", "rm", "--quiet", "old.txt")
	write("check.sh", "test \"$(cat calc.txt)\" = new || test -f pytest.ini\n")
	write("gone.sh", "test ! -e old.txt\n")
	write("always.sh", "exit 0\n")
	write("never.sh", "exit 1\n")
	write("bg.sh", "(sleep 1; touch \"$BENCH_MARK\") > /dev/null 2>&1 &\ntest \"$(cat calc.txt)\" = new\n")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "fix: calc says new")
	fix = sh(t, seed, env, "git", "rev-parse", "HEAD")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, base, fix
}

// W-31 2.4 slice ⑤, review 287: a question that does not stand never reaches
// the executor; the executor sees only the code before the fix; the judgement
// takes its change but no protected file and no link, and takes its removals;
// a check leaves nothing running; a run goes on from its results.
func TestBench(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, base, fix := benchRepo(t, env)
	q := func(id, test string) agentd.BenchQuestion {
		return agentd.BenchQuestion{ID: id, Repo: "/somewhere/bifrost-x", Language: "go", BaseCommit: base, FixCommit: fix,
			TestFiles: []string{test}, Checks: []agentd.BenchCheck{{Cwd: ".", Argv: []string{"sh", test}}}, TaskText: "fix: calc says new"}
	}
	qs := []agentd.BenchQuestion{q("Q1", "check.sh"), q("Q2", "check.sh"), q("Q3", "always.sh"), q("Q4", "never.sh"),
		q("Q5", "check.sh"), q("Q6", "check.sh"), q("Q7", "check.sh"), q("Q8", "gone.sh"), q("Q9", "bg.sh"), q("Q10", "check.sh"),
		q("Q11", "check.sh")}
	calls := filepath.Join(t.TempDir(), "calls")
	mark := filepath.Join(t.TempDir(), "late-marker")
	exec := script(`echo "$PWD" >> ` + calls + `
case "$PWD" in
*/Q2/work) ;;
*/Q5/work) sleep 30 ;;
*/Q6/work) echo > pytest.ini ;;
*/Q7/work) [ "$(git rev-list --all | wc -l | tr -d ' ')" = 1 ] && [ ! -e ../src ] && [ ! -e ../fix ] && echo new > calc.txt ;;
*/Q8/work) rm old.txt ;;
*/Q10/work) printf new > /tmp/bench-new-$$ && rm calc.txt && ln -s /tmp/bench-new-$$ calc.txt ;;
*/Q11/work) mkdir locked && echo new > locked/answer && chmod 000 locked ;;
*) echo new > calc.txt ;;
esac`)
	root := filepath.Join(t.TempDir(), "bench", "run-test-0001")
	b := &agentd.Bench{Root: root, Self: os.Args[0], Remote: func(string) string { return bare }, GitEnv: env,
		CheckEnv: append(os.Environ(), "AGENTD_TEST_LAUNCHER=1", "BENCH_MARK="+mark), Executor: exec,
		Tool: agentd.Tool{Executor: "exec:codex", Provider: "openai"}, Limit: 3 * time.Second, KillGrace: time.Second, Logf: t.Logf}
	ids := []string{}
	for _, x := range qs {
		ids = append(ids, x.ID)
	}
	meta, err := agentd.WriteBenchMeta(root, agentd.BenchMeta{RunID: "run-test-0001", Executor: "exec:codex", Provider: "openai",
		Questions: strings.Repeat("a", 64), QuestionIDs: ids, StartedAt: time.Now()})
	must(t, err)
	got, err := b.Run(context.Background(), qs)
	must(t, err)
	outcomes := map[string]string{}
	for _, r := range got {
		outcomes[r.Question] = r.Outcome + ": " + r.Reason
	}
	for id, want := range map[string]string{
		"Q1": "pass: ", "Q2": "fail: sh check.sh exited 1",
		"Q3": "unrunnable: the checks pass on the code before the fix",
		"Q4": "unrunnable: the fix does not pass its checks here: sh never.sh exited 1",
		"Q5": "fail: the executor ran out of time; sh check.sh exited 1",
		"Q6": "fail: 1 protected or linked files of its change left out; sh check.sh exited 1",
		"Q7": "pass: ", "Q8": "pass: ", "Q9": "pass: ",
		"Q10": "fail: 1 protected or linked files of its change left out; sh check.sh exited 1", // no output of the change's code (review 288 A1)
	} {
		if outcomes[id] != want {
			t.Errorf("%s: %q, want %q", id, outcomes[id], want)
		}
	}
	ran, err := os.ReadFile(calls)
	must(t, err)
	if n := strings.Count(string(ran), "\n"); n != 9 {
		t.Fatalf("the executor ran %d times (all but Q3 and Q4): %s", n, ran)
	}
	if !strings.HasPrefix(outcomes["Q11"], "fail: ") {
		t.Errorf("Q11: %q", outcomes["Q11"])
	}
	// Only out/ is left of each question: no clone, no tree, no change (review 288 B3).
	for _, x := range qs {
		entries, rerr := os.ReadDir(filepath.Join(root, x.ID))
		must(t, rerr)
		if len(entries) != 1 || entries[0].Name() != "out" {
			names := []string{}
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf("%s left %v", x.ID, names)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	if _, serr := os.Stat(mark); serr == nil {
		t.Fatal("a check of Q9 left a process that ran on after it")
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
	if all, cerr := meta.Complete(got); cerr != nil || len(all) != len(qs) {
		t.Fatalf("complete: %d results, %v", len(all), cerr)
	}
	if _, cerr := meta.Complete(got[:3]); cerr == nil {
		t.Fatal("a run with questions left was complete")
	}
	trial := meta
	trial.Trial = true
	if _, cerr := trial.Complete(got); cerr == nil {
		t.Fatal("a trial is handed in")
	}
	// With no model pinned a run is not resumed, even as it was (review 288 B9).
	if _, err = agentd.WriteBenchMeta(root, meta); err == nil {
		t.Fatal("a run with no model pinned was resumed")
	}
	// A run is of one executor, provider and model on one question set (review 287 B9).
	for name, m := range map[string]agentd.BenchMeta{
		"another executor": {RunID: meta.RunID, Executor: "exec:cursor", Provider: "openai", Questions: meta.Questions, QuestionIDs: ids},
		"another provider": {RunID: meta.RunID, Executor: "exec:codex", Provider: "other", Questions: meta.Questions, QuestionIDs: ids},
		"another model":    {RunID: meta.RunID, Executor: "exec:codex", Provider: "openai", Model: "m2", Questions: meta.Questions, QuestionIDs: ids},
		"fewer questions":  {RunID: meta.RunID, Executor: "exec:codex", Provider: "openai", Questions: meta.Questions, QuestionIDs: ids[:2]},
	} {
		if _, err = agentd.WriteBenchMeta(root, m); err == nil {
			t.Errorf("a run resumed with %s", name)
		}
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

// Review 287 B2, 288 B2: what the checks run as is never taken from a change.
func TestBenchProtected(t *testing.T) {
	for _, p := range []string{"pytest.py", "sub/conftest.py", "api/x_test.go", "console/src/test/setup.ts", "tests/helpers.py",
		"console/vitest.config.ts", "package.json", "go.mod", "node_modules/.bin/vitest", "lib/site.pth", "sitecustomize.py",
		"src/__mocks__/clock.ts", "pyproject.toml"} {
		if !agentd.Protected(p) {
			t.Errorf("%s is taken from a change", p)
		}
	}
	for _, p := range []string{"api/internal/agentd/run.go", "console/src/pages/Work.tsx", "scripts/release.sh", "src/testing.py"} {
		if agentd.Protected(p) {
			t.Errorf("%s is not taken from a change", p)
		}
	}
}

// Review 290 A1: a check runs where it sees none of the host's secrets: an
// empty /home but for its tree and what it reads, no network, no other
// process.
func TestBwrapSandbox(t *testing.T) {
	argv := agentd.BwrapSandbox("/opt/bwrap")("/home/u/run/Q1/judge", []string{"/home/u/go/pkg/mod", "/home/u/run/Q1/out/venv"},
		[]string{"sh", "check.sh"})
	got := strings.Join(argv, " ")
	for _, want := range []string{"/opt/bwrap --unshare-user", "--unshare-net", "--die-with-parent", "--ro-bind / /", "--tmpfs /home", "--tmpfs /tmp",
		"--ro-bind /home/u/go/pkg/mod /home/u/go/pkg/mod", "--ro-bind /home/u/run/Q1/out/venv /home/u/run/Q1/out/venv",
		"--bind /home/u/run/Q1/judge /home/u/run/Q1/judge", "--setenv HOME /home/u/run/Q1/judge/.bench/home -- sh check.sh"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %s", want, got)
		}
	}
	// No fresh /proc: on the agent host the kernel refuses it under the unit's hardening.
	if strings.Contains(got, "--proc") || strings.Contains(got, "--unshare-pid") || strings.Contains(got, "--unshare-all") {
		t.Errorf("a fresh /proc or pid namespace: %s", got)
	}
	if strings.Index(got, "--tmpfs /home") > strings.Index(got, "--bind /home/u/run/Q1/judge") {
		t.Errorf("the tree is bound before /home is emptied: %s", got)
	}
}

// Review 290 A1: no question runs unless a probe, run as a check is, sees
// none of the secrets.
func TestBenchPreflight(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "agentd.token")
	must(t, os.WriteFile(secret, []byte("x"), 0o600))
	b := &agentd.Bench{Root: t.TempDir(), Self: os.Args[0], CheckEnv: append(os.Environ(), "AGENTD_TEST_LAUNCHER=1"), KillGrace: time.Second,
		Secrets: []string{secret}}
	if err := b.Preflight(context.Background()); err == nil {
		t.Fatal("ran with no sandbox")
	}
	b.Sandbox = func(_ string, _, argv []string) []string { return argv } // confines nothing
	if err := b.Preflight(context.Background()); err == nil || !strings.Contains(err.Error(), "visible: "+secret) {
		t.Fatalf("a sandbox that shows the secret: %v", err)
	}
	b.Secrets = []string{filepath.Join(t.TempDir(), "not-there")}
	if err := b.Preflight(context.Background()); err != nil {
		t.Fatalf("a sandbox that shows no secret: %v", err)
	}
}
