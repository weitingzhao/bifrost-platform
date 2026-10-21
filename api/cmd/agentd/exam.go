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
	"strings"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

// runExam is agentd exam (W-31 2.4 ④; internal/agentd/exam.go):
//
//	agentd exam --job chief_of_staff --executor exec:claude [--limit 15m] [--only A1,B2] [--dev]
//
// It reads the daemon's configuration (the same environment), takes the host
// lock, so it refuses to run beside agentd (on agent-06 the exam unit stops
// the STG agentd first), proves the write wall, and sits the paper this host
// keeps for the job and the executor, or a new one from the platform, each
// run handed in as it ends. A sitting cut short goes on from where it was
// when it is run again, until the paper lapses (24 h after its issue). A
// host whose wall does not hold the executor proves no run's pins and the
// platform takes no run without them: there only --dev runs, a local trial
// that hands nothing in. Only exec:claude sits an exam for now: it is the
// chief of staff's seat.
func runExam(args []string) int {
	fs := flag.NewFlagSet("exam", flag.ContinueOnError)
	job := fs.String("job", "", "the job whose exam to sit, e.g. chief_of_staff")
	executor := fs.String("executor", "", "the executor that sits it: exec:claude")
	limit := fs.Duration("limit", 15*time.Minute, "the executor's time on one round of one run")
	only := fs.String("only", "", "only these items of the paper, comma-separated")
	dev := fs.Bool("dev", false, "a local trial, on a host without the write wall too: nothing is handed in")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 || *job == "" || *executor == "" {
		log.Printf("agentd exam: usage: agentd exam --job <job> --executor exec:claude [--limit 15m] [--only <item>,...] [--dev]")
		return 2
	}
	if err := sitExam(*job, *executor, *only, *limit, *dev); err != nil {
		log.Printf("agentd exam: %v", err)
		return 1
	}
	return 0
}

func sitExam(job, executor, only string, limit time.Duration, dev bool) error {
	if err := agentd.ExamExecutor(executor); err != nil {
		return err
	}
	cfg, err := config()
	if err != nil {
		return err
	}
	var tool agentd.Tool
	for _, tl := range cfg.tools {
		if tl.Executor == executor {
			tool = tl
		}
	}
	if tool.Executor == "" {
		return fmt.Errorf("%s is not in AGENTD_TOOLS", executor)
	}
	lockPath, err := agentd.HostLockPath()
	if err != nil {
		return err
	}
	lock, err := agentd.LockHost(lockPath)
	if errors.Is(err, agentd.ErrHeld) {
		return errors.New("agentd (or a bench or an exam) holds this host: stop it first")
	}
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	// What a bench, an exam or agentd killed left running is ended first, or
	// nothing is sat beside it (review 487 A2).
	if err = agentd.LeftOnHost(cfg.root, 10*time.Second); err != nil {
		return fmt.Errorf("%w; nothing is sat until then", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	executors, refused := agentd.Isolated(agentd.Executors(self, os.Environ(), agentd.Secrets{ClaudeOAuthToken: cfg.claudeToken}), agentd.MCPChecks)
	ex, ok := executors[executor]
	if !ok {
		return fmt.Errorf("%s is not offered on this host: %s", executor, strings.Join(refused, "; "))
	}
	wall, proof := agentd.ProveWall(ctx, self, cfg.root, agentd.AllowedEnv(os.Environ()), "")
	for _, line := range proof {
		log.Printf("agentd exam: write wall: %s", line)
	}
	version, err := agentd.ToolVersion(ctx, executor, os.Environ(), filepath.Join(cfg.root, "exam"))
	if err != nil {
		return err
	}
	e := &agentd.Exam{Root: cfg.root, Self: self, Client: &agentd.Client{Base: cfg.platform, Host: cfg.host, Token: cfg.token},
		Remote: func(repo string) string { return strings.TrimRight(cfg.gitBase, "/") + "/" + repo + ".git" }, GitEnv: cfg.gitEnv,
		Executor: ex, Tool: tool, ToolVersion: version, Wall: wall, Dev: dev, Limit: limit, Logf: log.Printf}
	p, err := e.Paper(ctx, job)
	if err != nil {
		return err
	}
	var items []string
	for _, it := range strings.Split(only, ",") {
		if it = strings.TrimSpace(it); it != "" {
			items = append(items, it)
		}
	}
	log.Printf("agentd exam: %s sits paper %s with %s (%s %s, %s)", cfg.host, p.RunID, executor, tool.Provider, tool.Model, version)
	return e.Sit(ctx, p, items)
}
