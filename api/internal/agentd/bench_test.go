package agentd_test

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
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
*/Q10/work) printf new > "${TMPDIR:-/tmp}/bench-new-$$" && rm calc.txt && ln -s "${TMPDIR:-/tmp}/bench-new-$$" calc.txt ;;
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
		"an empty declaration":    func(m map[string]any) { m["interface"] = []string{" "} },
		"a huge declaration":      func(m map[string]any) { m["interface"] = []string{strings.Repeat("x", 5000)} },
		"a huge interface": func(m map[string]any) {
			m["interface"] = []string{strings.Repeat("x", 4000), strings.Repeat("x", 4000), strings.Repeat("x", 4000),
				strings.Repeat("x", 4000), strings.Repeat("x", 4000), strings.Repeat("x", 4000), strings.Repeat("x", 4000),
				strings.Repeat("x", 4000), strings.Repeat("x", 4000)}
		},
		"a sibling named as a tree": func(m map[string]any) {
			m["siblings"] = []any{map[string]any{"repo": "judge", "commit": strings.Repeat("c", 40)}}
		},
		"the repository as its own sibling": func(m map[string]any) {
			m["siblings"] = []any{map[string]any{"repo": "bifrost-platform", "commit": strings.Repeat("c", 40)}}
		},
		"a sibling twice": func(m map[string]any) {
			m["siblings"] = []any{map[string]any{"repo": "bifrost-ui", "commit": strings.Repeat("c", 40)},
				map[string]any{"repo": "bifrost-ui", "commit": strings.Repeat("d", 40)}}
		},
		"a sibling at a branch": func(m map[string]any) {
			m["siblings"] = []any{map[string]any{"repo": "bifrost-ui", "commit": "main"}}
		},
		"a sibling outside": func(m map[string]any) {
			m["siblings"] = []any{map[string]any{"repo": "../bifrost-ui", "commit": strings.Repeat("c", 40)}}
		},
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
	good["interface"] = []string{"func MaskJSON(b []byte) []byte"}
	good["siblings"] = []any{map[string]any{"repo": "bifrost-ui", "commit": strings.Repeat("c", 40)}}
	b, _ := json.Marshal([]any{good})
	path := filepath.Join(t.TempDir(), "q.json")
	must(t, os.WriteFile(path, b, 0o600))
	if qs, sum, err := agentd.ReadBenchQuestions(path); err != nil || len(qs) != 1 || len(sum) != 64 ||
		len(qs[0].Interface) != 1 || len(qs[0].Siblings) != 1 || qs[0].Siblings[0].Repo != "bifrost-ui" {
		t.Fatalf("the good one: %v %s %v", qs, sum, err)
	}
}

