package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

// runBench is agentd bench (W-31 2.4 slice ⑤; internal/agentd/bench.go):
//
//	agentd bench --questions <file> --executor exec:codex [--run-id <id>] [--only Q01,Q02] [--limit 30m]
//	agentd bench --post <run id>
//
// It reads the daemon's configuration (the same environment), takes the host
// lock, so it refuses to run beside agentd (on agent-06 the bench unit stops
// the STG agentd first), runs the questions with the executor as agentd
// would run an attempt of it, and hands the run in as this host. A run cut
// short goes on with the same --run-id; --post hands a run in again.
func runBench(args []string) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	questions := fs.String("questions", "", "the question set (questions.json)")
	executor := fs.String("executor", "", "the executor to run them with, e.g. exec:codex")
	runID := fs.String("run-id", "", "the run to start or go on with (default: a new one)")
	only := fs.String("only", "", "only these questions, comma-separated")
	limit := fs.Duration("limit", 30*time.Minute, "the executor's time on one question")
	post := fs.String("post", "", "hand in this run again instead of running anything")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := bench(*questions, *executor, *runID, *only, *post, *limit); err != nil {
		log.Printf("agentd bench: %v", err)
		return 1
	}
	return 0
}

func bench(questions, executor, runID, only, post string, limit time.Duration) error {
	cfg, err := config()
	if err != nil {
		return err
	}
	lockPath, err := agentd.HostLockPath()
	if err != nil {
		return err
	}
	lock, err := agentd.LockHost(lockPath)
	if errors.Is(err, agentd.ErrHeld) {
		return errors.New("agentd (or another bench) holds this host: stop it first")
	}
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	client := &agentd.Client{Base: cfg.platform, Host: cfg.host, Token: cfg.token}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if post != "" {
		root := filepath.Join(cfg.root, "bench", filepath.Base(post))
		meta, merr := agentd.ReadBenchMeta(root)
		if merr != nil {
			return merr
		}
		results, rerr := agentd.ReadBenchResults(root)
		if rerr != nil {
			return rerr
		}
		return handIn(ctx, client, meta, results)
	}
	if questions == "" || executor == "" {
		return errors.New("--questions and --executor are required (or --post)")
	}
	qs, sum, err := agentd.ReadBenchQuestions(questions)
	if err != nil {
		return err
	}
	if only != "" {
		want := strings.Split(only, ",")
		qs = slices.DeleteFunc(qs, func(q agentd.BenchQuestion) bool { return !slices.Contains(want, q.ID) })
		if len(qs) == 0 {
			return fmt.Errorf("--only %s names no question of the set", only)
		}
	}
	var tool agentd.Tool
	for _, tl := range cfg.tools {
		if tl.Executor == executor {
			tool = tl
		}
	}
	if tool.Executor == "" || executor == "exec:deterministic" {
		return fmt.Errorf("%s is not an agent executor of AGENTD_TOOLS", executor)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	executors, refused := agentd.Isolated(agentd.Executors(self, os.Environ(), agentd.Secrets{
		CursorAPIKey: cfg.cursorKey, ClaudeOAuthToken: cfg.claudeToken,
	}), agentd.MCPChecks)
	ex, ok := executors[executor]
	if !ok {
		return fmt.Errorf("%s is not offered on this host: %s", executor, strings.Join(refused, "; "))
	}
	if runID == "" {
		runID = "bench-" + time.Now().UTC().Format("20060102T150405") + "-" + strings.TrimPrefix(executor, "exec:")
	}
	root := filepath.Join(cfg.root, "bench", filepath.Base(runID))
	meta, err := agentd.WriteBenchMeta(root, agentd.BenchMeta{RunID: runID, Executor: executor, Provider: tool.Provider,
		Model: tool.Model, Questions: sum, StartedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	log.Printf("agentd bench: run %s, %d questions with %s (%s), in %s", meta.RunID, len(qs), executor, tool.Provider, root)
	b := &agentd.Bench{Root: root, Self: self, GitEnv: cfg.gitEnv, CheckEnv: agentd.AllowedEnv(os.Environ()), Executor: ex,
		Tool: tool, Limit: limit, Logf: log.Printf,
		Remote: func(repo string) string { return strings.TrimRight(cfg.gitBase, "/") + "/" + repo + ".git" }}
	results, err := b.Run(ctx, qs)
	if err != nil {
		return fmt.Errorf("%w (the results so far are kept: run again with --run-id %s)", err, meta.RunID)
	}
	return handIn(ctx, client, meta, results)
}

func handIn(ctx context.Context, client *agentd.Client, meta agentd.BenchMeta, results []agentd.BenchResult) error {
	n := map[string]int{}
	for _, r := range results {
		n[r.Outcome]++
	}
	log.Printf("agentd bench: run %s: %d pass, %d fail, %d unrunnable", meta.RunID, n["pass"], n["fail"], n["unrunnable"])
	out, err := client.Benchmark(ctx, meta, results)
	if err != nil {
		return fmt.Errorf("handing the run in: %w (hand it in again with --post %s)", err, meta.RunID)
	}
	log.Printf("agentd bench: handed in: event %v, duplicate %v", out["event_id"], out["duplicate"])
	return nil
}
