//go:build benchreplay

package agentd_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

// TestBenchReplay runs a question set through agentd bench on this Mac, with
// local repositories and sandbox-exec standing in for bubblewrap (no network
// but loopback; nothing of the home directory or the run but the tree and
// what it reads), and an executor that applies the fix's own non-test change:
// every question should pass. BENCH_MODE=null makes an executor that does
// nothing: every question should fail.
//
//	BENCH_QUESTIONS=... BENCH_ROOT=... [BENCH_REPOS=...] [BENCH_ONLY=Q01,Q02] \
//	  go test -tags benchreplay ./internal/agentd -run TestBenchReplay -timeout 0 -v
func TestBenchReplay(t *testing.T) {
	qfile, root := os.Getenv("BENCH_QUESTIONS"), os.Getenv("BENCH_ROOT")
	if qfile == "" || root == "" || runtime.GOOS != "darwin" {
		t.Skip("macOS, with BENCH_QUESTIONS and BENCH_ROOT")
	}
	repos := os.Getenv("BENCH_REPOS")
	if repos == "" {
		repos = "/Users/vision-mac-trader/Desktop/stocks"
	}
	qs, sum, err := agentd.ReadBenchQuestions(qfile)
	must(t, err)
	if only := os.Getenv("BENCH_ONLY"); only != "" {
		var keep []agentd.BenchQuestion
		for _, q := range qs {
			if strings.Contains(","+only+",", ","+q.ID+",") {
				keep = append(keep, q)
			}
		}
		qs = keep
	}
	root, err = filepath.EvalSymlinks(root)
	must(t, err)
	patches := filepath.Join(root, "patches")
	must(t, os.MkdirAll(patches, 0o700))
	for _, q := range qs {
		args := []string{"-C", filepath.Join(repos, q.RepoName()), "diff", "--binary", q.BaseCommit, q.FixCommit, "--", "."}
		for _, f := range q.TestFiles {
			args = append(args, ":(exclude)"+f)
		}
		out, gerr := exec.Command("git", args...).Output()
		must(t, gerr)
		must(t, os.WriteFile(filepath.Join(patches, q.ID+".patch"), out, 0o600))
	}
	mode := os.Getenv("BENCH_MODE")
	ex := func(a *agentd.Attempt) (agentd.Spawn, error) {
		must(t, os.WriteFile(filepath.Join(patches, a.Claim.TaskID+".prompt"), []byte(a.Spec.Text), 0o600))
		body := `git apply --whitespace=nowarn --binary "$PATCH"`
		if mode == "null" {
			body = "true"
		}
		return agentd.Spawn{Argv: []string{"sh", "-c", body}, Dir: a.Dir,
			Env: append(os.Environ(), "PATCH="+filepath.Join(patches, a.Claim.TaskID+".patch"), "TESTMAIN_AGENTD_LAUNCHER=1")}, nil
	}
	home, _ := os.UserHomeDir()
	modCache := filepath.Join(home, "go", "pkg", "mod")
	if out, gerr := exec.Command("go", "env", "GOMODCACHE").Output(); gerr == nil {
		modCache = strings.TrimSpace(string(out))
	}
	run := filepath.Join(root, "run")
	env := append(agentd.AllowedEnv(os.Environ()), "TESTMAIN_AGENTD_LAUNCHER=1")
	b := &agentd.Bench{Root: run, Self: os.Args[0], GitEnv: append(agentd.AllowedEnv(os.Environ()), "GIT_TERMINAL_PROMPT=0"),
		CheckEnv: env, Executor: ex, Tool: agentd.Tool{Executor: "exec:replay", Provider: "local"},
		Limit: 5 * time.Minute, KillGrace: 5 * time.Second, Logf: t.Logf, GoModCache: modCache,
		Secrets: []string{filepath.Join(home, ".config"), filepath.Join(home, ".ssh"), filepath.Join(home, "Desktop"), patches},
		Sandbox: macSandbox(home, root, patches),
		Remote:  func(repo string) string { return filepath.Join(repos, repo) }}
	must(t, b.Preflight(context.Background()))
	ids := []string{}
	for _, q := range qs {
		ids = append(ids, q.ID)
	}
	_, err = agentd.WriteBenchMeta(run, agentd.BenchMeta{RunID: "replay-" + filepath.Base(root), Executor: "exec:replay",
		Provider: "local", Model: "replay", Questions: sum, QuestionIDs: ids, StartedAt: time.Now()})
	must(t, err)
	got, err := b.Run(context.Background(), qs)
	must(t, err)
	for _, r := range got {
		fmt.Printf("RESULT %s %s %s %ds %s\n", r.Question, r.Class, r.Outcome, r.WallS, r.Reason)
	}
}

// macSandbox is BwrapSandbox's stand-in on macOS: the home directory and the
// run hidden but for the tree (writable) and readOnly, no network but
// loopback, HOME in the tree.
func macSandbox(home, root, patches string) func(tree string, readOnly, argv []string) []string {
	return func(tree string, readOnly, argv []string) []string {
		p := fmt.Sprintf(`(version 1)
(allow default)
(deny network-outbound)
(allow network-outbound (remote ip "localhost:*"))
(allow network-outbound (remote unix-socket))
(deny file-read* file-write* (subpath %q))
(deny file-read* file-write* (subpath %q))
(allow file-read-metadata (subpath %q))
(deny file-read-metadata (subpath %q))
(deny file-read* (subpath %q))
(allow file-read* file-write* (subpath %q))
`, home, root, root, patches, patches, tree)
		for _, r := range readOnly {
			p += fmt.Sprintf("(allow file-read* (subpath %q))\n", r)
		}
		return append([]string{"/usr/bin/sandbox-exec", "-p", p, "/usr/bin/env", "HOME=" + filepath.Join(tree, ".bench", "home")}, argv...)
	}
}