// TD-373: a sibling is built beside the trees before the controls, and the
// checks read it; the executor is told the declarations the tests call and
// that the sibling is there; a sibling that cannot be had or built makes
// the question unrunnable before the executor is called; nothing of it
// stays.
func TestBenchSiblings(t *testing.T) {
	if _, err := osexec.LookPath("npm"); err != nil {
		t.Skip("no npm")
	}
	env := gitEnvT(t.TempDir())
	bare, base, fix := benchRepo(t, env)
	ui, _ := bareRemote(t, env)
	seed := filepath.Join(t.TempDir(), "ui")
	sh(t, filepath.Dir(seed), env, "git", "clone", "--quiet", ui, seed)
	put := func(name, body string) {
		must(t, os.WriteFile(filepath.Join(seed, name), []byte(body), 0o600))
		sh(t, seed, env, "git", "add", name)
	}
	put("package.json", `{"name":"ui","version":"1.0.0","scripts":{"build":"node -e \"require('fs').writeFileSync('dist.txt','built')\""}}`)
	put("package-lock.json", `{"name":"ui","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"ui","version":"1.0.0"}}}`)
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "ui")
	good := sh(t, seed, env, "git", "rev-parse", "HEAD")
	put("package.json", `{"name":"ui","version":"1.0.1","scripts":{"build":"exit 7"}}`)
	put("package-lock.json", `{"name":"ui","version":"1.0.1","lockfileVersion":3,"requires":true,"packages":{"":{"name":"ui","version":"1.0.1"}}}`)
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "ui that does not build")
	broken := sh(t, seed, env, "git", "rev-parse", "HEAD")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	// The check passes only with the sibling built beside the tree it runs in.
	q := func(id, commit string) agentd.BenchQuestion {
		return agentd.BenchQuestion{ID: id, Repo: "/somewhere/bifrost-x", Language: "go", BaseCommit: base, FixCommit: fix,
			TestFiles: []string{"check.sh"}, TaskText: "fix: calc says new",
			Checks:    []agentd.BenchCheck{{Cwd: ".", Argv: []string{"sh", "-c", `test "$(cat ../ui/dist.txt)" = built && sh check.sh`}}},
			Interface: []string{"func Calc() string // in calc.go"}, Siblings: []agentd.BenchSibling{{Repo: "ui", Commit: commit}}}
	}
	prompts := filepath.Join(t.TempDir(), "prompts")
	ex := func(a *agentd.Attempt) (agentd.Spawn, error) {
		f, err := os.OpenFile(prompts, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return agentd.Spawn{}, err
		}
		_, _ = f.WriteString(a.Spec.Text + "\n=====\n")
		_ = f.Close()
		return agentd.Spawn{Argv: []string{"sh", "-c", "test -f ../ui/dist.txt && echo new > calc.txt"}, Dir: a.Dir,
			Env: append(os.Environ(), "AGENTD_TEST_LAUNCHER=1")}, nil
	}
	root := filepath.Join(t.TempDir(), "bench", "run-test-0002")
	// A sandbox that confines nothing but records what each command may read
	// beside its tree: the judgement's checks must have the sibling (review 292 C8).
	var views []string
	sandbox := func(tree string, ro, argv []string) []string {
		views = append(views, filepath.Base(tree)+" "+strings.Join(ro, " "))
		return argv
	}
	b := &agentd.Bench{Root: root, Self: os.Args[0], GitEnv: env, CheckEnv: append(os.Environ(), "AGENTD_TEST_LAUNCHER=1"), Executor: ex,
		Sandbox: sandbox,
		Remote: func(repo string) string {
			if repo == "ui" {
				return ui
			}
			return bare
		},
		Tool: agentd.Tool{Executor: "exec:codex", Provider: "openai"}, Limit: 30 * time.Second, KillGrace: time.Second, Logf: t.Logf}
	qs := []agentd.BenchQuestion{q("Q1", good), q("Q2", broken), q("Q3", strings.Repeat("e", 40))}
	got, err := b.Run(context.Background(), qs)
	must(t, err)
	outcomes := map[string]string{}
	for _, r := range got {
		outcomes[r.Question] = r.Outcome + ": " + r.Reason
	}
	if outcomes["Q1"] != "pass: " {
		t.Errorf("Q1: %q", outcomes["Q1"])
	}
	for _, id := range []string{"Q2", "Q3"} {
		if !strings.HasPrefix(outcomes[id], "unrunnable: the sibling ui: ") {
			t.Errorf("%s: %q", id, outcomes[id])
		}
	}
	body, err := os.ReadFile(prompts)
	must(t, err)
	if n := strings.Count(string(body), "====="); n != 1 {
		t.Fatalf("the executor ran %d times (Q1 only): %s", n, body)
	}
	for _, want := range []string{"fix: calc says new", "Declare them as given", "\nfunc Calc() string // in calc.go\n",
		"../ui beside this checkout is ui as the change found it, built"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the prompt has no %q: %s", want, body)
		}
	}
	for _, x := range qs {
		entries, rerr := os.ReadDir(filepath.Join(root, x.ID))
		must(t, rerr)
		if len(entries) != 1 || entries[0].Name() != "out" {
			t.Errorf("%s left %v", x.ID, entries)
		}
	}
	judged := false
	for _, v := range views {
		if strings.HasPrefix(v, "judge ") {
			judged = true
			if !strings.Contains(v, filepath.Join(root, "Q1", "ui")) {
				t.Errorf("a check of the judgement cannot read the sibling: %s", v)
			}
		}
	}
	if !judged {
		t.Fatalf("no check of the judgement ran in the sandbox: %v", views)
	}
}

// Review 292 B6: a lockfile npm ci refuses is resolved once a question, and
// the tree before the fix and the judgement install that one resolution, not
// one each.
func TestBenchNpmResolvedOnce(t *testing.T) {
	npm, err := osexec.LookPath("npm")
	if err != nil {
		t.Skip("no npm")
	}
	env := gitEnvT(t.TempDir())
	bare, _ := bareRemote(t, env)
	seed := filepath.Join(t.TempDir(), "seed")
	sh(t, filepath.Dir(seed), env, "git", "clone", "--quiet", bare, seed)
	put := func(name, body string) {
		must(t, os.MkdirAll(filepath.Dir(filepath.Join(seed, name)), 0o700))
		must(t, os.WriteFile(filepath.Join(seed, name), []byte(body), 0o600))
		sh(t, seed, env, "git", "add", name)
	}
	put("package.json", `{"name":"x","version":"1.0.0","dependencies":{"dep":"file:./dep"}}`)
	// the lockfile misses dep: npm ci refuses it (EUSAGE)
	put("package-lock.json", `{"name":"x","version":"1.0.0","lockfileVersion":3,"requires":true,"packages":{"":{"name":"x","version":"1.0.0"}}}`)
	put("dep/package.json", `{"name":"dep","version":"1.0.0"}`)
	put("calc.txt", "old\n")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "base")
	base := sh(t, seed, env, "git", "rev-parse", "HEAD")
	put("calc.txt", "new\n")
	put("check.sh", "test -L node_modules/dep && test \"$(cat calc.txt)\" = new\n")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "fix: calc says new")
	fix := sh(t, seed, env, "git", "rev-parse", "HEAD")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	shim := t.TempDir()
	calls := filepath.Join(t.TempDir(), "npm-calls")
	must(t, os.WriteFile(filepath.Join(shim, "npm"), []byte("#!/bin/sh\necho \"$*\" >> "+calls+"\nexec "+npm+" \"$@\"\n"), 0o700))
	q := agentd.BenchQuestion{ID: "Q1", Repo: "/somewhere/bifrost-x", Language: "ts", BaseCommit: base, FixCommit: fix,
		TestFiles: []string{"check.sh"}, Checks: []agentd.BenchCheck{{Cwd: ".", Argv: []string{"sh", "check.sh"}}}, TaskText: "fix: calc says new"}
	root := filepath.Join(t.TempDir(), "bench", "run-test-0003")
	b := &agentd.Bench{Root: root, Self: os.Args[0], Remote: func(string) string { return bare }, GitEnv: env,
		CheckEnv: append(os.Environ(), "AGENTD_TEST_LAUNCHER=1", "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
			"npm_config_offline=true", "npm_config_cache="+t.TempDir()),
		Executor: script("echo new > calc.txt"), Tool: agentd.Tool{Executor: "exec:codex", Provider: "openai"},
		Limit: 30 * time.Second, KillGrace: time.Second, Logf: t.Logf}
	got, err := b.Run(context.Background(), []agentd.BenchQuestion{q})
	must(t, err)
	if len(got) != 1 || got[0].Outcome != "pass" {
		t.Fatalf("%+v", got)
	}
	body, err := os.ReadFile(calls)
	must(t, err)
	if n := strings.Count(string(body), "install --ignore-scripts"); n != 1 {
		t.Fatalf("resolved %d times (once for the fix's tree, the tree before it and the judgement): %s", n, body)
	}
	if strings.Contains(string(body), "--no-save") || strings.Count(string(body), "ci --ignore-scripts") != 6 {
		t.Fatalf("each tree must npm ci, refused and then with the one resolution: %s", body)
	}
}

// Review 287 B2, 288 B2: what the checks run as is never taken from a change.
func TestBenchProtected(t *testing.T) {
	for _, p := range []string{"pytest.py", "sub/conftest.py", "api/x_test.go", "console/src/test/setup.ts", "tests/helpers.py",
		"console/vitest.config.ts", "package.json", "go.mod", "node_modules/.bin/vitest", "lib/site.pth", "sitecustomize.py",
		"src/__mocks__/clock.ts", "pyproject.toml", "npm-shrinkwrap.json", "console/npm-shrinkwrap.json", ".pytest.ini", "sub/.pytest.ini",
		"pytest.toml", ".pytest.toml"} {
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
